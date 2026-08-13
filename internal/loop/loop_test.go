package loop

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "hb@test"},
		{"git", "config", "user.name", "hb"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "app.sh"), []byte("echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.sh"), []byte("#!/bin/sh\ngrep -q bye app.sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	cmd = exec.Command("git", "commit", "-m", "base")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	shaCmd := exec.Command("git", "rev-parse", "HEAD")
	shaCmd.Dir = dir
	sha, err := shaCmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HB_TEST_SHA", string(bytesTrim(sha)))
}

func TestCreateRunAndFinishWithoutExecute(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	src := filepath.Join(home, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, src)
	sha := os.Getenv("HB_TEST_SHA")
	l := paths.New(home, cwd)
	l.DataDir = filepath.Join(home, "data")
	sc := corpus.Scenario{
		ID:       "local-echo",
		Title:    "say bye",
		Prompt:   "change hi to bye",
		Language: "sh",
		Repo:     corpus.Repo{URL: src, BaseRef: sha},
		Acceptance: corpus.Acceptance{
			TestCommands: []string{"sh test.sh"},
		},
	}
	rec, err := CreateRun(l, sc, "manual", false)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != "pending" {
		t.Fatalf("status=%s", rec.Status)
	}
	prompt := filepath.Join(rec.Worktree, "HB_PROMPT.txt")
	if _, err := os.Stat(prompt); err != nil {
		t.Fatal("workspace prompt missing")
	}
	if _, err := LatestID(l); err != nil {
		t.Fatal(err)
	}
	// apply the fix without calling a real agent
	if err := os.WriteFile(filepath.Join(rec.Worktree, "app.sh"), []byte("echo bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	finished, err := Finish(l, rec.ID, sc, 12, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != "completed" {
		t.Fatalf("status=%s judges=%+v", finished.Status, finished.Judges)
	}
	if _, err := os.Stat(filepath.Join(l.RunDir(rec.ID), "patch.diff")); err != nil {
		t.Fatal("patch not written")
	}
}

func TestHeadlessCommandKnownHarnesses(t *testing.T) {
	if HeadlessCommand("grok") == "" || HeadlessCommand("pi") == "" {
		t.Fatal("missing launch templates")
	}
	if HeadlessCommand("manual") != "" {
		t.Fatal("manual must not have headless launch")
	}
}
