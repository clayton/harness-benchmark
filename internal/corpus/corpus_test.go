package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCacheWritesOfficialYAMLs(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCache(dir); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 {
		t.Fatalf("cached %d scenarios, want official corpus", len(got))
	}
	ids := map[string]bool{}
	for _, s := range got {
		ids[s.ID] = true
		if s.Repo.URL == "" || s.Repo.BaseRef == "" {
			t.Fatalf("scenario %s missing repo pins", s.ID)
		}
	}
	if !ids["js-commander-negative-exp-E"] {
		t.Fatal("missing commander scenario")
	}
	if _, err := os.Stat(filepath.Join(dir, "js-commander-negative-exp-E.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestFindReturnsCachedScenario(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCache(dir); err != nil {
		t.Fatal(err)
	}
	s, err := Find(dir, "go-chi-tee-bytes-double-count")
	if err != nil {
		t.Fatal(err)
	}
	if s.Language != "go" {
		t.Fatalf("language=%s", s.Language)
	}
}

func TestOfficialChiYAMLHasGoldAndFailToPass(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCache(dir); err != nil {
		t.Fatal(err)
	}
	s, err := Find(dir, "go-chi-tee-bytes-double-count")
	if err != nil {
		t.Fatal(err)
	}
	if s.Repo.GoldRef == "" {
		t.Fatal("gold_ref missing")
	}
	if len(s.Acceptance.FailToPass) == 0 {
		t.Fatal("fail_to_pass missing from official chi YAML")
	}
	if s.Acceptance.FailToPass[0] != "TestHttpFancyWriterReadFromByteCountWithTee" {
		t.Fatalf("fail_to_pass=%v", s.Acceptance.FailToPass)
	}
}

func TestOfficialChiPromptDoesNotRequireAddedTest(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCache(dir); err != nil {
		t.Fatal(err)
	}
	s, err := Find(dir, "go-chi-tee-bytes-double-count")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(s.Prompt)
	if strings.Contains(lower, "add a regression test") {
		t.Fatalf("prompt must not require a test the gold overlay replaces:\n%s", s.Prompt)
	}
	if !strings.Contains(lower, "you do not need to add a test") {
		t.Fatalf("prompt should say the judge applies the test:\n%s", s.Prompt)
	}
}
