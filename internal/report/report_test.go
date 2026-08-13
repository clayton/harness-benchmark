package report

import (
	"os"
	"strings"
	"testing"

	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func TestWriteLocalHTMLDoesNotMentionUpload(t *testing.T) {
	l := paths.New(t.TempDir(), t.TempDir())
	rec := loop.RunRecord{ID: "aaaabbbbcccc", ScenarioID: "js-commander-negative-exp-E", Status: "completed", Harness: "grok", CreatedAt: loop.Now()}
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
}
