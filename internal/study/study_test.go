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

func TestPrivateAllowsLocalScenarioIDs(t *testing.T) {
	m := valid()
	m.Visibility = "private"
	m.Scenarios[0].ID = "scenarios/local.yaml"
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsUnknownVisibility(t *testing.T) {
	m := valid()
	m.Visibility = "internal"
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "visibility") {
		t.Fatalf("got %v", err)
	}
}

func TestPublicRejectsPersonalSetupFields(t *testing.T) {
	m := valid()
	m.Arms[0].Mode = "personal"
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "private studies") {
		t.Fatalf("got %v", err)
	}
}

func TestLocalSkillPathsDoNotDefineIdentityButContentDoes(t *testing.T) {
	m := valid()
	m.Visibility = "private"
	m.Arms[0].Harness = "pi"
	m.Arms[1].Harness = m.Arms[0].Harness
	m.Arms[0].Mode = "personal"
	m.Arms[1].Mode = "personal"
	m.VariedAxes = []string{"skills"}
	m.Arms[0].LocalSkills = []string{"/private/one"}
	m.Arms[0].LocalSkillDigests = []string{strings.Repeat("b", 64)}
	m.Arms[1].LocalSkills = []string{"/private/two"}
	m.Arms[1].LocalSkillDigests = []string{strings.Repeat("c", 64)}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if !contains(m.DifferingAxes(), "skills") {
		t.Fatalf("local content difference was not an axis: %v", m.DifferingAxes())
	}
	left := m.Digest()
	m.Arms[0].LocalSkills[0] = "/another/path"
	if got := m.Digest(); got != left {
		t.Fatalf("path changed contract digest: %s != %s", got, left)
	}
	m.Arms[0].LocalSkillDigests[0] = strings.Repeat("d", 64)
	if got := m.Digest(); got == left {
		t.Fatal("local skill content digest did not change contract digest")
	}
}

func TestPrivateModeIsAComparisonAxis(t *testing.T) {
	m := valid()
	m.Visibility = "private"
	m.Arms[0].Mode = "clean-baseline"
	m.Arms[1].Mode = "personal"
	m.VariedAxes = []string{"harness", "mode"}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if !contains(m.DifferingAxes(), "mode") {
		t.Fatalf("mode difference was not an axis: %v", m.DifferingAxes())
	}
}

func TestPrivatePersonalCodexNeedsConfigFingerprint(t *testing.T) {
	m := valid()
	m.Visibility = "private"
	m.Arms[0].Mode = "personal"
	m.Arms[0].Harness = "codex"
	m.Arms[1].Mode = "personal"
	m.VariedAxes = []string{"harness", "config"}
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "config fingerprint") {
		t.Fatalf("got %v", err)
	}
	m.Arms[0].ConfigStatus = "missing"
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	m.Arms[1].ConfigStatus = "missing"
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "only supported for personal codex") {
		t.Fatalf("got %v", err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestAcceptsPublishedScenarioIDWithUppercase(t *testing.T) {
	m := valid()
	m.Scenarios[0].ID = "rodeo:js-commander-negative-exp-E@1"
	if err := m.Validate(); err != nil {
		t.Fatal(err)
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
