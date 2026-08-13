package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuggestHeadlessUsesRunAndExecute(t *testing.T) {
	s := Snapshot{
		PathBins:   map[string]bool{"grok": true, "node": true},
		Toolchains: map[string]bool{"node": true},
		SkillNames: []string{"superpowers", "seo-audit"},
		Scenarios: []Scenario{
			{ID: "js-commander-negative-exp-E", Language: "javascript", Difficulty: "easy", Tags: []string{"smoke-ok"}},
			{ID: "go-chi-tee-bytes-double-count", Language: "go", Difficulty: "easy"},
		},
	}
	got := Suggest(s)
	if got.Kind != "execute" {
		t.Fatalf("kind=%s want execute", got.Kind)
	}
	if got.Scenario != "js-commander-negative-exp-E" {
		t.Fatalf("scenario=%s", got.Scenario)
	}
	if !strings.Contains(got.Command, "hb run -s js-commander-negative-exp-E --harness grok") {
		t.Fatalf("command=%q", got.Command)
	}
	if !strings.Contains(got.Command, "&& hb execute") {
		t.Fatalf("expected run+execute, got %q", got.Command)
	}
	if strings.Count(got.Command, "\n") != 0 {
		t.Fatalf("want exactly one command line, got %q", got.Command)
	}
	if strings.Contains(got.Command, "superpowers") || strings.Contains(got.Command, "seo-audit") {
		t.Fatalf("skills leaked into command: %q", got.Command)
	}
	if len(got.Skills) != 2 {
		t.Fatalf("skills listed = %v", got.Skills)
	}
	text := Format(got)
	if strings.Count(text, "Suggested next step") != 1 {
		t.Fatalf("format should have one suggestion block:\n%s", text)
	}
	if !strings.Contains(text, "not attached") {
		t.Fatalf("should say skills are not attached:\n%s", text)
	}
}

func TestFormatPrepareDoesNotListEmptyHarnesses(t *testing.T) {
	s := Snapshot{
		Toolchains: map[string]bool{"node": true},
		Scenarios: []Scenario{
			{ID: "js-commander-negative-exp-E", Language: "javascript", Difficulty: "easy"},
		},
	}
	got := Suggest(s)
	if got.Kind != "prepare" {
		t.Fatalf("kind=%s", got.Kind)
	}
	text := Format(got)
	if strings.Contains(text, "Harnesses (headless):") || strings.Contains(text, "Harnesses (manual only):") {
		t.Fatalf("should not list empty harness groups:\n%s", text)
	}
	if strings.Contains(text, "(none)\n") && strings.Contains(text, "Harnesses") {
		t.Fatalf("should not say harnesses are none:\n%s", text)
	}
	if !strings.Contains(text, "only prepares a workspace") {
		t.Fatalf("should say it prepares a workspace:\n%s", text)
	}
}

func TestSuggestManualOnlyPreparesWorkspace(t *testing.T) {
	s := Snapshot{
		PathBins:   map[string]bool{"cursor": true, "node": true},
		Toolchains: map[string]bool{"node": true},
		Scenarios: []Scenario{
			{ID: "js-commander-negative-exp-E", Language: "javascript", Difficulty: "easy"},
		},
	}
	got := Suggest(s)
	if got.Kind != "prepare" {
		t.Fatalf("kind=%s want prepare", got.Kind)
	}
	if strings.Contains(got.Command, "execute") {
		t.Fatalf("must not execute for GUI-only: %q", got.Command)
	}
	if !strings.Contains(got.Command, "hb run -s js-commander-negative-exp-E --harness manual") {
		t.Fatalf("command=%q", got.Command)
	}
}

func TestSuggestPicksJudgeableLanguage(t *testing.T) {
	s := Snapshot{
		PathBins:   map[string]bool{"pi": true, "go": true},
		Toolchains: map[string]bool{"go": true},
		Scenarios: []Scenario{
			{ID: "js-commander-negative-exp-E", Language: "javascript", Difficulty: "easy"},
			{ID: "go-chi-tee-bytes-double-count", Language: "go", Difficulty: "easy"},
		},
	}
	got := Suggest(s)
	if got.Scenario != "go-chi-tee-bytes-double-count" {
		t.Fatalf("scenario=%s want chi (go toolchain)", got.Scenario)
	}
}

func TestProbeUsesInjectedLookPath(t *testing.T) {
	look := func(bin string) string {
		if bin == "codex" || bin == "python3" {
			return "/fake/" + bin
		}
		return ""
	}
	s := Probe(look)
	if !s.PathBins["codex"] {
		t.Fatal("expected codex")
	}
	if !s.Toolchains["python"] {
		t.Fatal("expected python toolchain")
	}
	if s.PathBins["grok"] {
		t.Fatal("grok should be absent")
	}
}

func TestListSkillsReadsDirectoryNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "superpowers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "seo-audit"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ListSkills([]string{dir})
	if len(got) != 2 || got[0] != "seo-audit" || got[1] != "superpowers" {
		t.Fatalf("got %v", got)
	}
}
