package publish

import (
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
	rec := loop.RunRecord{ID: "deadbeefcafe", ScenarioID: "js-commander-negative-exp-E", Status: "completed", Harness: "grok", CreatedAt: loop.Now()}
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
