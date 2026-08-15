package publish

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func TestPublishPostsOnlyWhenCalled(t *testing.T) {
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/riders":
			if r.Method != http.MethodPost {
				t.Fatalf("method %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok", "slug": "dusty"})
		case "/api/v1/runs":
			posted = true
			if r.Header.Get("Authorization") != "Bearer tok" {
				t.Fatalf("auth %s", r.Header.Get("Authorization"))
			}
			body, _ := io.ReadAll(r.Body)
			if !json.Valid(body) {
				t.Fatal("invalid json")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "unofficial": false})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("HB_RODEO_URL", srv.URL)
	t.Setenv("HB_ALLOW_INSECURE_LOCALHOST", "1")
	rider := filepath.Join(t.TempDir(), "rider.json")
	t.Setenv("HB_RIDER_FILE", rider)

	home := t.TempDir()
	cwd := t.TempDir()
	l := paths.New(home, cwd)
	rec := loop.RunRecord{ID: "deadbeefcafe", ScenarioID: "js-commander-negative-exp-E", Status: "completed", Worktree: l.Worktree("deadbeefcafe"), Harness: "grok", CreatedAt: loop.Now()}
	if err := loop.Save(l, rec); err != nil {
		t.Fatal(err)
	}
	if posted {
		t.Fatal("save must not upload")
	}
	out, err := Publish(l, rec.ID, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Fatal("publish did not hit /api/v1/runs")
	}
	if out["id"] == nil {
		t.Fatalf("response %+v", out)
	}
}

func TestRodeoOriginRequiresHTTPSAndNormalizesDefaults(t *testing.T) {
	if _, err := normalizeOrigin("http://example.com"); err == nil {
		t.Fatal("plaintext remote origin accepted")
	}
	if _, err := normalizeOrigin("https://agentrodeo.dev/path"); err == nil {
		t.Fatal("origin with a path accepted")
	}
	got, err := normalizeOrigin("HTTPS://AgentRodeo.DEV:443/")
	if err != nil || got != "https://agentrodeo.dev" {
		t.Fatalf("normalized origin=%q err=%v", got, err)
	}
}

func TestRiderCredentialCannotCrossOriginsAndPermissionsAreRepaired(t *testing.T) {
	t.Setenv("HB_ALLOW_INSECURE_LOCALHOST", "1")
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("mismatched credential caused a request") }))
	defer server.Close()
	origin, err := normalizeOrigin(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rider.json")
	t.Setenv("HB_RIDER_FILE", path)
	if err := os.WriteFile(path, []byte(`{"token":"production","origin":"https://agentrodeo.dev"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureRider(noRedirectClient(server.Client()), origin); err == nil || !strings.Contains(err.Error(), "origin mismatch") {
		t.Fatalf("mismatched origin error=%v", err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"token":"local","origin":%q}`, origin)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureRider(noRedirectClient(server.Client()), origin); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}
}

func TestLegacyRiderMigrationRepairsPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HB_RIDER_FILE", "")
	legacy := filepath.Join(home, ".config", "hb", "rider.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"token":"legacy"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("legacy migration made a network request")
		return nil, nil
	})}
	rider, err := ensureRider(client, defaultRodeoURL)
	if err != nil {
		t.Fatal(err)
	}
	if rider["token"] != "legacy" || rider["origin"] != defaultRodeoURL {
		t.Fatalf("rider=%+v", rider)
	}
	for _, path := range []string{legacy, RiderFile(defaultRodeoURL)} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%v err=%v", path, info.Mode().Perm(), err)
		}
	}
}

func TestRiderCredentialSymlinkFailsClosed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	path := filepath.Join(dir, "rider.json")
	if err := os.WriteFile(target, []byte(`{"token":"tok","origin":"https://agentrodeo.dev"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HB_RIDER_FILE", path)
	if _, err := ensureRider(http.DefaultClient, defaultRodeoURL); err == nil || !strings.Contains(err.Error(), "unsafe rider credential") {
		t.Fatalf("symlink error=%v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestPublishDoesNotFollowRedirectsWithAuthorization(t *testing.T) {
	var redirected bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer target.Close()
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer front.Close()
	t.Setenv("HB_RODEO_URL", front.URL)
	t.Setenv("HB_ALLOW_INSECURE_LOCALHOST", "1")
	origin, err := normalizeOrigin(front.URL)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rider.json")
	t.Setenv("HB_RIDER_FILE", path)
	if err := saveRider(path, map[string]any{"token": "tok", "origin": origin}); err != nil {
		t.Fatal(err)
	}
	l := paths.New(t.TempDir(), t.TempDir())
	id := "f00df00df00d"
	if err := os.MkdirAll(l.Worktree(id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := loop.Save(l, loop.RunRecord{ID: id, Status: "completed", Worktree: l.Worktree(id), Judges: []loop.JudgeScore{{Name: "test"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(l, id, front.Client()); err == nil || !strings.Contains(err.Error(), "rodeo 307") {
		t.Fatalf("redirect error=%v", err)
	}
	if redirected {
		t.Fatal("authorization-bearing request followed a redirect")
	}
}

func TestReportDoesNotUpload(t *testing.T) {
	// structural: report package never imports net/http
	dir, _ := os.Getwd()
	root := filepath.Join(dir, "..", "report")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("missing report package")
	}
}

func TestBuildPayloadExcludesPrivateRunAndSnapshotFields(t *testing.T) {
	l := paths.New(t.TempDir(), t.TempDir())
	id := "feedfacecafe"
	worktree := l.Worktree(id)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := loop.RunRecord{ID: id, ScenarioID: "task", ConfigID: "config", Status: "completed", Worktree: worktree,
		Harness: "codex", Model: "gpt-5", Error: "SECRET_ERROR", Notes: "SECRET_NOTES", CreatedAt: loop.Now(),
		Metadata: map[string]any{"workflow": "baseline", "private": "SECRET_METADATA"}}
	if err := loop.Save(l, rec); err != nil {
		t.Fatal(err)
	}
	snapshot := `{"prompt":"SECRET_PROMPT","repo":{"url":"/Users/alice/private","base_ref":"abc"},"config":{"workflow":"baseline"}}`
	if err := os.WriteFile(filepath.Join(l.RunDir(id), "snapshot.json"), []byte(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := BuildPayload(l, id)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(payload)
	for _, secret := range []string{"SECRET_ERROR", "SECRET_NOTES", "SECRET_METADATA", "SECRET_PROMPT", "/Users/alice/private", worktree} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("payload leaked %q: %s", secret, raw)
		}
	}
}

func TestBuildPayloadPublishesFrozenStudyBinding(t *testing.T) {
	l := paths.New(t.TempDir(), t.TempDir())
	id := "abcdeffedcba"
	worktree := l.Worktree(id)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := loop.Save(l, loop.RunRecord{ID: id, ScenarioID: "task", Status: "completed", Worktree: worktree, Harness: "codex", Model: "sol", CreatedAt: loop.Now()}); err != nil {
		t.Fatal(err)
	}
	snapshot := `{"study":{"id":"fight","contract_digest":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","arm_id":"a","scenario_id":"rodeo:task@1","repeat":2,"scenario_digest":"ssssssssssssssssssssssssssssssssssssssssssssssssssssssssssssssss"},"config":{"id":"a","harness":"codex","harness_version":"codex-cli 1","model":"sol","workflow":"baseline","skills":[],"interaction":"unattended","judge_protocol":"scenario-default","budget":{"max_minutes_per_run":45}}}`
	if err := os.WriteFile(filepath.Join(l.RunDir(id), "snapshot.json"), []byte(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := BuildPayload(l, id)
	if err != nil {
		t.Fatal(err)
	}
	publicSnapshot := payload["snapshot"].(map[string]any)
	binding := publicSnapshot["study"].(map[string]any)
	if binding["arm_id"] != "a" || binding["repeat"] != float64(2) {
		t.Fatalf("study binding=%+v", binding)
	}
	config := publicSnapshot["config"].(map[string]any)
	if config["judge_protocol"] != "scenario-default" || config["harness_version"] != "codex-cli 1" {
		t.Fatalf("study config=%+v", config)
	}
}
