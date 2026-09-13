package loop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clayton/harness-benchmark/internal/paths"
)

func TestExecuteUsesAdapterArgvWithoutShellExpansion(t *testing.T) {
	root := t.TempDir()
	l := paths.New(root, root)
	id := "adaf7e000001"
	worktree := l.Worktree(id)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "HB_PROMPT.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(bin, "adapter-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ncat \"$1\" > adapter-result.txt\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	manifest := filepath.Join(root, "adapter.yaml")
	if err := os.WriteFile(manifest, []byte("schema: hb.adapter.v1\nid: local\nversion: 1\ncommand: [adapter-helper, '${prompt_file}']\ntelemetry: none\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := map[string]any{"harness": "adapter:local", "adapter_manifest": manifest, "adapter_telemetry": "none"}
	if err := Save(l, RunRecord{ID: id, Status: "pending", Worktree: worktree, Harness: "adapter:local", HarnessVersion: "adapter/test", CreatedAt: Now(), Metadata: map[string]any{"profile": profile}}); err != nil {
		t.Fatal(err)
	}
	result, err := Execute(l, id, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReturnCode != 0 {
		t.Fatalf("return code=%d", result.ReturnCode)
	}
	got, err := os.ReadFile(filepath.Join(worktree, "adapter-result.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("result=%q", got)
	}
}
