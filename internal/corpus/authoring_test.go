package corpus

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNewScaffoldScenarioAndRoundTrip(t *testing.T) {
	scenario, err := NewScaffoldScenario(NewScenarioOptions{
		ID: "original-task", Title: "Original task", Prompt: "Build the thing", Type: "feature", Language: "Go",
	}, map[string]string{"README.md": "start\n", "cmd/main.go": "package main\n"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "original-task.yaml")
	if err := WriteScenario(path, scenario); err != nil {
		t.Fatal(err)
	}
	got, err := ValidateScenarioFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Workspace.Kind != "scaffold" || got.Workspace.Files["cmd/main.go"] == "" {
		t.Fatalf("scaffold did not round-trip: %+v", got.Workspace)
	}
	if err := WriteScenario(path, scenario); err == nil {
		t.Fatal("write silently replaced an existing manifest")
	}
}

func TestValidateScenarioRejectsUnsafeAndIncompleteManifests(t *testing.T) {
	base := Scenario{ID: "task", Title: "Task", Prompt: "Fix", Type: "bugfix", Language: "Go", Repo: Repo{URL: "https://github.com/example/project", BaseRef: strings.Repeat("a", 40)}}
	if err := ValidateScenario(base); err == nil {
		t.Fatal("accepted scenario without an acceptance contract")
	}
	base.Acceptance.TestCommands = []string{"go test ./..."}
	base.Acceptance.GoldFiles = []string{"../secret"}
	if err := ValidateScenario(base); err == nil {
		t.Fatal("accepted unsafe gold path")
	}
	base.Acceptance.GoldFiles = nil
	base.Workspace = Workspace{Kind: "scaffold", Files: map[string]string{".git/config": "no"}}
	base.Repo = Repo{}
	if err := ValidateScenario(base); err == nil {
		t.Fatal("accepted unsafe scaffold")
	}
	base.Workspace = Workspace{}
	base.Acceptance = Acceptance{TestCommands: []string{"go test ./..."}}
	base.ID, base.Version = "task@2", 1
	base.Fetches = []Fetch{{Kind: "npm", Lockfile: "package-lock.json", Reason: ""}}
	if err := ValidateScenario(base); err == nil {
		t.Fatal("accepted inconsistent version and fetch")
	}
}

func TestParseWorkspaceFiles(t *testing.T) {
	files, err := ParseWorkspaceFiles(`{"README.md":"hello\n"}`)
	if err != nil || files["README.md"] != "hello\n" {
		t.Fatalf("files=%v err=%v", files, err)
	}
	if _, err := ParseWorkspaceFiles(`{"../escape":"x"}`); err == nil {
		t.Fatal("accepted escaping workspace file")
	}
}
