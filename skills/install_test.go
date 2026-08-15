package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstall(t *testing.T) {
	root := t.TempDir()
	if err := Install(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "run-agent-rodeo-study", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := Install(root); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}
