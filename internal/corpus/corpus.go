package corpus

import (
	"embed"
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
	URL     string `yaml:"url"`
	BaseRef string `yaml:"base_ref"`
	GoldRef string `yaml:"gold_ref"`
}

type Acceptance struct {
	SetupCommands []string `yaml:"setup_commands"`
	TestCommands  []string `yaml:"test_commands"`
	BuildCommands []string `yaml:"build_commands"`
}

type Scenario struct {
	ID          string     `yaml:"id"`
	Type        string     `yaml:"type"`
	Title       string     `yaml:"title"`
	Description string     `yaml:"description"`
	Prompt      string     `yaml:"prompt"`
	Language    string     `yaml:"language"`
	Tags        []string   `yaml:"tags"`
	Difficulty  string     `yaml:"difficulty"`
	Repo        Repo       `yaml:"repo"`
	Acceptance  Acceptance `yaml:"acceptance"`
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
