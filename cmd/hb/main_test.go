package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func capture(t *testing.T, args []string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := run(args)
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if runErr != nil {
		t.Fatalf("run %v: %v\n%s", args, runErr, out)
	}
	return string(out)
}

func TestVersionSaysGo(t *testing.T) {
	out := capture(t, []string{"version"})
	if !strings.Contains(out, "go") || !strings.Contains(out, "hb") {
		t.Fatalf("version: %q", out)
	}
}

func TestBarePrintsOnlyTheSuggestedCommand(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(filepath.Dir(cwd)) })
	out := capture(t, nil)
	if strings.Contains(out, "hbench doctor") {
		t.Fatalf("bare hbench should not dump doctor:\n%s", out)
	}
	if strings.Contains(out, "Harnesses") {
		t.Fatalf("bare hbench should not list harnesses:\n%s", out)
	}
	if !strings.Contains(out, "hbench run -s") {
		t.Fatalf("bare hbench should print one run command:\n%s", out)
	}
	if strings.Count(strings.TrimSpace(out), "\n") > 1 {
		t.Fatalf("want one command line, got:\n%s", out)
	}
}

func TestDoctorPrintsFullReport(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(filepath.Dir(cwd)) })
	out := capture(t, []string{"doctor"})
	if !strings.Contains(out, "hbench doctor") {
		t.Fatalf("missing doctor header:\n%s", out)
	}
	if !strings.Contains(out, "not attached") {
		t.Fatalf("must say skills are not attached:\n%s", out)
	}
	if strings.Count(out, "Suggested next step") != 1 {
		t.Fatalf("want one suggestion:\n%s", out)
	}
}

func TestRunHelpExitsZero(t *testing.T) {
	out := capture(t, []string{"run", "--help"})
	if !strings.Contains(out, "hbench run -s") {
		t.Fatalf("run help:\n%s", out)
	}
}

func TestPublishHelpDoesNotUpload(t *testing.T) {
	out := capture(t, []string{"publish", "--help"})
	if !strings.Contains(out, "not automatic") {
		t.Fatalf("help: %q", out)
	}
}

func TestListScenariosFromEmbeddedCorpus(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	out := capture(t, []string{"list", "scenarios"})
	if !bytes.Contains([]byte(out), []byte("js-commander-negative-exp-E")) {
		t.Fatalf("list:\n%s", out)
	}
}
