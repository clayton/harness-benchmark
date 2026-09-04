package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScenarioNewAndValidate(t *testing.T) {
	out := filepath.Join(t.TempDir(), "scenario.yaml")
	if err := run([]string{"scenario", "new", "--out", out, "--id", "hello-world@1", "--title", "Hello world", "--prompt", "Add hello", "--type", "rewrite", "--language", "go", "--files-json", `{"README.md":"starter\n"}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"scenario", "validate", out}); err != nil {
		t.Fatal(err)
	}
}

func TestScenarioNewRequiresStarterFiles(t *testing.T) {
	err := run([]string{"scenario", "new", "--out", filepath.Join(t.TempDir(), "scenario.yaml"), "--id", "hello-world@1", "--title", "Hello world", "--prompt", "Add hello", "--language", "go", "--files-json", `{}`})
	if err == nil || !strings.Contains(err.Error(), "workspace files") {
		t.Fatalf("err=%v", err)
	}
}
