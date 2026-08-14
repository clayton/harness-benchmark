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
	finished, err := Finish(l, rec.ID, sc, 12, "manual", false)
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
	got := HeadlessLaunch("pi", "grok-4.6")
	if !strings.Contains(got, "--provider xai") || !strings.Contains(got, "--model grok-4.6") {
		t.Fatalf("pi grok-4.6 launch: %s", got)
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
	failed, err := Finish(l, wrong.ID, sc, 5, "wrong", false)
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
	passed, err := Finish(l, fix.ID, sc, 5, "fix", false)
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

func TestOverlayLocalGoldAndEnvironmentPatch(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	src := filepath.Join(home, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, src)
	sha := os.Getenv("HB_TEST_SHA")
	if err := os.WriteFile(filepath.Join(src, "app.sh"), []byte("echo seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "diff", "--no-ext-diff", "app.sh")
	cmd.Dir = src
	patchBytes, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "app.sh"), []byte("echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pack := t.TempDir()
	if err := os.WriteFile(filepath.Join(pack, "environment.patch"), patchBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pack, "verification_test.rb"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := paths.New(home, cwd)
	l.DataDir = filepath.Join(home, "data")
	sc := corpus.Scenario{
		ID:        "pack-demo",
		Title:     "seed",
		Prompt:    "fix it",
		SourceDir: pack,
		Repo:      corpus.Repo{URL: src, BaseRef: sha, EnvironmentPatch: "environment.patch"},
		Acceptance: corpus.Acceptance{
			GoldFiles:    []string{"verification_test.rb"},
			TestCommands: []string{"true"},
		},
	}
	rec, err := CreateRun(l, sc, "manual", false)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := os.ReadFile(filepath.Join(rec.Worktree, "app.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(seed), "echo seed") {
		t.Fatalf("environment patch not seeded: %q", seed)
	}
	if err := os.WriteFile(filepath.Join(rec.Worktree, "app.sh"), []byte("echo bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	finished, err := Finish(l, rec.ID, sc, 5, "pack", false)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != "completed" {
		t.Fatalf("status=%s judges=%+v", finished.Status, finished.Judges)
	}
	diff, err := os.ReadFile(filepath.Join(l.RunDir(rec.ID), "patch.diff"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(diff), "echo seed") && strings.Contains(string(diff), "echo hi") {
		t.Fatalf("published diff should be vs seeded HEAD, got:\n%s", diff)
	}
	if _, err := os.Stat(filepath.Join(rec.Worktree, "test", "hb_verification_test.rb")); err == nil {
		t.Fatal("gold file should be restored off the worktree after judge")
	}
}

func TestLoadNamesTheLookPath(t *testing.T) {
	l := paths.New(t.TempDir(), t.TempDir())
	_, err := Load(l, "missingid")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "run not found: missingid") {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(err.Error(), l.OutDir) {
		t.Fatalf("error should name hb-out path %s: %v", l.OutDir, err)
	}
}

func TestResolveRunIDFromWorkspacePath(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	l := paths.New(home, cwd)
	rec := RunRecord{ID: "aabbccdd", Status: "pending", CreatedAt: Now()}
	if err := Save(l, rec); err != nil {
		t.Fatal(err)
	}
	if err := SetLatest(l, rec.ID); err != nil {
		t.Fatal(err)
	}
	ws := l.Worktree(rec.ID)
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	got := RunIDFromPath(ws, l.OutDir)
	if got != rec.ID {
		t.Fatalf("from workspace: got %s", got)
	}
	got = RunIDFromPath(filepath.Join(ws, "middleware"), l.OutDir)
	if got != rec.ID {
		t.Fatalf("from nested: got %s", got)
	}
	got = RunIDFromPath(t.TempDir(), l.OutDir)
	if got != "" {
		t.Fatalf("unrelated path should not resolve: %s", got)
	}
}

func TestFinishDoesNotRejudgeCompletedWithoutForce(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(rec.Worktree, "app.sh"), []byte("echo bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := Finish(l, rec.ID, sc, 12, "manual", false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "completed" {
		t.Fatalf("status=%s", first.Status)
	}
	marker := filepath.Join(l.RunDir(rec.ID), "judge-sh-test-sh.txt")
	info1, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Finish(l, rec.ID, sc, 99, "again", false)
	if err != nil {
		t.Fatal(err)
	}
	if second.FinishedAt != first.FinishedAt {
		t.Fatalf("re-judged without --force: %s vs %s", first.FinishedAt, second.FinishedAt)
	}
	info2, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	if !info2.ModTime().Equal(info1.ModTime()) {
		t.Fatal("judge output rewritten without --force")
	}
	third, err := Finish(l, rec.ID, sc, 99, "forced", true)
	if err != nil {
		t.Fatal(err)
	}
	if third.FinishedAt == first.FinishedAt {
		t.Fatal("--force should re-judge")
	}
}

func TestManualWorkspaceGuideHasNoAgentSlang(t *testing.T) {
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
		ID:     "local-echo",
		Title:  "say bye",
		Prompt: "change hi to bye",
		Repo:   corpus.Repo{URL: src, BaseRef: sha},
	}
	rec, err := CreateRun(l, sc, "manual", false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(rec.Worktree, "HB_RUN.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ToLower(string(raw))
	for _, word := range []string{"agent", "token", "feed"} {
		if strings.Contains(body, word) {
			t.Fatalf("manual guide still says %q:\n%s", word, raw)
		}
	}
	if !strings.Contains(string(raw), "Stay in the directory") {
		t.Fatalf("guide should say stay in the start directory:\n%s", raw)
	}
	if !strings.Contains(string(raw), "hbench finish "+rec.ID) {
		t.Fatalf("guide missing finish command:\n%s", raw)
	}
}
