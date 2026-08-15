package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clayton/harness-benchmark/internal/corpus"
)

func capture(t *testing.T, args []string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := run(args)
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if runErr != nil {
		t.Fatalf("run %v: %v\n%s", args, runErr, out)
	}
	return string(out)
}

func TestVersionSaysGo(t *testing.T) {
	out := capture(t, []string{"version"})
	if !strings.Contains(out, "go") || !strings.Contains(out, "hb") {
		t.Fatalf("version: %q", out)
	}
}

func TestBarePrintsOnlyTheSuggestedCommand(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(filepath.Dir(cwd)) })
	out := capture(t, nil)
	if strings.Contains(out, "hbench doctor") {
		t.Fatalf("bare hbench should not dump doctor:\n%s", out)
	}
	if strings.Contains(out, "Harnesses") {
		t.Fatalf("bare hbench should not list harnesses:\n%s", out)
	}
	if !strings.Contains(out, "hbench run -s") {
		t.Fatalf("bare hbench should print one run command:\n%s", out)
	}
	if strings.Count(strings.TrimSpace(out), "\n") > 1 {
		t.Fatalf("want one command line, got:\n%s", out)
	}
}

func TestDoctorPrintsFullReport(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(filepath.Dir(cwd)) })
	out := capture(t, []string{"doctor"})
	if !strings.Contains(out, "hbench doctor") {
		t.Fatalf("missing doctor header:\n%s", out)
	}
	if !strings.Contains(out, "not attached") {
		t.Fatalf("must say skills are not attached:\n%s", out)
	}
	if strings.Count(out, "Suggested next step") != 1 {
		t.Fatalf("want one suggestion:\n%s", out)
	}
}

func TestRunHelpExitsZero(t *testing.T) {
	out := capture(t, []string{"run", "--help"})
	if !strings.Contains(out, "hbench run -s") {
		t.Fatalf("run help:\n%s", out)
	}
}

func TestCommunitySetupRefusesUnattendedInput(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	old := os.Stdin
	os.Stdin = read
	t.Cleanup(func() { os.Stdin = old })

	err = confirmCommunitySetup(corpus.Scenario{ID: "community@1", Acceptance: corpus.Acceptance{SetupCommands: []string{"bundle install"}}})
	if err == nil || !strings.Contains(err.Error(), "refusing unreviewed setup") {
		t.Fatalf("expected unattended setup refusal, got %v", err)
	}
}

func TestPublishHelpDoesNotUpload(t *testing.T) {
	out := capture(t, []string{"publish", "--help"})
	if !strings.Contains(out, "not automatic") {
		t.Fatalf("help: %q", out)
	}
}

func TestListScenariosFromEmbeddedCorpus(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	out := capture(t, []string{"list", "scenarios"})
	if !bytes.Contains([]byte(out), []byte("js-commander-negative-exp-E")) {
		t.Fatalf("list:\n%s", out)
	}
}

func captureResult(t *testing.T, args []string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := run(args)
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), runErr
}

func TestFinishHelpIsHelp(t *testing.T) {
	out := capture(t, []string{"finish", "--help"})
	if !strings.Contains(out, "hbench finish") {
		t.Fatalf("finish help:\n%s", out)
	}
	if strings.Contains(out, "run not found") {
		t.Fatalf("--help must not be parsed as a run id:\n%s", out)
	}
	if !strings.Contains(out, "--force") {
		t.Fatalf("finish help should mention --force:\n%s", out)
	}
}

func TestExecuteHelpIsHelp(t *testing.T) {
	out := capture(t, []string{"execute", "--help"})
	if !strings.Contains(out, "hbench execute") {
		t.Fatalf("execute help:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "spend tokens") {
		t.Fatalf("execute help should not pitch tokens:\n%s", out)
	}
}

func TestBareAfterPendingSuggestsFinish(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(filepath.Dir(cwd)) })
	if err := os.MkdirAll(filepath.Join(cwd, "hb-out", "cafebabe"), 0o755); err != nil {
		t.Fatal(err)
	}
	runJSON := `{
  "id": "cafebabe",
  "scenario_id": "go-chi-tee-bytes-double-count",
  "status": "pending",
  "harness": "manual",
  "created_at": "2026-08-13T00:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(cwd, "hb-out", "cafebabe", "run.json"), []byte(runJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "hb-out", "latest"), []byte("cafebabe"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, nil)
	if strings.TrimSpace(out) != "hbench finish cafebabe" {
		t.Fatalf("pending next step: %q", out)
	}
}

func TestBareAfterCompletedSuggestsReport(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(filepath.Dir(cwd)) })
	if err := os.MkdirAll(filepath.Join(cwd, "hb-out", "cafebabe"), 0o755); err != nil {
		t.Fatal(err)
	}
	runJSON := `{
  "id": "cafebabe",
  "scenario_id": "go-chi-tee-bytes-double-count",
  "status": "completed",
  "harness": "manual",
  "created_at": "2026-08-13T00:00:00Z",
  "finished_at": "2026-08-13T00:01:00Z"
}
`
	if err := os.WriteFile(filepath.Join(cwd, "hb-out", "cafebabe", "run.json"), []byte(runJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "hb-out", "latest"), []byte("cafebabe"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, nil)
	if strings.TrimSpace(out) != "hbench report" {
		t.Fatalf("completed next step: %q", out)
	}
}

func TestReportMissingStoreNamesLookPath(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(filepath.Dir(cwd)) })
	_, err := captureResult(t, []string{"report"})
	if err == nil {
		t.Fatal("report with no hb-out should fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, cwd) {
		t.Fatalf("should name the look directory %s: %v", cwd, err)
	}
	if !strings.Contains(msg, "nothing was uploaded") {
		t.Fatalf("should still say nothing uploaded: %v", err)
	}
	if strings.Contains(msg, "No ./hb-out yet") {
		t.Fatalf("old fresh-install copy leaked: %v", err)
	}
}

func TestFinishFromWorkspaceFindsAncestorRun(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	start := t.TempDir()
	ws := filepath.Join(start, "hb-out", "cafebabe", "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	runJSON := `{
  "id": "cafebabe",
  "scenario_id": "does-not-exist",
  "status": "pending",
  "harness": "manual",
  "created_at": "2026-08-13T00:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(start, "hb-out", "cafebabe", "run.json"), []byte(runJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(start, "hb-out", "latest"), []byte("cafebabe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(start) })
	_, err := captureResult(t, []string{"finish"})
	if err == nil {
		t.Fatal("expected scenario miss after loading the run")
	}
	if strings.Contains(err.Error(), "run not found") {
		t.Fatalf("should resolve the run from the workspace ancestor hb-out: %v", err)
	}
	if !strings.Contains(err.Error(), "scenario not found") {
		t.Fatalf("should load the ancestor run: %v", err)
	}
}

func TestExecuteManualDoesNotTalkAboutTokens(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(filepath.Dir(cwd)) })
	if err := os.MkdirAll(filepath.Join(cwd, "hb-out", "cafebabe"), 0o755); err != nil {
		t.Fatal(err)
	}
	runJSON := `{
  "id": "cafebabe",
  "scenario_id": "go-chi-tee-bytes-double-count",
  "status": "pending",
  "harness": "manual",
  "created_at": "2026-08-13T00:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(cwd, "hb-out", "cafebabe", "run.json"), []byte(runJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "hb-out", "latest"), []byte("cafebabe"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := captureResult(t, []string{"execute", "cafebabe"})
	if err == nil {
		t.Fatal("manual execute should fail")
	}
	if strings.Contains(out, "spend tokens") || strings.Contains(err.Error(), "spend tokens") {
		t.Fatalf("manual path mentioned tokens:\n%s\n%v", out, err)
	}
	if !strings.Contains(err.Error(), "hbench finish cafebabe") {
		t.Fatalf("should point at finish: %v", err)
	}
}

func TestFinishUnknownIDNamesLookPath(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(filepath.Dir(cwd)) })
	_, err := captureResult(t, []string{"finish", "no-such-run"})
	if err == nil {
		t.Fatal("expected run not found")
	}
	if !strings.Contains(err.Error(), "run not found: no-such-run") {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(cwd, "hb-out")) {
		t.Fatalf("should name ./hb-out path: %v", err)
	}
}
