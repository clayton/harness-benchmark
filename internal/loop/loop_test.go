package loop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(bytesTrim(out))
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareWorktreeCompletesFilesWhenCacheIsBlobless(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	src := filepath.Join(home, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, src)
	// Files that disappeared on the official chi first ride.
	writeFile(t, filepath.Join(src, "wrap_writer.go"), "package middleware\n")
	writeFile(t, filepath.Join(src, "chi.go"), "package chi\n")
	writeFile(t, filepath.Join(src, "mux.go"), "package chi\n")
	gitOutput(t, src, "add", ".")
	gitOutput(t, src, "commit", "-m", "source files")
	sha := gitOutput(t, src, "rev-parse", "HEAD")

	l := paths.New(home, cwd)
	l.DataDir = filepath.Join(home, "data")
	sc := corpus.Scenario{
		ID:     "blobless-repro",
		Title:  "complete tree",
		Prompt: "fix it",
		Repo:   corpus.Repo{URL: src, BaseRef: sha},
	}

	// Plant the old bug: blobless cache that cannot serve blobs to a child clone.
	cache := filepath.Join(l.ReposDir(), repoSlug(src))
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "clone", "--filter=blob:none", src, cache)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("plant blobless cache: %v\n%s", err, out)
	}

	wt, err := PrepareWorktree(l, sc, "ride1", false)
	if err != nil {
		t.Fatal(err)
	}
	origin := gitOutput(t, wt, "remote", "get-url", "origin")
	if origin == cache {
		t.Fatalf("worktree origin must not be the cache %s", cache)
	}
	for _, name := range []string{"wrap_writer.go", "chi.go", "mux.go"} {
		raw, err := os.ReadFile(filepath.Join(wt, name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if len(raw) == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestFinishOverlaysGoldTestsBeforeScoring(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	src := filepath.Join(home, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "hb@test"},
		{"git", "config", "user.name", "hb"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = src
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	// Base: existing tests pass even when the bug is present.
	writeFile(t, filepath.Join(src, "app.sh"), "echo hi\n")
	writeFile(t, filepath.Join(src, "test.sh"), "#!/bin/sh\nexit 0\n")
	gitOutput(t, src, "add", ".")
	gitOutput(t, src, "commit", "-m", "base")
	base := gitOutput(t, src, "rev-parse", "HEAD")

	// Gold: FAIL_TO_PASS test that only passes after the production fix.
	writeFile(t, filepath.Join(src, "app.sh"), "echo bye\n")
	writeFile(t, filepath.Join(src, "tests", "fail_to_pass.sh"), "#!/bin/sh\ngrep -q bye app.sh\n")
	writeFile(t, filepath.Join(src, "test.sh"), "#!/bin/sh\nsh tests/fail_to_pass.sh\n")
	gitOutput(t, src, "add", ".")
	gitOutput(t, src, "commit", "-m", "gold")
	gold := gitOutput(t, src, "rev-parse", "HEAD")

	l := paths.New(home, cwd)
	l.DataDir = filepath.Join(home, "data")
	sc := corpus.Scenario{
		ID:       "gold-overlay",
		Title:    "overlay",
		Prompt:   "say bye",
		Language: "sh",
		Repo:     corpus.Repo{URL: src, BaseRef: base, GoldRef: gold},
		Acceptance: corpus.Acceptance{
			TestCommands: []string{"sh test.sh"},
			FailToPass:   []string{"tests/fail_to_pass.sh"},
		},
	}

	wrong, err := CreateRun(l, sc, "manual", false)
	if err != nil {
		t.Fatal(err)
	}
	// Non-empty but wrong patch: base tests would pass without overlay.
	if err := os.WriteFile(filepath.Join(wrong.Worktree, "app.sh"), []byte("echo no\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	failed, err := Finish(l, wrong.ID, sc, 5, "wrong")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" {
		t.Fatalf("wrong patch must fail with gold overlay, status=%s judges=%+v", failed.Status, failed.Judges)
	}
	var sawOverlay bool
	for _, j := range failed.Judges {
		if j.Name == "gold_test_overlay" && j.Passed != nil && *j.Passed {
			sawOverlay = true
		}
	}
	if !sawOverlay {
		t.Fatalf("expected gold_test_overlay judge: %+v", failed.Judges)
	}
	overlayNote, err := os.ReadFile(filepath.Join(l.RunDir(wrong.ID), "judge-gold-overlay.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(overlayNote), "fail_to_pass.sh") && !strings.Contains(string(overlayNote), "test.sh") {
		t.Fatalf("overlay list: %q", overlayNote)
	}

	fix, err := CreateRun(l, sc, "manual", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fix.Worktree, "app.sh"), []byte("echo bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	passed, err := Finish(l, fix.ID, sc, 5, "fix")
	if err != nil {
		t.Fatal(err)
	}
	if passed.Status != "completed" {
		t.Fatalf("gold-passing fix must complete, status=%s judges=%+v", passed.Status, passed.Judges)
	}
}

func TestPrepareWorktreeOfficialChiHasSourceFiles(t *testing.T) {
	dir := t.TempDir()
	if err := corpus.EnsureCache(dir); err != nil {
		t.Fatal(err)
	}
	sc, err := corpus.Find(dir, "go-chi-tee-bytes-double-count")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	cwd := t.TempDir()
	l := paths.New(home, cwd)
	l.DataDir = filepath.Join(home, "data")
	wt, err := PrepareWorktree(l, sc, "chi-ride", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"middleware/wrap_writer.go", "chi.go", "mux.go"} {
		info, err := os.Stat(filepath.Join(wt, rel))
		if err != nil {
			t.Fatalf("official chi worktree missing %s: %v", rel, err)
		}
		if info.Size() == 0 {
			t.Fatalf("official chi %s is empty", rel)
		}
	}
}
