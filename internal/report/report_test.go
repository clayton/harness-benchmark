package report

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
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
