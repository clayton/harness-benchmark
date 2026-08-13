package paths

import (
	"path/filepath"
	"testing"
)

func TestLayoutPutsCorpusInHomeAndArtifactsInCwd(t *testing.T) {
	l := New("/home/rider", "/tmp/work")
	if l.ScenariosDir() == "" || l.DataDir == "" {
		t.Fatal("data dir empty")
	}
	if filepath.Base(l.OutDir) != "hb-out" {
		t.Fatalf("out dir = %s, want .../hb-out", l.OutDir)
	}
	if l.OutDir != filepath.Join("/tmp/work", "hb-out") {
		t.Fatalf("artifacts should be under cwd, got %s", l.OutDir)
	}
	if l.RunDir("abc") != filepath.Join("/tmp/work", "hb-out", "abc") {
		t.Fatalf("run dir = %s", l.RunDir("abc"))
	}
}
