package loop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/fetchconsent"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func TestCheckRequirementsUsesResolvedPATHVersion(t *testing.T) {
	bin := t.TempDir()
	tool := filepath.Join(bin, "cargo")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\necho cargo 1.79.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	sc := corpus.Scenario{Requirements: corpus.Requirements{Commands: []corpus.CommandRequirement{{Name: "cargo", MinimumVersion: "1.85", Purpose: "build"}}}}
	results, err := CheckRequirements(sc)
	if err == nil || !strings.Contains(err.Error(), "below required 1.85") {
		t.Fatalf("err=%v", err)
	}
	if len(results) != 1 || results[0].Path != tool || results[0].Version != "1.79.0" {
		t.Fatalf("results=%+v", results)
	}
}

func TestPrepareInputsStopsBeforeRepositoryFetch(t *testing.T) {
	root := t.TempDir()
	l := paths.New(root, root)
	l.DataDir = filepath.Join(root, "data")
	sc := corpus.Scenario{Repo: corpus.Repo{URL: "https://example.test/repo.git", BaseRef: "abc123"}}
	called := false
	err := PrepareInputs(l, sc, true, func(plan fetchconsent.Plan) error {
		called = true
		return os.ErrPermission
	})
	if !called || !os.IsPermission(err) {
		t.Fatalf("called=%v err=%v", called, err)
	}
	if _, statErr := os.Stat(l.ReposDir()); !os.IsNotExist(statErr) {
		t.Fatalf("repository cache was touched: %v", statErr)
	}
}

func TestPrepareInputsSkipsRepositoryForScaffold(t *testing.T) {
	root := t.TempDir()
	l := paths.New(root, root)
	l.DataDir = filepath.Join(root, "data")
	sc := corpus.Scenario{Workspace: corpus.Workspace{Kind: "scaffold", Files: map[string]string{"README.md": "hello"}}}
	if err := PrepareInputs(l, sc, true, func(fetchconsent.Plan) error { return os.ErrPermission }); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareInputsRejectsUnplannedInstaller(t *testing.T) {
	root := t.TempDir()
	l := paths.New(root, root)
	l.DataDir = filepath.Join(root, "data")
	sc := corpus.Scenario{Acceptance: corpus.Acceptance{SetupCommands: []string{"pip install -e ."}}}
	called := false
	err := PrepareInputs(l, sc, true, func(fetchconsent.Plan) error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "without a supported fetch plan") {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("authorizer was called for an unsupported installer")
	}
}

func TestNormalizeDirectPiProfile(t *testing.T) {
	profile, err := NormalizeDirectProfile(Profile{Harness: "pi", Model: "openai/gpt-5.6-sol:medium"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Provider != "openai" || profile.Model != "gpt-5.6-sol" || profile.Reasoning != "medium" {
		t.Fatalf("profile=%+v", profile)
	}
	if _, err := NormalizeDirectProfile(Profile{Harness: "pi", Provider: "google", Model: "openai/gpt-5.6-sol"}); err == nil {
		t.Fatal("provider conflict was accepted")
	}
}
