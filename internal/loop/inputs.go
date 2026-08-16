package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/fetchconsent"
	"github.com/clayton/harness-benchmark/internal/paths"
)

var versionPattern = regexp.MustCompile(`\b([0-9]+)\.([0-9]+)(?:\.([0-9]+))?\b`)
var commandNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

type RequirementResult struct {
	Name    string
	Path    string
	Version string
	Minimum string
	Purpose string
	Problem string
}

func CheckRequirements(sc corpus.Scenario) ([]RequirementResult, error) {
	results := make([]RequirementResult, 0, len(sc.Requirements.Commands))
	var problems []string
	for _, requirement := range sc.Requirements.Commands {
		result := RequirementResult{Name: requirement.Name, Minimum: requirement.MinimumVersion, Purpose: requirement.Purpose}
		if !commandNamePattern.MatchString(requirement.Name) {
			result.Problem = "invalid executable name"
			problems = append(problems, requirement.Name+": "+result.Problem)
			results = append(results, result)
			continue
		}
		path, err := exec.LookPath(requirement.Name)
		if err != nil {
			result.Problem = "not found on PATH"
			problems = append(problems, requirement.Name+": "+result.Problem)
			results = append(results, result)
			continue
		}
		result.Path = path
		if requirement.MinimumVersion != "" {
			raw, err := exec.Command(requirement.Name, "--version").CombinedOutput()
			if err != nil {
				result.Problem = "could not read version"
			} else if found := versionPattern.FindStringSubmatch(string(raw)); found == nil {
				result.Problem = "version output was not recognized"
			} else {
				result.Version = found[1] + "." + found[2] + "." + defaultPatch(found[3])
				if compareVersion(result.Version, requirement.MinimumVersion) < 0 {
					result.Problem = fmt.Sprintf("version %s is below required %s", result.Version, requirement.MinimumVersion)
				}
			}
			if result.Problem != "" {
				problems = append(problems, requirement.Name+": "+result.Problem)
			}
		}
		results = append(results, result)
	}
	if len(problems) > 0 {
		return results, fmt.Errorf("scenario prerequisites are not satisfied:\n  %s\nhbench did not fetch or install anything", strings.Join(problems, "\n  "))
	}
	return results, nil
}

func defaultPatch(value string) string {
	if value == "" {
		return "0"
	}
	return value
}

func compareVersion(left, right string) int {
	parse := func(value string) [3]int {
		match := versionPattern.FindStringSubmatch(value)
		var out [3]int
		if match != nil {
			out[0], _ = strconv.Atoi(match[1])
			out[1], _ = strconv.Atoi(match[2])
			out[2], _ = strconv.Atoi(defaultPatch(match[3]))
		}
		return out
	}
	a, b := parse(left), parse(right)
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

type FetchAuthorizer func(fetchconsent.Plan) error

func PrepareInputs(l paths.Layout, sc corpus.Scenario, includeDependencies bool, authorize FetchAuthorizer) error {
	if includeDependencies {
		if err := validateSetupFetches(sc); err != nil {
			return err
		}
	}
	repo := ""
	if sc.Repo.URL != "" {
		cache := filepath.Join(l.ReposDir(), repoSlug(sc.Repo.URL))
		if repositoryNeedsFetch(cache, sc.Repo.BaseRef) {
			plan := fetchconsent.New(fetchconsent.Item{
				Kind: "Git repository", Source: sc.Repo.URL, Ref: sc.Repo.BaseRef,
				Reason: "benchmark source", Destination: cache, Size: "unknown",
			})
			if err := authorize(plan); err != nil {
				return err
			}
		}
		var err error
		repo, err = ensureRepo(l, sc)
		if err != nil {
			return err
		}
	}
	if !includeDependencies {
		return nil
	}
	for _, declared := range sc.Fetches {
		if repo == "" {
			return fmt.Errorf("declared fetch %q requires a repository", declared.Kind)
		}
		switch declared.Kind {
		case "cargo":
			if err := prepareCargoDependencies(l, repo, declared, authorize); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported declared fetch kind %q", declared.Kind)
		}
	}
	return nil
}

func validateSetupFetches(sc corpus.Scenario) error {
	for _, command := range sc.Acceptance.SetupCommands {
		lower := strings.ToLower(command)
		unsafe := (strings.Contains(lower, "pip install") && !strings.Contains(lower, "--no-index")) ||
			((strings.Contains(lower, "npm install") || strings.Contains(lower, "npm ci")) && !strings.Contains(lower, "--offline")) ||
			(strings.Contains(lower, "pnpm install") && !strings.Contains(lower, "--offline")) ||
			(strings.Contains(lower, "yarn install") && !strings.Contains(lower, "--offline")) ||
			(strings.Contains(lower, "cargo build") && !strings.Contains(lower, "--offline")) ||
			strings.Contains(lower, "cargo install") || strings.Contains(lower, "go mod download") || strings.Contains(lower, "bundle install") ||
			strings.Contains(lower, "git clone") || strings.Contains(lower, "git fetch") ||
			strings.Contains(lower, "apt-get ") || strings.Contains(lower, "brew install") ||
			strings.Contains(lower, "curl ") || strings.Contains(lower, "wget ")
		if unsafe {
			return fmt.Errorf("setup command can download or install software without a supported fetch plan: %q\nhbench did not run setup or access the network", command)
		}
	}
	return nil
}

func repositoryNeedsFetch(cache, ref string) bool {
	if isPartialClone(cache) {
		return true
	}
	if _, err := os.Stat(filepath.Join(cache, ".git")); err != nil {
		return true
	}
	return git(cache, "cat-file", "-e", ref+"^{commit}") != nil
}

func cargoHome(l paths.Layout) string {
	return filepath.Join(l.DataDir, "dependencies", "cargo")
}

func prepareCargoDependencies(l paths.Layout, repo string, declared corpus.Fetch, authorize FetchAuthorizer) error {
	lockfile := filepath.Clean(declared.Lockfile)
	if lockfile == "." || filepath.IsAbs(lockfile) || lockfile == ".." || strings.HasPrefix(lockfile, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe cargo lockfile %q", declared.Lockfile)
	}
	lockPath := filepath.Join(repo, lockfile)
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("read cargo lockfile: %w", err)
	}
	manifest := filepath.Join(repo, "Cargo.toml")
	home := cargoHome(l)
	if cargoFetch(repo, manifest, home, true) == nil {
		return nil
	}
	sum := sha256.Sum256(raw)
	plan := fetchconsent.New(fetchconsent.Item{
		Kind: "Cargo dependencies", Source: "https://crates.io", Ref: "sha256:" + hex.EncodeToString(sum[:]),
		Checksum: "sha256:" + hex.EncodeToString(sum[:]), Reason: declared.Reason, Destination: home, Size: "unknown",
	})
	if err := authorize(plan); err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	if err := cargoFetch(repo, manifest, home, false); err != nil {
		return fmt.Errorf("fetch approved Cargo dependencies: %w", err)
	}
	return nil
}

func cargoFetch(repo, manifest, home string, offline bool) error {
	args := []string{"fetch", "--locked", "--manifest-path", manifest}
	if offline {
		args = append(args, "--offline")
	}
	cmd := exec.Command("cargo", args...)
	cmd.Dir = repo
	cmd.Env = append(minimalCommandEnv(), "CARGO_HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cargo %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

func preparationEnv(l paths.Layout, sc corpus.Scenario) []string {
	env := minimalCommandEnv()
	for _, declared := range sc.Fetches {
		if declared.Kind == "cargo" {
			env = append(env, "CARGO_HOME="+cargoHome(l), "CARGO_NET_OFFLINE=true")
		}
	}
	return env
}
