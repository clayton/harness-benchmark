package loop

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/fetchconsent"
	"github.com/clayton/harness-benchmark/internal/paths"
)

var versionPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9])v?([0-9]+)\.([0-9]+)(?:\.([0-9]+))?\b`)
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

func InputPlan(l paths.Layout, sc corpus.Scenario, includeDependencies bool) (fetchconsent.Plan, error) {
	if includeDependencies {
		if err := validateSetupFetches(sc); err != nil {
			return fetchconsent.Plan{}, err
		}
	}
	repo := ""
	var items []fetchconsent.Item
	if sc.Repo.URL != "" {
		repo = filepath.Join(l.ReposDir(), repoSlug(sc.Repo.URL))
		items = append(items, fetchconsent.Item{
			Kind: "Git repository", Source: sc.Repo.URL, Ref: sc.Repo.BaseRef,
			Reason: "benchmark source", Destination: repo, Size: "unknown",
		})
		if sc.Repo.GoldRef != "" {
			items = append(items, fetchconsent.Item{
				Kind: "Git repository", Source: sc.Repo.URL, Ref: sc.Repo.GoldRef,
				Reason: "judge reference", Destination: repo, Size: "unknown",
			})
		}
	}
	if includeDependencies {
		for _, declared := range sc.Fetches {
			if repo == "" {
				return fetchconsent.Plan{}, fmt.Errorf("declared fetch %q requires a repository", declared.Kind)
			}
			dependencyItems, err := lockedDependencyItems(l, repo, sc, declared)
			if err != nil {
				return fetchconsent.Plan{}, err
			}
			items = append(items, dependencyItems...)
		}
	}
	return fetchconsent.New(items...), nil
}

func PrepareInputs(l paths.Layout, sc corpus.Scenario, includeDependencies bool, authorize FetchAuthorizer) error {
	plan, err := InputPlan(l, sc, includeDependencies)
	if err != nil {
		return err
	}
	if len(plan.Items) > 0 {
		if err := authorize(plan); err != nil {
			return err
		}
	}
	return PrepareApprovedInputs(l, sc, includeDependencies)
}

func PrepareApprovedInputs(l paths.Layout, sc corpus.Scenario, includeDependencies bool) error {
	repo := ""
	if sc.Repo.URL != "" {
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
		if err := verifyBundledDependencyLock(repo, sc, declared); err != nil {
			return err
		}
		if declared.Kind == "cargo" {
			if err := prepareCargoDependencies(l, repo, sc, declared); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSetupFetches(sc corpus.Scenario) error {
	declared := map[string]bool{}
	for _, fetch := range sc.Fetches {
		declared[fetch.Kind] = true
	}
	for _, command := range sc.Acceptance.SetupCommands {
		lower := strings.ToLower(command)
		unsafe := (strings.Contains(lower, "pip install") && !strings.Contains(lower, "--no-index")) ||
			((strings.Contains(lower, "npm install") || strings.Contains(lower, "npm ci")) && !strings.Contains(lower, "--offline") && !declared["npm"]) ||
			(strings.Contains(lower, "uv sync") && !strings.Contains(lower, "--offline") && !declared["uv"]) ||
			(strings.Contains(lower, "bundle install") && !declared["bundler"]) ||
			(strings.Contains(lower, "pnpm install") && !strings.Contains(lower, "--offline")) ||
			(strings.Contains(lower, "yarn install") && !strings.Contains(lower, "--offline")) ||
			(strings.Contains(lower, "cargo build") && !strings.Contains(lower, "--offline")) ||
			strings.Contains(lower, "cargo install") || strings.Contains(lower, "go mod download") ||
			strings.Contains(lower, "git clone") || strings.Contains(lower, "git fetch") ||
			strings.Contains(lower, "apt-get ") || strings.Contains(lower, "brew install") ||
			strings.Contains(lower, "curl ") || strings.Contains(lower, "wget ")
		if unsafe {
			return fmt.Errorf("setup command can download or install software without a supported fetch plan: %q\nhbench did not run setup or access the network", command)
		}
	}
	return nil
}

func lockedDependencyItems(l paths.Layout, repo string, sc corpus.Scenario, declared corpus.Fetch) ([]fetchconsent.Item, error) {
	if declared.Kind != "cargo" && declared.Kind != "npm" && declared.Kind != "uv" && declared.Kind != "bundler" {
		return nil, fmt.Errorf("unsupported declared fetch kind %q", declared.Kind)
	}
	raw, err := dependencyLock(repo, sc, declared)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	checksum := "sha256:" + hex.EncodeToString(sum[:])
	sources := dependencySources(declared.Kind, raw)
	items := make([]fetchconsent.Item, 0, len(sources))
	for _, source := range sources {
		destination := dependencyCache(l, declared.Kind, raw)
		if declared.Kind == "cargo" {
			destination = cargoHome(l)
		}
		items = append(items, fetchconsent.Item{
			Kind: strings.ToUpper(declared.Kind) + " dependencies", Source: source,
			Ref: checksum, Checksum: checksum, Reason: declared.Reason,
			Destination: destination, Size: "unknown",
		})
	}
	return items, nil
}

func verifyBundledDependencyLock(repo string, sc corpus.Scenario, declared corpus.Fetch) error {
	if declared.SourceLockfile == "" {
		return nil
	}
	bundled, err := os.ReadFile(filepath.Join(sc.SourceDir, filepath.Clean(declared.SourceLockfile)))
	if err != nil {
		return fmt.Errorf("read source lockfile %q: %w", declared.SourceLockfile, err)
	}
	cmd := exec.Command("git", "-C", repo, "show", sc.Repo.BaseRef+":"+filepath.ToSlash(filepath.Clean(declared.Lockfile)))
	frozen, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("read %s at frozen ref %s: %w", declared.Lockfile, sc.Repo.BaseRef, err)
	}
	if !bytes.Equal(bundled, frozen) {
		return fmt.Errorf("bundled source lockfile %q does not match %s at frozen ref %s", declared.SourceLockfile, declared.Lockfile, sc.Repo.BaseRef)
	}
	return nil
}

func dependencyLock(repo string, sc corpus.Scenario, declared corpus.Fetch) ([]byte, error) {
	lockfile := filepath.Clean(declared.Lockfile)
	if lockfile == "." || filepath.IsAbs(lockfile) || lockfile == ".." || strings.HasPrefix(lockfile, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("unsafe %s lockfile %q", declared.Kind, declared.Lockfile)
	}
	if repo != "" {
		cmd := exec.Command("git", "-C", repo, "show", sc.Repo.BaseRef+":"+filepath.ToSlash(lockfile))
		if raw, err := cmd.Output(); err == nil {
			return raw, nil
		}
	}
	if declared.SourceLockfile == "" || sc.SourceDir == "" {
		return nil, fmt.Errorf("read %s lockfile %q at %s", declared.Kind, declared.Lockfile, sc.Repo.BaseRef)
	}
	raw, err := readRooted(sc.SourceDir, declared.SourceLockfile)
	if err != nil {
		return nil, fmt.Errorf("read %s source lockfile: %w", declared.Kind, err)
	}
	return raw, nil
}

var dependencyURL = regexp.MustCompile(`https?://[^\s"']+`)

func dependencySources(kind string, lock []byte) []string {
	var rawSources []string
	switch kind {
	case "npm":
		var value any
		if json.Unmarshal(lock, &value) == nil {
			collectNPMResolved(value, &rawSources)
		}
	case "bundler":
		for _, line := range strings.Split(string(lock), "\n") {
			if source, ok := strings.CutPrefix(strings.TrimSpace(line), "remote: "); ok {
				rawSources = append(rawSources, source)
			}
		}
	default:
		rawSources = dependencyURL.FindAllString(string(lock), -1)
	}
	seen := map[string]bool{}
	for _, raw := range rawSources {
		u, err := url.Parse(strings.TrimRight(raw, ",;)]}"))
		if err == nil && u.Host != "" {
			seen[u.Scheme+"://"+u.Host] = true
		}
	}
	if len(seen) == 0 {
		defaults := map[string]string{"cargo": "https://crates.io", "npm": "https://registry.npmjs.org", "uv": "https://pypi.org", "bundler": "https://rubygems.org"}
		seen[defaults[kind]] = true
	}
	out := make([]string, 0, len(seen))
	for source := range seen {
		out = append(out, source)
	}
	sort.Strings(out)
	return out
}

func collectNPMResolved(value any, out *[]string) {
	switch value := value.(type) {
	case map[string]any:
		if resolved, ok := value["resolved"].(string); ok {
			*out = append(*out, resolved)
		}
		for _, child := range value {
			collectNPMResolved(child, out)
		}
	case []any:
		for _, child := range value {
			collectNPMResolved(child, out)
		}
	}
}

func dependencyCache(l paths.Layout, kind string, lock []byte) string {
	sum := sha256.Sum256(lock)
	return filepath.Join(l.DataDir, "dependencies", kind, hex.EncodeToString(sum[:]))
}

func cargoHome(l paths.Layout) string {
	return filepath.Join(l.DataDir, "dependencies", "cargo")
}

func prepareCargoDependencies(l paths.Layout, repo string, sc corpus.Scenario, declared corpus.Fetch) error {
	lockfile := filepath.Clean(declared.Lockfile)
	if lockfile == "." || filepath.IsAbs(lockfile) || lockfile == ".." || strings.HasPrefix(lockfile, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe cargo lockfile %q", declared.Lockfile)
	}
	checkout, cleanup, err := dependencyCheckout(repo, sc.Repo.BaseRef)
	if err != nil {
		return err
	}
	defer cleanup()
	lockPath := filepath.Join(checkout, lockfile)
	if _, err := os.ReadFile(lockPath); err != nil {
		return fmt.Errorf("read cargo lockfile: %w", err)
	}
	manifest := filepath.Join(checkout, "Cargo.toml")
	home := cargoHome(l)
	if cargoFetch(checkout, manifest, home, true) == nil {
		return nil
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	if err := cargoFetch(checkout, manifest, home, false); err != nil {
		return fmt.Errorf("fetch approved Cargo dependencies: %w", err)
	}
	return nil
}

func dependencyCheckout(repo, ref string) (string, func(), error) {
	root, err := os.MkdirTemp("", "hbench-dependencies-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	checkout := filepath.Join(root, "repo")
	if err := runGit("", "clone", "--quiet", "--no-hardlinks", repo, checkout); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := git(checkout, "checkout", "--quiet", "--detach", ref); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return checkout, cleanup, nil
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

func preparationEnv(l paths.Layout, sc corpus.Scenario, workspace string) []string {
	env := minimalCommandEnv()
	for _, declared := range sc.Fetches {
		if declared.Kind == "cargo" {
			env = append(env, "CARGO_HOME="+cargoHome(l), "CARGO_NET_OFFLINE=true")
			continue
		}
		lock, err := readRooted(workspace, declared.Lockfile)
		if err != nil {
			continue
		}
		cache := dependencyCache(l, declared.Kind, lock)
		switch declared.Kind {
		case "npm":
			env = append(env, "NPM_CONFIG_CACHE="+cache)
		case "uv":
			env = append(env, "UV_CACHE_DIR="+cache, "UV_PYTHON_DOWNLOADS=never")
		case "bundler":
			env = append(env, "BUNDLE_PATH="+cache)
		}
	}
	return env
}
