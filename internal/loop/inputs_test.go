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

func TestCheckRequirementsReadsVPrefixedVersion(t *testing.T) {
	bin := t.TempDir()
	tool := filepath.Join(bin, "node")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\necho v26.7.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	sc := corpus.Scenario{Requirements: corpus.Requirements{Commands: []corpus.CommandRequirement{{Name: "node", MinimumVersion: "22.12", Purpose: "test"}}}}
	if _, err := CheckRequirements(sc); err != nil {
		t.Fatal(err)
	}
}

func TestDependencyCheckoutUsesFrozenRef(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "Cargo.lock"), "frozen\n")
	gitOutput(t, repo, "add", "Cargo.lock")
	gitOutput(t, repo, "commit", "-m", "lock")
	frozen := gitOutput(t, repo, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(repo, "Cargo.lock"), "newer\n")
	gitOutput(t, repo, "commit", "-am", "newer")

	checkout, cleanup, err := dependencyCheckout(repo, frozen)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, err := os.ReadFile(filepath.Join(checkout, "Cargo.lock"))
	if err != nil || string(raw) != "frozen\n" {
		t.Fatalf("lock=%q err=%v", raw, err)
	}
}

func TestPrepareInputsAggregatesBeforeRepositoryFetch(t *testing.T) {
	root := t.TempDir()
	l := paths.New(root, root)
	l.DataDir = filepath.Join(root, "data")
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"packages":{"node_modules/x":{"resolved":"https://registry.npmjs.org/x/-/x.tgz"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sc := corpus.Scenario{
		Repo:      corpus.Repo{URL: "https://example.test/repo.git", BaseRef: "abc123", GoldRef: "def456"},
		SourceDir: root,
		Fetches:   []corpus.Fetch{{Kind: "npm", Lockfile: "package-lock.json", SourceLockfile: "package-lock.json", Reason: "locked packages"}},
	}
	called := 0
	err := PrepareInputs(l, sc, true, func(plan fetchconsent.Plan) error {
		called++
		if len(plan.Items) != 3 {
			t.Fatalf("items=%+v", plan.Items)
		}
		if plan.Items[0].Ref != "abc123" || plan.Items[1].Ref != "def456" {
			t.Fatalf("repository refs=%+v", plan.Items)
		}
		return os.ErrPermission
	})
	if called != 1 || !os.IsPermission(err) {
		t.Fatalf("called=%d err=%v", called, err)
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

func TestValidateSetupFetchesAllowsDeclaredInstaller(t *testing.T) {
	sc := corpus.Scenario{
		Acceptance: corpus.Acceptance{SetupCommands: []string{"npm ci"}},
		Fetches:    []corpus.Fetch{{Kind: "npm", Lockfile: "package-lock.json", Reason: "locked packages"}},
	}
	if err := validateSetupFetches(sc); err != nil {
		t.Fatal(err)
	}
}

func TestDependencySourcesUsesDownloadOrigins(t *testing.T) {
	bundler := dependencySources("bundler", []byte("remote: https://rubygems.org/\nremote: https://github.com/example/repo.git\n"))
	if strings.Join(bundler, ",") != "https://github.com,https://rubygems.org" {
		t.Fatalf("bundler sources=%v", bundler)
	}
	npm := dependencySources("npm", []byte(`{"packages":{"node_modules/x":{"resolved":"https://registry.npmjs.org/x/-/x.tgz","funding":"https://example.com"}}}`))
	if strings.Join(npm, ",") != "https://registry.npmjs.org" {
		t.Fatalf("npm sources=%v", npm)
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

func TestNormalizeDirectProfileRecordsDefaults(t *testing.T) {
	pi, err := NormalizeDirectProfile(Profile{Harness: "pi", Provider: "openai", Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if pi.Reasoning != "default" {
		t.Fatalf("Pi reasoning=%q", pi.Reasoning)
	}
	codex, err := NormalizeDirectProfile(Profile{Harness: "codex", Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if codex.Provider != "openai" || codex.Reasoning != "default" {
		t.Fatalf("Codex profile=%+v", codex)
	}
}

func TestDetectHarnessIdentityReportsResolvedExecutable(t *testing.T) {
	bin := t.TempDir()
	piPath := filepath.Join(bin, "pi")
	if err := os.WriteFile(piPath, []byte("#!/bin/sh\necho 0.84.2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	identity, err := DetectHarnessIdentity("pi")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Path != piPath || identity.Version != "0.84.2" {
		t.Fatalf("identity=%+v", identity)
	}
}

func TestDetectHarnessIdentityAllowsSlowStartup(t *testing.T) {
	bin := t.TempDir()
	piPath := filepath.Join(bin, "pi")
	if err := os.WriteFile(piPath, []byte("#!/bin/sh\nsleep 3\necho 0.84.2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	identity, err := DetectHarnessIdentity("pi")
	if err != nil || identity.Version != "0.84.2" {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
}
