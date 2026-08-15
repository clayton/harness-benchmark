package loop

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	WallMS           int           `json:"wall_ms,omitempty"`
	TokensIn         *int          `json:"tokens_in"`
	TokensOut        *int          `json:"tokens_out"`
	EstimatedUSD     *float64      `json:"estimated_usd"`
	Turns            *int          `json:"turns"`
	ReasoningTokens  *int          `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  *int          `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int          `json:"cache_write_tokens,omitempty"`
	TotalTokens      *int          `json:"total_tokens,omitempty"`
	UsageByAgent     *[]AgentUsage `json:"usage_by_agent,omitempty"`
	CostKind         string        `json:"cost_kind,omitempty"`
	PriceSnapshot    string        `json:"price_snapshot,omitempty"`
	Complete         *bool         `json:"complete,omitempty"`
	TokenComplete    *bool         `json:"token_complete,omitempty"`
}

type AgentUsage struct {
	AgentID         string   `json:"agent_id"`
	Model           string   `json:"model,omitempty"`
	TokensIn        int      `json:"tokens_in,omitempty"`
	TokensOut       int      `json:"tokens_out,omitempty"`
	ReasoningTokens int      `json:"reasoning_tokens,omitempty"`
	CacheReadTokens int      `json:"cache_read_tokens,omitempty"`
	TotalTokens     int      `json:"total_tokens,omitempty"`
	EstimatedUSD    *float64 `json:"estimated_usd,omitempty"`
}

func NewID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

var runIDPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

func ValidateRunID(id string) error {
	if !runIDPattern.MatchString(id) {
		return fmt.Errorf("invalid run id %q (want 12 lowercase hex characters)", id)
	}
	return nil
}

func Save(l paths.Layout, r RunRecord) error {
	if err := ValidateRunID(r.ID); err != nil {
		return err
	}
	dir := l.RunDir(r.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe run directory: %s", dir)
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(filepath.Join(dir, "run.json"), append(raw, '\n'), 0o644)
}

func Load(l paths.Layout, id string) (RunRecord, error) {
	if err := ValidateRunID(id); err != nil {
		return RunRecord{}, err
	}
	dir := l.RunDir(id)
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return RunRecord{}, fmt.Errorf("run not found: %s\nlooked in %s", id, l.OutDir)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return RunRecord{}, fmt.Errorf("run not found: %s\nlooked in %s", id, l.OutDir)
	}
	var r RunRecord
	if err := json.Unmarshal(raw, &r); err != nil {
		return RunRecord{}, err
	}
	if r.ID != id {
		return RunRecord{}, fmt.Errorf("run record id %q does not match directory %q", r.ID, id)
	}
	expectedWorkspace := filepath.Clean(l.Worktree(id))
	if !samePath(r.Worktree, expectedWorkspace) {
		return RunRecord{}, fmt.Errorf("run %s has unexpected workspace %q", id, r.Worktree)
	}
	if info, err := os.Lstat(expectedWorkspace); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return RunRecord{}, fmt.Errorf("run %s workspace is a symlink", id)
	}
	return r, nil
}

func samePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if left == right {
		return true
	}
	resolvedLeft, leftErr := filepath.EvalSymlinks(left)
	resolvedRight, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && resolvedLeft == resolvedRight
}

func LatestID(l paths.Layout) (string, error) {
	raw, err := os.ReadFile(l.LatestFile())
	if err != nil {
		return "", fmt.Errorf("no latest run in %s", l.OutDir)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("no latest run in %s", l.OutDir)
	}
	id := string(bytesTrim(raw))
	if err := ValidateRunID(id); err != nil {
		return "", fmt.Errorf("invalid latest run: %w", err)
	}
	return id, nil
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
	if ValidateRunID(id) != nil {
		return ""
	}
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
	if err := ValidateRunID(id); err != nil {
		return err
	}
	if err := os.MkdirAll(l.OutDir, 0o755); err != nil {
		return err
	}
	return WriteFileAtomic(l.LatestFile(), []byte(id), 0o644)
}

func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hb-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
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
