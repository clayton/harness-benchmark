package corpus

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchRodeoVerifiesAndCachesSafeManifest(t *testing.T) {
	manifest := map[string]any{
		"schema": "rodeo.scenario.v2", "id": "safe-task@1", "slug": "safe-task", "version": 1,
		"status": "community", "type": "bugfix", "title": "Safe task", "description": "Fix it", "prompt": "Fix <tag> & keep it safe",
		"language": "Go", "tags": []any{}, "difficulty": "medium",
		"repo":       map[string]any{"url": "https://github.com/example/project", "base_ref": strings.Repeat("a", 40)},
		"acceptance": map[string]any{"setup_commands": []any{"go mod download"}, "test_commands": []any{"go test ./..."}},
		"rubric":     "Tests pass", "environment_image_digest": nil, "network_policy": "none", "budget": map[string]any{},
		"protocol_id": nil, "rating_eligible_until": nil,
	}
	digestible := map[string]any{}
	for key, value := range manifest {
		if key != "status" {
			digestible[key] = value
		}
	}
	raw, _ := canonicalJSON(digestible)
	manifest["manifest_digest"] = fmt.Sprintf("%x", sha256.Sum256(raw))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(manifest) }))
	defer server.Close()
	t.Setenv("HB_RODEO_URL", server.URL)
	dir := t.TempDir()

	scenario, err := FetchRodeo(dir, "safe-task@1", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if scenario.ID != "safe-task@1" || scenario.Repo.GoldRef != "" || scenario.Status != "community" {
		t.Fatalf("unsafe or incomplete scenario: %#v", scenario)
	}
	if _, err := os.Stat(filepath.Join(dir, "community", "safe-task-v1.json")); err != nil {
		t.Fatal(err)
	}
}

func TestFetchRodeoHydratesMatchingBuiltInScenario(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCache(dir); err != nil {
		t.Fatal(err)
	}
	local, err := Find(dir, "zip-password-finder-python-port")
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema": "rodeo.scenario.v2", "id": "zip-password-finder-python-port@1", "slug": local.ID, "version": 1,
		"status": "official", "type": local.Type, "title": local.Title, "description": local.Description, "prompt": local.Prompt,
		"language": local.Language, "tags": local.Tags, "difficulty": local.Difficulty,
		"repo":         map[string]any{"url": local.Repo.URL, "base_ref": local.Repo.BaseRef},
		"acceptance":   map[string]any{"setup_commands": local.Acceptance.SetupCommands, "test_commands": local.Acceptance.TestCommands},
		"requirements": local.Requirements, "fetches": local.Fetches,
		"rubric": "Public evaluator", "environment_image_digest": nil, "network_policy": "none", "budget": map[string]any{},
		"protocol_id": nil, "rating_eligible_until": nil, "builtin_scenario_id": local.ID,
	}
	encoded, _ := json.Marshal(manifest)
	_ = json.Unmarshal(encoded, &manifest)
	digestible := map[string]any{}
	for key, value := range manifest {
		if key != "status" && key != "builtin_scenario_id" {
			digestible[key] = value
		}
	}
	raw, _ := canonicalJSON(digestible)
	manifest["manifest_digest"] = fmt.Sprintf("%x", sha256.Sum256(raw))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(manifest) }))
	defer server.Close()
	t.Setenv("HB_RODEO_URL", server.URL)

	scenario, err := FetchRodeo(dir, "zip-password-finder-python-port@1", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if scenario.External || scenario.ID != "zip-password-finder-python-port@1" || len(scenario.Acceptance.GoldFiles) == 0 {
		t.Fatalf("built-in scenario was not hydrated: %+v", scenario)
	}
}

func TestFetchRodeoRejectsDriftedBuiltInScenario(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCache(dir); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema": "rodeo.scenario.v2", "id": "zip-password-finder-python-port@1", "slug": "zip-password-finder-python-port", "version": 1,
		"status": "official", "prompt": "changed", "repo": map[string]any{"url": "https://github.com/agourlay/zip-password-finder.git", "base_ref": strings.Repeat("a", 40)},
		"acceptance": map[string]any{"setup_commands": []any{}, "test_commands": []any{}}, "builtin_scenario_id": "zip-password-finder-python-port",
	}
	digestible := map[string]any{}
	for key, value := range manifest {
		if key != "status" && key != "builtin_scenario_id" {
			digestible[key] = value
		}
	}
	raw, _ := canonicalJSON(digestible)
	manifest["manifest_digest"] = fmt.Sprintf("%x", sha256.Sum256(raw))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(manifest) }))
	defer server.Close()
	t.Setenv("HB_RODEO_URL", server.URL)
	if _, err := FetchRodeo(dir, "zip-password-finder-python-port@1", server.Client()); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("drifted built-in scenario err=%v", err)
	}
}

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
	if _, err := os.Stat(filepath.Join(dir, "hb_zip_password_finder_test.py")); err != nil {
		t.Fatalf("embedded evaluator asset missing: %v", err)
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

func TestUnmarshalScenarioJSONAcceptsLegacyPascalCase(t *testing.T) {
	legacy := []byte(`{
  "ID": "pack-demo",
  "Prompt": "fix it",
  "SourceDir": "/tmp/pack",
  "Acceptance": {
    "TestCommands": ["bin/rails test"],
    "GoldFiles": ["verification_test.rb"]
  },
  "Repo": {"URL": "https://example.com/app.git", "BaseRef": "abc", "EnvironmentPatch": "environment.patch"}
}`)
	s, err := UnmarshalScenarioJSON(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "pack-demo" || s.SourceDir != "/tmp/pack" {
		t.Fatalf("legacy decode: %+v", s)
	}
	if len(s.Acceptance.TestCommands) != 1 || s.Acceptance.TestCommands[0] != "bin/rails test" {
		t.Fatalf("test_commands=%v", s.Acceptance.TestCommands)
	}
	if s.Repo.EnvironmentPatch != "environment.patch" {
		t.Fatalf("environment_patch=%q", s.Repo.EnvironmentPatch)
	}
	if len(s.Acceptance.GoldFiles) != 1 || s.Acceptance.GoldFiles[0] != "verification_test.rb" {
		t.Fatalf("gold_files=%v", s.Acceptance.GoldFiles)
	}
}

func TestResolveLoadsOptionalPackYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "optional", "scenario.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "id: pack-demo\ntitle: demo\nlanguage: ruby\nprompt: |\n  fix it\nrepo:\n  url: https://example.com/app.git\n  base_ref: abc\n  environment_patch: environment.patch\nacceptance:\n  gold_files:\n    - verification_test.rb\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := Resolve(t.TempDir(), dir, "pack-demo")
	if err != nil {
		t.Fatal(err)
	}
	if sc.SourceDir != filepath.Dir(path) {
		t.Fatalf("SourceDir=%s", sc.SourceDir)
	}
	if sc.Repo.EnvironmentPatch != "environment.patch" {
		t.Fatalf("patch=%q", sc.Repo.EnvironmentPatch)
	}
	if len(sc.Acceptance.GoldFiles) != 1 {
		t.Fatalf("gold_files=%v", sc.Acceptance.GoldFiles)
	}
	sc2, err := LoadFile(path)
	if err != nil || sc2.ID != "pack-demo" {
		t.Fatalf("LoadFile: %+v %v", sc2, err)
	}
}

func TestTrustDigestCoversCommandsAndReferencedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "environment.patch")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Scenario{ID: "external", SourceDir: dir, Repo: Repo{EnvironmentPatch: "environment.patch"}, Acceptance: Acceptance{TestCommands: []string{"go test ./..."}}}
	first, err := TrustDigest(s)
	if err != nil {
		t.Fatal(err)
	}
	s.Acceptance.TestCommands[0] = "make upload"
	second, err := TrustDigest(s)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("command change did not change trust digest")
	}
	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := TrustDigest(s)
	if err != nil {
		t.Fatal(err)
	}
	if second == third {
		t.Fatal("referenced file change did not change trust digest")
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
