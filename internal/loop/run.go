package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func CreateRun(l paths.Layout, sc corpus.Scenario, harness string, runSetup bool) (RunRecord, error) {
	id := NewID()
	wt, err := PrepareWorktree(l, sc, id, runSetup)
	if err != nil {
		return RunRecord{}, err
	}
	sum := sha256.Sum256([]byte(stringsTrim(sc.Prompt)))
	rec := RunRecord{
		ID:         id,
		ScenarioID: sc.ID,
		ConfigID:   harness + "-baseline",
		Status:     "pending",
		Worktree:   wt,
		Harness:    harness,
		Model:      defaultModel(harness),
		Metadata: map[string]any{
			"workflow":         "baseline",
			"interaction":      "unattended",
			"prompt_sha256_16": hex.EncodeToString(sum[:])[:16],
			"base_ref":         sc.Repo.BaseRef,
			"gold_ref":         sc.Repo.GoldRef,
		},
		CreatedAt: Now(),
	}
	if err := Save(l, rec); err != nil {
		return RunRecord{}, err
	}
	snap := map[string]any{
		"schema":           "hb.snapshot.v1",
		"run_id":           id,
		"prompt_sha256_16": hex.EncodeToString(sum[:])[:16],
		"scenario":         sc,
		"config": map[string]any{
			"id":       rec.ConfigID,
			"harness":  harness,
			"workflow": "baseline",
		},
		"repo": map[string]any{
			"url":      sc.Repo.URL,
			"base_ref": sc.Repo.BaseRef,
			"gold_ref": sc.Repo.GoldRef,
		},
	}
	raw, _ := json.MarshalIndent(snap, "", "  ")
	_ = os.WriteFile(filepath.Join(l.RunDir(id), "snapshot.json"), append(raw, '\n'), 0o644)
	_ = os.WriteFile(filepath.Join(l.RunDir(id), "prompt.txt"), []byte(stringsTrim(sc.Prompt)+"\n"), 0o644)
	if err := SetLatest(l, id); err != nil {
		return RunRecord{}, err
	}
	return rec, nil
}

func defaultModel(harness string) string {
	switch harness {
	case "grok", "pi":
		return "grok-4.5"
	case "claude":
		return "claude-sonnet-4"
	case "codex":
		return "gpt-5"
	default:
		return ""
	}
}

func stringsTrim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func HeadlessCommand(harness string) string {
	switch harness {
	case "grok":
		return "grok --always-approve --max-turns 80 --output-format json --permission-mode bypassPermissions --prompt-file HB_PROMPT.txt"
	case "pi":
		return `pi -p --mode json --approve --no-session --no-extensions --no-skills "$(cat HB_PROMPT.txt)"`
	case "claude":
		return `claude -p --output-format json --dangerously-skip-permissions "$(cat HB_PROMPT.txt)"`
	case "codex":
		return `codex exec --skip-git-repo-check "$(cat HB_PROMPT.txt)"`
	default:
		return ""
	}
}

func ResolveRunID(l paths.Layout, id string) (string, error) {
	if id != "" && id != "latest" {
		return id, nil
	}
	return LatestID(l)
}

func MustOutDir(l paths.Layout) error {
	return os.MkdirAll(l.OutDir, 0o755)
}
