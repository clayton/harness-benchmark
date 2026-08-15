package loop

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/clayton/harness-benchmark/internal/corpus"
)

func TestRootedFilesRejectTraversalAndEscapingSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	escape := filepath.Join(outside, "escape.txt")
	if err := os.WriteFile(escape, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeRooted(root, "../escape.txt", []byte("owned"), 0o600); err == nil {
		t.Fatal("parent traversal was accepted")
	}
	if err := writeRooted(root, escape, []byte("owned"), 0o600); err == nil {
		t.Fatal("absolute path was accepted")
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := writeRooted(root, "linked/escape.txt", []byte("owned"), 0o600); err == nil {
		t.Fatal("escaping directory symlink was accepted")
	}
	if _, err := readRooted(root, "linked/escape.txt"); err == nil {
		t.Fatal("escaping source symlink was accepted")
	}
	got, err := os.ReadFile(escape)
	if err != nil || string(got) != "safe" {
		t.Fatalf("outside file changed: %q %v", got, err)
	}
}

func TestRootedFilesRejectSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	outside := t.TempDir()
	declared := filepath.Join(parent, "declared")
	if err := os.Symlink(outside, declared); err != nil {
		t.Fatal(err)
	}
	if err := writeRooted(declared, "owned.txt", []byte("owned"), 0o600); err == nil {
		t.Fatal("symlink root was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "owned.txt")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through root symlink: %v", err)
	}
}

func TestRootedFilesResistRootSymlinkSwap(t *testing.T) {
	parent := t.TempDir()
	outside := t.TempDir()
	declared := filepath.Join(parent, "declared")
	parked := filepath.Join(parent, "parked")
	if err := os.Mkdir(declared, 0o755); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			if os.Rename(declared, parked) != nil {
				continue
			}
			if os.Symlink(outside, declared) == nil {
				_ = os.Remove(declared)
			}
			_ = os.Rename(parked, declared)
		}
	}()
	for i := 0; i < 500; i++ {
		_ = writeRooted(declared, "owned.txt", []byte("inside"), 0o600)
	}
	stop.Store(true)
	<-done
	if _, err := os.Stat(filepath.Join(outside, "owned.txt")); !os.IsNotExist(err) {
		t.Fatalf("write escaped during root swap: %v", err)
	}
}

func TestScenarioPatchAndGoldFlowsRejectEscapingFiles(t *testing.T) {
	parent := t.TempDir()
	pack := filepath.Join(parent, "pack")
	worktree := filepath.Join(parent, "worktree")
	if err := os.MkdirAll(pack, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside.patch")
	if err := os.WriteFile(outside, []byte("not a patch"), 0o600); err != nil {
		t.Fatal(err)
	}
	scenario := corpus.Scenario{SourceDir: pack, Repo: corpus.Repo{EnvironmentPatch: "../outside.patch"}}
	if err := applyEnvironmentPatch(worktree, scenario); err == nil {
		t.Fatal("environment_patch traversal was accepted")
	}
	if err := os.Symlink(outside, filepath.Join(pack, "gold.rb")); err != nil {
		t.Fatal(err)
	}
	scenario.Repo.EnvironmentPatch = ""
	scenario.Acceptance.GoldFiles = []string{"gold.rb"}
	if _, _, err := overlayLocalGold(worktree, scenario); err == nil {
		t.Fatal("escaping gold file symlink was accepted")
	}
}

func TestRootedAtomicWriteReplacesAFileSymlinkWithoutFollowingIt(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "result.txt")); err != nil {
		t.Fatal(err)
	}

	if err := writeRooted(root, "result.txt", []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideRaw, _ := os.ReadFile(outside)
	insideRaw, _ := os.ReadFile(filepath.Join(root, "result.txt"))
	if string(outsideRaw) != "safe" || string(insideRaw) != "inside" {
		t.Fatalf("outside=%q inside=%q", outsideRaw, insideRaw)
	}
}
