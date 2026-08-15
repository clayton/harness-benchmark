package loop

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/clayton/harness-benchmark/internal/paths"
)

type RunRecord struct {
	ID             string         `json:"id"`
	ScenarioID     string         `json:"scenario_id"`
	ConfigID       string         `json:"config_id"`
	Status         string         `json:"status"`
	Worktree       string         `json:"worktree"`
	Harness        string         `json:"harness"`
	HarnessVersion string         `json:"harness_version,omitempty"`
	Model          string         `json:"model,omitempty"`
	Error          string         `json:"error,omitempty"`
	Notes          string         `json:"notes,omitempty"`
	Judges         []JudgeScore   `json:"judges,omitempty"`
	Telemetry      Telemetry      `json:"telemetry"`
	PatchStats     map[string]any `json:"patch_stats,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      string         `json:"created_at"`
	FinishedAt     string         `json:"finished_at,omitempty"`
}

type JudgeScore struct {
	Name   string  `json:"name"`
	Score  float64 `json:"score"`
	Passed *bool   `json:"passed"`
	Notes  string  `json:"notes,omitempty"`
}

type Telemetry struct {
	WallMS           int      `json:"wall_ms,omitempty"`
	TokensIn         *int     `json:"tokens_in"`
	TokensOut        *int     `json:"tokens_out"`
	EstimatedUSD     *float64 `json:"estimated_usd"`
	Turns            *int     `json:"turns"`
	ReasoningTokens  *int     `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  *int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int     `json:"cache_write_tokens,omitempty"`
	TotalTokens      *int     `json:"total_tokens,omitempty"`
}

func NewID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func Save(l paths.Layout, r RunRecord) error {
	dir := l.RunDir(r.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "run.json"), append(raw, '\n'), 0o644)
}

func Load(l paths.Layout, id string) (RunRecord, error) {
	raw, err := os.ReadFile(filepath.Join(l.RunDir(id), "run.json"))
	if err != nil {
		return RunRecord{}, fmt.Errorf("run not found: %s\nlooked in %s", id, l.OutDir)
	}
	var r RunRecord
	if err := json.Unmarshal(raw, &r); err != nil {
		return RunRecord{}, err
	}
	return r, nil
}

func LatestID(l paths.Layout) (string, error) {
	raw, err := os.ReadFile(l.LatestFile())
	if err != nil {
		return "", fmt.Errorf("no latest run in %s", l.OutDir)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("no latest run in %s", l.OutDir)
	}
	return string(bytesTrim(raw)), nil
}

func LatestRecord(l paths.Layout) (RunRecord, error) {
	id, err := LatestID(l)
	if err != nil {
		return RunRecord{}, err
	}
	return Load(l, id)
}

// RunIDFromPath returns the run id if path is inside that run's directory
// (workspace, run dir, or a child). Empty when path is not under OutDir.
func RunIDFromPath(path, outDir string) string {
	if path == "" || outDir == "" {
		return ""
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		absOut = outDir
	}
	rel, err := filepath.Rel(absOut, absPath)
	if err != nil || rel == "." || rel == "" {
		return ""
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	parts := splitPath(rel)
	if len(parts) == 0 {
		return ""
	}
	id := parts[0]
	if _, err := os.Stat(filepath.Join(absOut, id, "run.json")); err != nil {
		return ""
	}
	return id
}

func splitPath(p string) []string {
	var out []string
	for p != "" && p != "." {
		base := filepath.Base(p)
		if base == "." || base == string(filepath.Separator) {
			break
		}
		out = append([]string{base}, out...)
		next := filepath.Dir(p)
		if next == p {
			break
		}
		p = next
	}
	return out
}

func SetLatest(l paths.Layout, id string) error {
	if err := os.MkdirAll(l.OutDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(l.LatestFile(), []byte(id), 0o644)
}

func bytesTrim(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\t' || b[j-1] == '\r') {
		j--
	}
	return b[i:j]
}

func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
