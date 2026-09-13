package publish

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
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

func TestPublisherKeysAreSecureOriginScopedAndValidated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HB_RIDER_FILE", filepath.Join(t.TempDir(), "shared-rider.json"))
	firstOrigin := "https://one.example"
	secondOrigin := "https://two.example"
	first, _, err := publisherKey(firstOrigin)
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := publisherKey(firstOrigin)
	if err != nil || !bytes.Equal(first, again) {
		t.Fatalf("publisher key was not reused: %v", err)
	}
	second, _, err := publisherKey(secondOrigin)
	if err != nil || bytes.Equal(first, second) {
		t.Fatalf("origins shared a publisher key: %v", err)
	}
	for _, origin := range []string{firstOrigin, secondOrigin} {
		path := PublisherKeyFile(origin)
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 || strings.Contains(path, "shared-rider") {
			t.Fatalf("publisher path=%s mode=%v", path, info.Mode().Perm())
		}
	}

	badOrigin := "https://bad.example"
	badPath := PublisherKeyFile(badOrigin)
	if err := os.MkdirAll(filepath.Dir(badPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPath, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := publisherKey(badOrigin); err == nil {
		t.Fatal("malformed publisher key was accepted")
	}

	rsaOrigin := "https://rsa.example"
	rsaPath := PublisherKeyFile(rsaOrigin)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rsaPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := publisherKey(rsaOrigin); err == nil || !strings.Contains(err.Error(), "not Ed25519") {
		t.Fatalf("non-Ed25519 key error=%v", err)
	}
}

func TestPublisherKeySymlinkFailsClosed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origin := "https://symlink.example"
	path := PublisherKeyFile(origin)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(target, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := publisherKey(origin); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink error=%v", err)
	}
}

func TestPublishPostsOnlyWhenCalled(t *testing.T) {
	var posted bool
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			evidence := payload["evidence"].(map[string]any)
			if evidence["schema"] != "hb.evidence.v1" {
				t.Fatalf("evidence schema=%v", evidence["schema"])
			}
			canonical, err := canonicalJSON(map[string]any{"run": payload["run"], "snapshot": payload["snapshot"]})
			if err != nil {
				t.Fatal(err)
			}
			if evidence["canonical_payload"] != string(canonical) {
				t.Fatal("published evidence does not carry the signed envelope")
			}
			block, _ := pem.Decode([]byte(evidence["public_key"].(string)))
			parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			signature, err := base64.StdEncoding.DecodeString(evidence["signature"].(string))
			if err != nil || !ed25519.Verify(parsed.(ed25519.PublicKey), canonical, signature) {
				t.Fatalf("published signature invalid: %v", err)
			}
			sum := sha256.Sum256(canonical)
			if evidence["payload_sha256"] != hex.EncodeToString(sum[:]) {
				t.Fatal("published digest does not bind Rails-compatible envelope")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "url": srv.URL + "/runs/1", "unofficial": false})
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
	rec := loop.RunRecord{ID: "deadbeefcafe", ScenarioID: "js-commander-negative-exp-E", Status: "completed", Worktree: l.Worktree("deadbeefcafe"), Harness: "grok", Judges: []loop.JudgeScore{{Name: "test"}}, CreatedAt: loop.Now()}
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
	if out["id"] == nil || out["url"] != srv.URL+"/runs/1" {
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
		Judges: []loop.JudgeScore{{Name: "test"}}, Metadata: map[string]any{"workflow": "baseline", "private": "SECRET_METADATA"}}
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

func TestBuildPayloadPublishesOnlyBoundedPatchArtifact(t *testing.T) {
	l := paths.New(t.TempDir(), t.TempDir())
	id := "decafbadcafe"
	worktree := l.Worktree(id)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := loop.Save(l, loop.RunRecord{ID: id, Status: "completed", Worktree: worktree, Judges: []loop.JudgeScore{{Name: "test"}}, CreatedAt: loop.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.RunDir(id), "agent.log"), []byte("PRIVATE_AGENT_LOG"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := BuildPayload(l, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := payload["run"].(map[string]any)["patch_artifact"]; present {
		t.Fatal("missing patch.diff produced an artifact")
	}
	if err := os.WriteFile(filepath.Join(l.RunDir(id), "patch.diff"), []byte("diff --git a/a b/a\n+x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err = BuildPayload(l, id)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(payload)
	if !bytes.Contains(raw, []byte(`"patch_artifact"`)) || !bytes.Contains(raw, []byte(`+x`)) || bytes.Contains(raw, []byte("PRIVATE_AGENT_LOG")) {
		t.Fatalf("artifact payload=%s", raw)
	}
	if err := os.WriteFile(filepath.Join(l.RunDir(id), "patch.diff"), bytes.Repeat([]byte("x"), (1<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPayload(l, id); err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("oversized patch error=%v", err)
	}
}

func TestBuildPayloadPublishesFrozenStudyBinding(t *testing.T) {
	l := paths.New(t.TempDir(), t.TempDir())
	id := "abcdeffedcba"
	worktree := l.Worktree(id)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := loop.Save(l, loop.RunRecord{ID: id, ScenarioID: "task", Status: "completed", Worktree: worktree, Harness: "codex", Model: "sol", ModelVersion: "rev-1", Judges: []loop.JudgeScore{{Name: "test"}}, CreatedAt: loop.Now()}); err != nil {
		t.Fatal(err)
	}
	snapshot := `{"study":{"id":"fight","contract_digest":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","arm_id":"a","scenario_id":"rodeo:task@1","repeat":2,"scenario_digest":"ssssssssssssssssssssssssssssssssssssssssssssssssssssssssssssssss"},"config":{"id":"a","harness":"codex","harness_version":"codex-cli 1","model":"sol","model_version":"rev-1","workflow":"baseline","skills":[],"interaction":"unattended","judge_protocol":"scenario-default","budget":{"max_minutes_per_run":45},"runtime":{"name":"docker","version":"29","architecture":"arm64"},"prompt_treatment":{"placement":"user_append"},"adapter":{"schema":"hb.adapter.v1","id":"demo","version":"1","command":["demo","${prompt}"]},"adapter_manifest":"/Users/alice/private/adapter.yaml","assurance":{"harness":"declared"}}}`
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
	if config["judge_protocol"] != "scenario-default" || config["harness_version"] != "codex-cli 1" || config["model_version"] != "rev-1" || config["runtime"].(map[string]any)["name"] != "docker" {
		t.Fatalf("study config=%+v", config)
	}
	if config["prompt_treatment"].(map[string]any)["placement"] != "user_append" || config["assurance"].(map[string]any)["harness"] != "declared" {
		t.Fatalf("v2 config=%+v", config)
	}
	raw, _ := json.Marshal(payload)
	if bytes.Contains(raw, []byte("adapter_manifest")) || bytes.Contains(raw, []byte("/Users/alice")) {
		t.Fatalf("payload leaked adapter path: %s", raw)
	}
	unsafeSnapshot := `{"config":{"adapter":{"schema":"hb.adapter.v1","id":"demo","version":"1","command":["agent-cli","API_KEY=do-not-publish"]}}}`
	if err := os.WriteFile(filepath.Join(l.RunDir(id), "snapshot.json"), []byte(unsafeSnapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPayload(l, id); err == nil || !strings.Contains(err.Error(), "public adapter is unsafe") {
		t.Fatalf("unsafe adapter error=%v", err)
	}
}

func TestBuildPayloadStripsFrozenSkillPathsButKeepsIdentity(t *testing.T) {
	l := paths.New(t.TempDir(), t.TempDir())
	id := "faceb00c1234"
	worktree := l.Worktree(id)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := loop.Save(l, loop.RunRecord{ID: id, ScenarioID: "task", Status: "completed", Worktree: worktree, Harness: "pi", Model: "sol", Judges: []loop.JudgeScore{{Name: "test"}}, CreatedAt: loop.Now()}); err != nil {
		t.Fatal(err)
	}
	snapshot := `{"config":{"mode":"personal","frozen_skills":[{"name":"review","path":"/Users/alice/.pi/skills/review","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","files":2}],"config_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","config_status":"frozen"}}`
	if err := os.WriteFile(filepath.Join(l.RunDir(id), "snapshot.json"), []byte(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := BuildPayload(l, id)
	if err != nil {
		t.Fatal(err)
	}
	config := payload["snapshot"].(map[string]any)["config"].(map[string]any)
	if config["mode"] != "personal" || config["config_sha256"] != strings.Repeat("b", 64) {
		t.Fatalf("config=%+v", config)
	}
	frozen := config["frozen_skills"].([]map[string]any)
	if len(frozen) != 1 || frozen[0]["sha256"] != strings.Repeat("a", 64) {
		t.Fatalf("frozen=%+v", frozen)
	}
	raw, _ := json.Marshal(payload)
	if bytes.Contains(raw, []byte("/Users/alice")) || bytes.Contains(raw, []byte(`"path"`)) {
		t.Fatalf("payload leaked frozen skill path: %s", raw)
	}
}

func TestCanonicalOpenEvidenceVector(t *testing.T) {
	raw, err := os.ReadFile("testdata/open_evidence_vector.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Payload       map[string]any `json:"payload"`
		Canonical     string         `json:"canonical"`
		SeedHex       string         `json:"seed_hex"`
		PublicKey     string         `json:"public_key"`
		Signature     string         `json:"signature"`
		PayloadSHA256 string         `json:"payload_sha256"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalJSON(vector.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != vector.Canonical {
		t.Fatalf("canonical=%q want %q", got, vector.Canonical)
	}
	seed, err := hex.DecodeString(vector.SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	private := ed25519.NewKeyFromSeed(seed)
	signature := ed25519.Sign(private, got)
	if base64.StdEncoding.EncodeToString(signature) != vector.Signature {
		t.Fatal("signature vector drift")
	}
	sum := sha256.Sum256(got)
	if hex.EncodeToString(sum[:]) != vector.PayloadSHA256 {
		t.Fatal("payload digest vector drift")
	}
	block, _ := pem.Decode([]byte(vector.PublicKey))
	if block == nil {
		t.Fatal("invalid vector public key")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	public, ok := parsed.(ed25519.PublicKey)
	if !ok || !ed25519.Verify(public, got, signature) {
		t.Fatal("vector signature does not verify")
	}
}
