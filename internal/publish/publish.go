package publish

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
)

const defaultRodeoURL = "https://agentrodeo.dev"

func ValidatedRodeoURL() (string, error) {
	if u := os.Getenv("HB_RODEO_URL"); u != "" {
		return normalizeOrigin(u)
	}
	return defaultRodeoURL, nil
}

func RiderFile(origin string) string {
	if p := os.Getenv("HB_RIDER_FILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(origin)))
	return filepath.Join(home, ".config", "hb", "riders", digest+".json")
}

func Publish(l paths.Layout, id string, client *http.Client) (map[string]any, error) {
	origin, err := ValidatedRodeoURL()
	if err != nil {
		return nil, err
	}
	client = noRedirectClient(client)
	payload, err := BuildPayload(l, id)
	if err != nil {
		return nil, err
	}
	rider, err := ensureRider(client, origin)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, origin+"/api/v1/runs", bytes.NewReader(body))
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

func AuthenticatedJSON(method, endpoint string, payload any, client *http.Client) (map[string]any, error) {
	origin, err := ValidatedRodeoURL()
	if err != nil {
		return nil, err
	}
	client = noRedirectClient(client)
	rider, err := ensureRider(client, origin)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, origin+endpoint, bytes.NewReader(body))
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
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("invalid HB_RODEO_URL origin")
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	if scheme != "https" {
		local := hostname == "localhost" || net.ParseIP(hostname) != nil && net.ParseIP(hostname).IsLoopback()
		if scheme != "http" || !local || os.Getenv("HB_ALLOW_INSECURE_LOCALHOST") != "1" {
			return "", fmt.Errorf("HB_RODEO_URL must use HTTPS (set HB_ALLOW_INSECURE_LOCALHOST=1 only for loopback development)")
		}
	}
	port := parsed.Port()
	if scheme == "https" && port == "443" || scheme == "http" && port == "80" {
		port = ""
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	}
	return (&url.URL{Scheme: scheme, Host: host}).String(), nil
}

func noRedirectClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copy := *client
	copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &copy
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
		"total_tokens":   rec.Telemetry.TotalTokens,
		"usage_by_agent": rec.Telemetry.UsageByAgent, "cost_kind": rec.Telemetry.CostKind,
		"price_snapshot": rec.Telemetry.PriceSnapshot, "complete": rec.Telemetry.Complete,
		"token_complete": rec.Telemetry.TokenComplete,
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
			if study, ok := source["study"].(map[string]any); ok {
				snapshot["study"] = study
			}
			if repo, ok := source["repo"].(map[string]any); ok {
				snapshot["repo"] = map[string]any{"base_ref": repo["base_ref"], "gold_ref": repo["gold_ref"]}
			}
			if config, ok := source["config"].(map[string]any); ok {
				publicConfig := map[string]any{
					"id": config["id"], "harness": config["harness"], "model": config["model"],
					"workflow": config["workflow"], "skills": config["skills"], "interaction": config["interaction"],
				}
				if judgeProtocol, _ := config["judge_protocol"].(string); judgeProtocol != "" {
					publicConfig["judge_protocol"] = judgeProtocol
				}
				for _, key := range []string{"harness_version", "provider", "reasoning", "extensions", "plugins", "tools", "subagent_topology", "budget", "environment", "network"} {
					if meaningfulPublicConfigValue(config[key]) {
						publicConfig[key] = config[key]
					}
				}
				snapshot["config"] = publicConfig
			}
		}
	}
	return map[string]any{"schema": "hb.publish.v1", "run": run, "snapshot": snapshot}, nil
}

func meaningfulPublicConfigValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func ensureRider(client *http.Client, origin string) (map[string]any, error) {
	path := RiderFile(origin)
	if raw, found, err := readSecureRider(path); err != nil {
		return nil, err
	} else if found {
		var existing map[string]any
		if json.Unmarshal(raw, &existing) == nil && existing["token"] != nil {
			if existing["origin"] != origin {
				return nil, fmt.Errorf("rider credential origin mismatch")
			}
			return existing, nil
		}
	}
	if os.Getenv("HB_RIDER_FILE") == "" && origin == defaultRodeoURL {
		home, _ := os.UserHomeDir()
		legacy := filepath.Join(home, ".config", "hb", "rider.json")
		if raw, found, err := readSecureRider(legacy); err != nil {
			return nil, err
		} else if found {
			var existing map[string]any
			if json.Unmarshal(raw, &existing) == nil && existing["token"] != nil {
				existing["origin"] = origin
				if err := saveRider(path, existing); err != nil {
					return nil, err
				}
				return existing, nil
			}
		}
	}
	req, err := http.NewRequest(http.MethodPost, origin+"/api/v1/riders", nil)
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
	created["origin"] = origin
	if err := saveRider(path, created); err != nil {
		return nil, err
	}
	return created, nil
}

func readSecureRider(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect rider credential: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("unsafe rider credential file %q", path)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, false, fmt.Errorf("secure rider credential: %w", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("read rider credential: %w", err)
	}
	return raw, true, nil
}

func saveRider(path string, rider map[string]any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	pretty, err := json.MarshalIndent(rider, "", "  ")
	if err != nil {
		return err
	}
	return loop.WriteFileAtomic(path, append(pretty, '\n'), 0o600)
}
