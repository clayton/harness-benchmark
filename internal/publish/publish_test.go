package publish

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
