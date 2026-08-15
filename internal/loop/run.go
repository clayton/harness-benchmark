package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func CreateRun(l paths.Layout, sc corpus.Scenario, harness string, runSetup bool) (RunRecord, error) {
	return CreateRunWithModel(l, sc, harness, "", runSetup)
}

func CreateRunWithModel(l paths.Layout, sc corpus.Scenario, harness, model string, runSetup bool) (RunRecord, error) {
	id := NewID()
	wt, err := PrepareWorktree(l, sc, id, runSetup)
	if err != nil {
		return RunRecord{}, err
	}
	if model == "" {
		model = defaultModel(harness)
	}
	sum := sha256.Sum256([]byte(stringsTrim(sc.Prompt)))
	rec := RunRecord{
		ID:         id,
		ScenarioID: sc.ID,
		ConfigID:   harness + "-baseline",
		Status:     "pending",
		Worktree:   wt,
		Harness:    harness,
		Model:      model,
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
	case "cursor":
		return "composer-2.5"
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
	return HeadlessLaunch(harness, "")
}

func HeadlessLaunch(harness, model string) string {
	switch harness {
	case "grok":
		cmd := "grok --always-approve --max-turns 80 --output-format json --permission-mode bypassPermissions --prompt-file HB_PROMPT.txt"
		if model != "" {
			cmd = "grok -m " + shellTok(model) + " --always-approve --max-turns 80 --output-format json --permission-mode bypassPermissions --prompt-file HB_PROMPT.txt"
		}
		return cmd
	case "pi":
		cmd := `pi -p --mode json --approve --no-session --no-extensions --no-skills`
		provider, mid := splitPiModel(model)
		if provider != "" {
			cmd += " --provider " + shellTok(provider)
		}
		if mid != "" {
			cmd += " --model " + shellTok(mid)
		}
		cmd += ` --append-system-prompt "UNATTENDED BENCHMARK MODE: Do not ask the user any questions. The task is fully specified in the user message. Work only in the current working directory. Do not create git worktrees outside this directory. Implement the fix, verify, then stop." "$(cat HB_PROMPT.txt)"`
		return cmd
	case "claude":
		return `claude -p --output-format json --dangerously-skip-permissions "$(cat HB_PROMPT.txt)"`
	case "codex":
		return `codex exec --skip-git-repo-check "$(cat HB_PROMPT.txt)"`
	case "cursor":
		mid := model
		if mid == "" {
			mid = "composer-2.5"
		}
		return "cursor-agent -p --output-format text --force --model " + shellTok(mid) + ` "$(cat HB_PROMPT.txt)"`
	default:
		return ""
	}
}

func splitPiModel(model string) (provider, id string) {
	model = stringsTrim(model)
	if model == "" {
		return "", ""
	}
	if i := indexByte(model, '/'); i >= 0 {
		left, right := model[:i], model[i+1:]
		if left == "x-ai" || left == "openrouter" {
			return "openrouter", model
		}
		return left, right
	}
	if len(model) >= 4 && model[:4] == "grok" {
		return "xai", model
	}
	return "", model
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func shellTok(s string) string {
	if s == "" {
		return s
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if !(ch == '-' || ch == '.' || ch == '_' || ch == '/' || ch == ':' ||
			ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9') {
			return "'" + s + "'"
		}
	}
	return s
}

func ScenarioForRun(l paths.Layout, officialDir, id string) (corpus.Scenario, error) {
	rec, err := Load(l, id)
	if err != nil {
		return corpus.Scenario{}, err
	}
	if rec.ScenarioID != "" {
		if sc, err := corpus.Find(officialDir, rec.ScenarioID); err == nil {
			return sc, nil
		}
	}
	return scenarioFromSnapshot(l, id)
}

func scenarioFromSnapshot(l paths.Layout, id string) (corpus.Scenario, error) {
	raw, err := os.ReadFile(filepath.Join(l.RunDir(id), "snapshot.json"))
	if err != nil {
		return corpus.Scenario{}, fmt.Errorf("scenario not found for run %s", id)
	}
	var wrap struct {
		Scenario json.RawMessage `json:"scenario"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return corpus.Scenario{}, err
	}
	sc, err := corpus.UnmarshalScenarioJSON(wrap.Scenario)
	if err != nil {
		return corpus.Scenario{}, err
	}
	if sc.ID == "" {
		return corpus.Scenario{}, fmt.Errorf("scenario not found for run %s", id)
	}
	return sc, nil
}

func ResolveRunID(l paths.Layout, id string) (string, error) {
	if id != "" && id != "latest" {
		return id, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		if rid := RunIDFromPath(cwd, l.OutDir); rid != "" {
			return rid, nil
		}
	}
	return LatestID(l)
}

func MustOutDir(l paths.Layout) error {
	return os.MkdirAll(l.OutDir, 0o755)
}
