package corpus

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/clayton/harness-benchmark/internal/doctor"
	"gopkg.in/yaml.v3"
)

//go:embed scenarios/*.yaml
var embedded embed.FS

type Repo struct {
	URL              string `yaml:"url" json:"url"`
	BaseRef          string `yaml:"base_ref" json:"base_ref"`
	GoldRef          string `yaml:"gold_ref" json:"gold_ref"`
	EnvironmentPatch string `yaml:"environment_patch" json:"environment_patch,omitempty"`
}

type Acceptance struct {
	SetupCommands []string `yaml:"setup_commands" json:"setup_commands"`
	TestCommands  []string `yaml:"test_commands" json:"test_commands"`
	BuildCommands []string `yaml:"build_commands" json:"build_commands"`
	FailToPass    []string `yaml:"fail_to_pass" json:"fail_to_pass"`
	GoldFiles     []string `yaml:"gold_files" json:"gold_files,omitempty"`
}

type Scenario struct {
	ID          string     `yaml:"id" json:"id"`
	Type        string     `yaml:"type" json:"type"`
	Title       string     `yaml:"title" json:"title"`
	Description string     `yaml:"description" json:"description"`
	Prompt      string     `yaml:"prompt" json:"prompt"`
	Language    string     `yaml:"language" json:"language"`
	Tags        []string   `yaml:"tags" json:"tags"`
	Difficulty  string     `yaml:"difficulty" json:"difficulty"`
	Repo        Repo       `yaml:"repo" json:"repo"`
	Acceptance  Acceptance `yaml:"acceptance" json:"acceptance"`
	SourceDir   string     `yaml:"-" json:"source_dir,omitempty"`
}

func (s Scenario) ToDoctor() doctor.Scenario {
	return doctor.Scenario{
		ID:         s.ID,
		Language:   s.Language,
		Difficulty: s.Difficulty,
		Tags:       s.Tags,
	}
}

func EnsureCache(destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	entries, err := fs.ReadDir(embedded, "scenarios")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if e.Name() == "example-synthetic-bugfix.yaml" {
			continue
		}
		data, err := embedded.ReadFile("scenarios/" + e.Name())
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, e.Name())
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func Load(dir string) ([]Scenario, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if nested, err := filepath.Glob(filepath.Join(dir, "*", "*.yaml")); err == nil {
		matches = append(matches, nested...)
	}
	var out []Scenario
	for _, path := range matches {
		if strings.Contains(filepath.Base(path), "example-synthetic") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var s Scenario
		if err := yaml.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if s.ID == "" {
			continue
		}
		s.SourceDir = filepath.Dir(path)
		out = append(out, s)
	}
	return out, nil
}

func Find(dir, id string) (Scenario, error) {
	all, err := Load(dir)
	if err != nil {
		return Scenario{}, err
	}
	for _, s := range all {
		if s.ID == id {
			return s, nil
		}
	}
	return Scenario{}, fmt.Errorf("scenario not found: %s", id)
}

func LoadFile(path string) (Scenario, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, err
	}
	var s Scenario
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return Scenario{}, fmt.Errorf("%s: %w", path, err)
	}
	if s.ID == "" {
		return Scenario{}, fmt.Errorf("%s: missing id", path)
	}
	s.SourceDir = filepath.Dir(path)
	return s, nil
}

func Resolve(officialDir, from, idOrPath string) (Scenario, error) {
	if idOrPath != "" && (strings.HasSuffix(idOrPath, ".yaml") || strings.HasSuffix(idOrPath, ".yml") || strings.Contains(idOrPath, string(filepath.Separator))) {
		return LoadFile(idOrPath)
	}
	if from != "" {
		return Find(from, idOrPath)
	}
	return Find(officialDir, idOrPath)
}

// UnmarshalScenarioJSON loads a scenario from a snapshot. New snapshots use the
// same snake_case keys as the YAML. Older snapshots used Go field names
// (Acceptance, TestCommands); those still load.
func UnmarshalScenarioJSON(raw []byte) (Scenario, error) {
	norm, err := rewriteLegacyScenarioJSON(raw)
	if err != nil {
		return Scenario{}, err
	}
	var s Scenario
	if err := json.Unmarshal(norm, &s); err != nil {
		return Scenario{}, err
	}
	return s, nil
}

var scenarioJSONAlias = map[string]string{
	"ID": "id", "Type": "type", "Title": "title", "Description": "description",
	"Prompt": "prompt", "Language": "language", "Tags": "tags",
	"Difficulty": "difficulty", "Repo": "repo", "Acceptance": "acceptance",
	"SourceDir": "source_dir", "URL": "url", "BaseRef": "base_ref",
	"GoldRef": "gold_ref", "EnvironmentPatch": "environment_patch",
	"SetupCommands": "setup_commands", "TestCommands": "test_commands",
	"BuildCommands": "build_commands", "FailToPass": "fail_to_pass",
	"GoldFiles": "gold_files",
}

func rewriteLegacyScenarioJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(rewriteScenarioKeys(v))
}

func rewriteScenarioKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if nk, ok := scenarioJSONAlias[k]; ok {
				k = nk
			}
			out[k] = rewriteScenarioKeys(val)
		}
		return out
	case []any:
		for i, item := range t {
			t[i] = rewriteScenarioKeys(item)
		}
		return t
	default:
		return v
	}
}

func DoctorScenarios(dir string) []doctor.Scenario {
	all, err := Load(dir)
	if err != nil {
		return nil
	}
	out := make([]doctor.Scenario, 0, len(all))
	for _, s := range all {
		out = append(out, s.ToDoctor())
	}
	return out
}
