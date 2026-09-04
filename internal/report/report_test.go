package report

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
	studycontract "github.com/clayton/harness-benchmark/internal/study"
)

func TestWriteLocalHTMLDoesNotMentionUpload(t *testing.T) {
	l := paths.New(t.TempDir(), t.TempDir())
	rec := loop.RunRecord{ID: "aaaabbbbcccc", ScenarioID: "js-commander-negative-exp-E", Status: "completed", Worktree: l.Worktree("aaaabbbbcccc"), Harness: "grok", CreatedAt: loop.Now()}
	if err := loop.Save(l, rec); err != nil {
		t.Fatal(err)
	}
	path, n, err := Write(l)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "aaaabbbbcccc") {
		t.Fatalf("report missing run id:\n%s", body)
	}
	if !strings.Contains(body, "Nothing was uploaded") {
		t.Fatal("report should say it did not upload")
	}
	if !strings.Contains(body, "hbench publish") {
		t.Fatal("report should mention optional publish")
	}
	if !strings.Contains(body, "quality") {
		t.Fatal("report should show quality")
	}
	if !strings.Contains(body, `Content-Security-Policy`) || !strings.Contains(body, `href="./aaaabbbbcccc/patch.diff"`) {
		t.Fatalf("report lacks CSP or rooted artifact link: %s", body)
	}
}

func TestWriteStudyComparisonShowsRepeatsCostsAndReproduction(t *testing.T) {
	l := paths.New(t.TempDir(), t.TempDir())
	id := "abcdeffedcba"
	worktree := l.Worktree(id)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	cost, complete := 0.25, true
	passed := true
	rec := loop.RunRecord{ID: id, ScenarioID: "rodeo:task@1", ConfigID: "arm-a", Status: "completed", Worktree: worktree, Harness: "pi", CreatedAt: loop.Now(),
		Judges: []loop.JudgeScore{{Name: "test", Passed: &passed}}, Telemetry: loop.Telemetry{EstimatedUSD: &cost, CostKind: "actual", Complete: &complete},
		Metadata: map[string]any{"profile": map[string]any{"arm_id": "arm-a", "study_id": "fight"}}}
	if err := loop.Save(l, rec); err != nil {
		t.Fatal(err)
	}
	m := studycontract.Manifest{ID: "fight", Question: "Which wins?", Scenarios: []studycontract.Scenario{{ID: "rodeo:task@1"}}, Arms: []studycontract.Arm{{ID: "arm-a"}, {ID: "arm-b"}}, Repeats: 1}
	path, _, err := WriteStudyComparison(l, m, map[string]bool{id: true}, "fight.yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{"Study comparison", "1/1", "$0.250000", "incomplete", "hbench study run fight.yaml --approve-spend"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in report:\n%s", want, body)
		}
	}
}

func TestWriteSkipsUnvalidatedRunRecords(t *testing.T) {
	l := paths.New(t.TempDir(), t.TempDir())
	attacks := []string{
		"javascript:alert(1)",
		"data:text/html,evil",
		`bad" onclick="alert(1)`,
		"%0ajavascript:alert(1)",
	}
	for _, attack := range attacks {
		badDir := filepath.Join(l.OutDir, attack)
		if err := os.MkdirAll(badDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(badDir, "run.json"), []byte(`{"id":`+strconv.Quote(attack)+`}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	traversalDir := filepath.Join(l.OutDir, "deadbeef0001")
	if err := os.MkdirAll(traversalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(traversalDir, "run.json"), []byte(`{"id":"../../evil"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path, n, err := Write(l)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("unsafe record rendered: n=%d %s", n, raw)
	}
	for _, attack := range append(attacks, "../../evil") {
		if strings.Contains(string(raw), attack) {
			t.Fatalf("unsafe record %q rendered: %s", attack, raw)
		}
	}
}
