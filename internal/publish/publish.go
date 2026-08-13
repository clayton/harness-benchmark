package publish

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func RodeoURL() string {
	if u := os.Getenv("HB_RODEO_URL"); u != "" {
		return u
	}
	return "https://agentrodeo.dev"
}

func RiderFile() string {
	if p := os.Getenv("HB_RIDER_FILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "hb", "rider.json")
}

func Publish(l paths.Layout, id string, client *http.Client) (map[string]any, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	rec, err := loop.Load(l, id)
	if err != nil {
		return nil, err
	}
	rider, err := ensureRider(client)
	if err != nil {
		return nil, err
	}
	snap := map[string]any{}
	if raw, err := os.ReadFile(filepath.Join(l.RunDir(id), "snapshot.json")); err == nil {
		_ = json.Unmarshal(raw, &snap)
	}
	payload := map[string]any{"run": rec, "snapshot": snap}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, RodeoURL()+"/api/v1/runs", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+fmt.Sprint(rider["token"]))
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rodeo %d: %s", resp.StatusCode, raw)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

func ensureRider(client *http.Client) (map[string]any, error) {
	path := RiderFile()
	if raw, err := os.ReadFile(path); err == nil {
		var existing map[string]any
		if json.Unmarshal(raw, &existing) == nil && existing["token"] != nil {
			return existing, nil
		}
	}
	req, err := http.NewRequest(http.MethodPost, RodeoURL()+"/api/v1/riders", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rodeo rider %d: %s", resp.StatusCode, raw)
	}
	var created map[string]any
	if err := json.Unmarshal(raw, &created); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	pretty, _ := json.MarshalIndent(created, "", "  ")
	if err := os.WriteFile(path, append(pretty, '\n'), 0o600); err != nil {
		return nil, err
	}
	return created, nil
}
