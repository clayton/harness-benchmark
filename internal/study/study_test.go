package study

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func valid() Manifest {
	return Manifest{Schema: Schema, ID: "test", Question: "Which wins?", ComparisonMode: "controlled", Scenarios: []Scenario{{ID: "rodeo:one@1", Digest: strings.Repeat("a", 64)}}, Arms: []Arm{{ID: "a", Harness: "codex", Version: "test", Model: "same"}, {ID: "b", Harness: "pi", Version: "test", Model: "same"}}, VariedAxes: []string{"harness"}, Repeats: 3, JudgeProtocol: "scenario-default", WinRule: WinRule, Budget: Budget{MaxMinutes: 45}}
}

func TestRejectsLocalScenarioIDsBeforeExecution(t *testing.T) {
	m := valid()
	m.Scenarios[0].ID = "one"
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "rodeo:slug@version") {
		t.Fatalf("got %v", err)
	}
}

func TestControlledRejectsUndeclaredDifference(t *testing.T) {
	m := valid()
	m.Arms[1].Workflow = "swarm"
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "workflow") {
		t.Fatalf("got %v", err)
	}
}

func TestEcologicalAllowsBundleDifference(t *testing.T) {
	m := valid()
	m.ComparisonMode = "ecological"
	m.Arms[1].Workflow = "swarm"
	m.VariedAxes = []string{"harness", "workflow"}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEcologicalRejectsUndisclosedBundleDifference(t *testing.T) {
	m := valid()
	m.ComparisonMode = "ecological"
	m.Arms[1].Workflow = "swarm"
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "workflow") {
		t.Fatalf("got %v", err)
	}
}

func TestRejectsDollarPrecisionThatCannotRoundTripAcrossContracts(t *testing.T) {
	m := valid()
	limit := 0.0000001
	m.Budget.MaxUSDPerRun = &limit
	if err := m.Validate(); err == nil {
		t.Fatal("sub-micro-dollar budget was accepted")
	}
}
func TestYAMLTags(t *testing.T) {
	raw, err := yaml.Marshal(valid())
	if err != nil || !strings.Contains(string(raw), "comparison_mode: controlled") {
		t.Fatalf("%v %s", err, raw)
	}
}

func TestDigestMatchesRailsCanonicalJSON(t *testing.T) {
	const expected = "34799cbeabed8b5b394aa66abe7f3f85c324b2c4dfb3193652a38a290a7ef286"
	if got := valid().Digest(); got != expected {
		t.Fatalf("digest=%s want=%s", got, expected)
	}
}

func TestDigestMatchesRailsForHTMLUnicodeAndDecimalValues(t *testing.T) {
	m := valid()
	m.Question = "A & B < C > D \u2028 \u2029"
	m.Sources = []Source{{URL: "https://example.com/?a=1&b=2", Author: "A&B"}}
	limit := 0.125
	m.Budget.MaxUSDPerRun = &limit
	const expected = "ddc33f9af6a0718c99a6fd2119f6ffcae0fe2fae1c0e8d21cf90df77e57dca15"
	if got := m.Digest(); got != expected {
		t.Fatalf("digest=%s want=%s", got, expected)
	}
}

func TestLoadRejectsUnknownBudgetFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "study.yaml")
	raw, err := yaml.Marshal(valid())
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "    max_minutes_per_run: 45\n", "    max_minutes_per_run: 45\n    max_token_per_run: 10\n", 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "max_token_per_run") {
		t.Fatalf("got %v", err)
	}
}
