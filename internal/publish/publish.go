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
	payload, err := BuildPayload(l, id)
	if err != nil {
		return nil, err
	}
	rider, err := ensureRider(client)
	if err != nil {
		return nil, err
	}
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

// BuildPayload constructs the public hb.publish.v1 DTO. It deliberately does
// not serialize RunRecord or the scenario snapshot wholesale: those contain
// local paths, prompts, commands, errors, notes, and extension metadata.
func BuildPayload(l paths.Layout, id string) (map[string]any, error) {
	rec, err := loop.Load(l, id)
	if err != nil {
		return nil, err
	}
	judges := make([]map[string]any, 0, len(rec.Judges))
	for _, judge := range rec.Judges {
		judges = append(judges, map[string]any{"name": judge.Name, "score": judge.Score, "passed": judge.Passed})
	}
	telemetry := map[string]any{
		"wall_ms": rec.Telemetry.WallMS, "tokens_in": rec.Telemetry.TokensIn,
		"tokens_out": rec.Telemetry.TokensOut, "estimated_usd": rec.Telemetry.EstimatedUSD,
		"turns": rec.Telemetry.Turns, "reasoning_tokens": rec.Telemetry.ReasoningTokens,
		"cache_read_tokens": rec.Telemetry.CacheReadTokens, "cache_write_tokens": rec.Telemetry.CacheWriteTokens,
		"total_tokens": rec.Telemetry.TotalTokens,
	}
	workflow, _ := rec.Metadata["workflow"].(string)
	interaction, _ := rec.Metadata["interaction"].(string)
	run := map[string]any{
		"id": rec.ID, "scenario_id": rec.ScenarioID, "config_id": rec.ConfigID,
		"status": rec.Status, "harness": rec.Harness, "harness_version": rec.HarnessVersion,
		"model": rec.Model, "judges": judges, "telemetry": telemetry,
		"metadata": map[string]any{"workflow": workflow, "interaction": interaction},
	}
	snapshot := map[string]any{}
	if raw, readErr := os.ReadFile(filepath.Join(l.RunDir(id), "snapshot.json")); readErr == nil {
		var source map[string]any
		if json.Unmarshal(raw, &source) == nil {
			snapshot["prompt_sha256_16"] = source["prompt_sha256_16"]
			if repo, ok := source["repo"].(map[string]any); ok {
				snapshot["repo"] = map[string]any{"base_ref": repo["base_ref"], "gold_ref": repo["gold_ref"]}
			}
			if config, ok := source["config"].(map[string]any); ok {
				snapshot["config"] = map[string]any{
					"id": config["id"], "harness": config["harness"], "model": config["model"],
					"workflow": config["workflow"], "skills": config["skills"], "interaction": config["interaction"],
				}
			}
		}
	}
	return map[string]any{"schema": "hb.publish.v1", "run": run, "snapshot": snapshot}, nil
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
