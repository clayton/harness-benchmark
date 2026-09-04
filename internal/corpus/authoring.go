package corpus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// NewScenarioOptions contains the small set of fields needed to start an
// original scaffold manifest. Scaffold acceptance is supplied by the private
// evaluator pack, so it may be added later without changing the public base.
type NewScenarioOptions struct {
	ID          string
	Title       string
	Description string
	Prompt      string
	Type        string
	Language    string
	Difficulty  string
}

// NewScaffoldScenario creates an original-task scenario. Scaffold scenarios
// do not need a public repository or base commit: the workspace is materialized
// by the runner and the private evaluator owns the solution.
func NewScaffoldScenario(options NewScenarioOptions, files map[string]string) (Scenario, error) {
	scenario := Scenario{
		ID: options.ID, Title: options.Title, Description: options.Description,
		Prompt: options.Prompt, Type: options.Type, Language: options.Language,
		Difficulty: options.Difficulty,
		Workspace:  Workspace{Kind: "scaffold", Files: files},
	}
	if at := strings.LastIndex(options.ID, "@"); at >= 0 {
		version, err := strconv.Atoi(options.ID[at+1:])
		if err != nil {
			return Scenario{}, fmt.Errorf("scenario version in %q is invalid: %w", options.ID, err)
		}
		scenario.Version = version
	}
	if err := ValidateScenario(scenario); err != nil {
		return Scenario{}, err
	}
	return scenario, nil
}

var authoredID = regexp.MustCompile(`\A[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*(?:@[1-9][0-9]*)?\z`)

// ValidateScenario checks the authoring contract before a manifest is handed
// to a runner or submitted to Agent Rodeo. It is deliberately local and does
// not fetch repositories or execute commands.
func ValidateScenario(s Scenario) error {
	var problems []string
	if !authoredID.MatchString(s.ID) {
		problems = append(problems, "id must be a slug or slug@version")
	}
	if strings.Contains(s.ID, "@") {
		parts := strings.SplitN(s.ID, "@", 2)
		if s.Version < 1 || fmt.Sprintf("%d", s.Version) != parts[1] {
			problems = append(problems, "version must match the @version in id")
		}
	} else if s.Version < 0 {
		problems = append(problems, "version cannot be negative")
	}
	if strings.TrimSpace(s.Title) == "" {
		problems = append(problems, "title is required")
	}
	if strings.TrimSpace(s.Prompt) == "" {
		problems = append(problems, "prompt is required")
	}
	if !slicesContains([]string{"bugfix", "feature", "refactor", "rewrite", "endurance"}, s.Type) {
		problems = append(problems, "type must be bugfix, feature, refactor, rewrite, or endurance")
	}
	if strings.TrimSpace(s.Language) == "" {
		problems = append(problems, "language is required")
	}
	if s.Workspace.Kind == "scaffold" {
		if len(s.Workspace.Files) == 0 {
			problems = append(problems, "scaffold workspace must contain at least one file")
		} else if _, err := ScaffoldPaths(s.Workspace.Files); err != nil {
			problems = append(problems, err.Error())
		}
		// A scaffold may optionally point at a public target repository, but a
		// public base is not part of the authoring contract.
		if s.Repo.BaseRef != "" && !fullSHA.MatchString(s.Repo.BaseRef) {
			problems = append(problems, "base_ref must be a 40-character commit SHA")
		}
	} else {
		if !strings.HasPrefix(s.Repo.URL, "https://github.com/") {
			problems = append(problems, "repository URL must use https://github.com/")
		}
		if !fullSHA.MatchString(s.Repo.BaseRef) {
			problems = append(problems, "base_ref must be a 40-character commit SHA")
		}
	}
	if s.Workspace.Kind != "scaffold" && len(s.Acceptance.SetupCommands)+len(s.Acceptance.BuildCommands)+len(s.Acceptance.TestCommands) == 0 && len(s.Acceptance.GoldFiles) == 0 {
		problems = append(problems, "at least one setup, build, test, or gold file is required")
	}
	for _, rel := range s.Acceptance.GoldFiles {
		if _, err := safeAuthorPath(rel); err != nil {
			problems = append(problems, fmt.Sprintf("gold file %q: %v", rel, err))
		}
	}
	for _, fetch := range s.Fetches {
		if !slicesContains([]string{"cargo", "npm", "uv", "bundler"}, fetch.Kind) {
			problems = append(problems, fmt.Sprintf("unsupported fetch kind %q", fetch.Kind))
		}
		for _, rel := range []string{fetch.Lockfile, fetch.SourceLockfile} {
			if rel == "" {
				continue
			}
			if _, err := safeAuthorPath(rel); err != nil {
				problems = append(problems, fmt.Sprintf("fetch lockfile %q: %v", rel, err))
			}
		}
		if strings.TrimSpace(fetch.Reason) == "" {
			problems = append(problems, "fetch reason is required")
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid scenario: %s", strings.Join(problems, "; "))
	}
	return nil
}

var fullSHA = regexp.MustCompile(`\A[0-9a-fA-F]{40}\z`)

func slicesContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func safeAuthorPath(raw string) (string, error) {
	clean := filepath.Clean(raw)
	if raw == "" || raw != clean || filepath.IsAbs(raw) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path must be relative and canonical")
	}
	return clean, nil
}

// WriteScenario writes a new YAML manifest without silently overwriting an
// existing version. Parent directories are created as needed.
func WriteScenario(path string, scenario Scenario) error {
	if err := ValidateScenario(scenario); err != nil {
		return err
	}
	if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
		return fmt.Errorf("scenario path must end in .yaml or .yml")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("scenario file already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	raw, err := yaml.Marshal(scenario)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// ValidateScenarioFile loads and validates a YAML authoring manifest.
func ValidateScenarioFile(path string) (Scenario, error) {
	scenario, err := LoadFile(path)
	if err != nil {
		return Scenario{}, err
	}
	if err := ValidateScenario(scenario); err != nil {
		return Scenario{}, fmt.Errorf("%s: %w", path, err)
	}
	return scenario, nil
}

// ParseWorkspaceFiles accepts the JSON object used by the web contributor
// form. Keeping this helper here makes CLI and Rails-generated manifests agree
// on the scaffold representation.
func ParseWorkspaceFiles(raw string) (map[string]string, error) {
	var files map[string]string
	if err := json.Unmarshal([]byte(raw), &files); err != nil {
		return nil, fmt.Errorf("workspace files must be a JSON object: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("workspace files must contain at least one file")
	}
	if _, err := ScaffoldPaths(files); err != nil {
		return nil, err
	}
	return files, nil
}
