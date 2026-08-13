package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLayoutPutsCorpusInHomeAndArtifactsInCwd(t *testing.T) {
	t.Setenv("HB_OUT_DIR", "")
	cwd := t.TempDir()
	l := New("/home/rider", cwd)
	if l.ScenariosDir() == "" || l.DataDir == "" {
		t.Fatal("data dir empty")
	}
	if filepath.Base(l.OutDir) != "hb-out" {
		t.Fatalf("out dir = %s, want .../hb-out", l.OutDir)
	}
	if l.OutDir != filepath.Join(cwd, "hb-out") {
		t.Fatalf("artifacts should be under cwd, got %s", l.OutDir)
	}
	if l.RunDir("abc") != filepath.Join(cwd, "hb-out", "abc") {
		t.Fatalf("run dir = %s", l.RunDir("abc"))
	}
}

func TestFindOutDirWalksFromWorkspaceToAncestor(t *testing.T) {
	t.Setenv("HB_OUT_DIR", "")
	root := t.TempDir()
	store := filepath.Join(root, "hb-out")
	ws := filepath.Join(store, "abc123", "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "latest"), []byte("abc123"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := FindOutDir(ws)
	if filepath.Clean(got) != filepath.Clean(store) {
		t.Fatalf("from workspace: got %s want %s", got, store)
	}
	nested := filepath.Join(ws, "middleware")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got = FindOutDir(nested)
	if filepath.Clean(got) != filepath.Clean(store) {
		t.Fatalf("from nested: got %s want %s", got, store)
	}
	got = FindOutDir(root)
	if filepath.Clean(got) != filepath.Clean(store) {
		t.Fatalf("from start dir: got %s want %s", got, store)
	}
}

func TestFindOutDirWithoutStoreStaysInStart(t *testing.T) {
	t.Setenv("HB_OUT_DIR", "")
	start := t.TempDir()
	got := FindOutDir(start)
	want := filepath.Join(start, "hb-out")
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("got %s want %s", got, want)
	}
	if IsStore(got) {
		t.Fatal("missing dir must not look like a store")
	}
}
