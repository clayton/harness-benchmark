package adapter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidatesAndExpandsArgvManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adapter.yaml")
	raw := []byte("schema: hb.adapter.v1\nid: local\nversion: 1\ncommand: [agent-cli, --prompt-file, '${prompt_file}', --workspace, '${workspace}']\ntelemetry: none\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, digest, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if digest == "" || manifest.ID != "local" {
		t.Fatalf("manifest=%+v digest=%q", manifest, digest)
	}
	program, args := Launch(manifest, "ignored", "/workspace")
	if program != "agent-cli" || len(args) != 4 || args[1] != "HB_PROMPT.txt" || args[3] != "/workspace" {
		t.Fatalf("argv=%q %q", program, args)
	}
}

func TestRejectsShellOnlyAdapterCommand(t *testing.T) {
	manifest := Manifest{Schema: Schema, ID: "bad", Version: "1", Command: []string{"sh", "-c", "agent ${prompt}"}}
	if err := manifest.Validate(); err == nil {
		t.Fatal("shell adapter was accepted")
	}
	manifest.Command = []string{"agent-cli", "${unknown}"}
	if err := manifest.Validate(); err == nil {
		t.Fatal("unknown placeholder was accepted")
	}
	manifest.Command = []string{"agent-cli", "${prompt}", "${unfinished"}
	if err := manifest.Validate(); err == nil {
		t.Fatal("malformed placeholder was accepted")
	}
	for _, argument := range []string{"/Users/alice/private", "API_KEY=do-not-publish"} {
		manifest.Command = []string{"agent-cli", argument}
		if err := manifest.Validate(); err == nil {
			t.Fatalf("private adapter argument %q was accepted", argument)
		}
	}
}
