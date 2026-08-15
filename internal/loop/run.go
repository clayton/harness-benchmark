package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

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
	interaction := "unattended"
	if harness == "manual" {
		interaction = "human"
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
			"interaction":      interaction,
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
			"id":          rec.ConfigID,
			"harness":     harness,
			"model":       model,
			"workflow":    "baseline",
			"skills":      []string{},
			"interaction": interaction,
		},
		"repo": map[string]any{
			"url":      sc.Repo.URL,
			"base_ref": sc.Repo.BaseRef,
			"gold_ref": sc.Repo.GoldRef,
		},
	}
	raw, _ := json.MarshalIndent(snap, "", "  ")
	_ = WriteFileAtomic(filepath.Join(l.RunDir(id), "snapshot.json"), append(raw, '\n'), 0o644)
	_ = WriteFileAtomic(filepath.Join(l.RunDir(id), "prompt.txt"), []byte(stringsTrim(sc.Prompt)+"\n"), 0o600)
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

type LaunchSpec struct {
	Program string
	Args    []string
}

var modelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

func HeadlessCommand(harness string) string {
	return HeadlessLaunch(harness, "", "").Program
}

// HeadlessLaunch returns argv, never a shell command. Prompt text is passed as
// one argument (or via the harness' prompt-file option), so model and prompt
// values cannot be interpreted by a shell.
func HeadlessLaunch(harness, model, prompt string) LaunchSpec {
	if model != "" && !modelIDPattern.MatchString(model) {
		return LaunchSpec{}
	}
	switch harness {
	case "grok":
		args := []string{"--always-approve", "--max-turns", "80", "--output-format", "json", "--permission-mode", "bypassPermissions", "--prompt-file", "HB_PROMPT.txt"}
		if model != "" {
			args = append([]string{"-m", model}, args...)
		}
		return LaunchSpec{Program: "grok", Args: args}
	case "pi":
		args := []string{"-p", "--mode", "json", "--approve", "--no-session", "--no-extensions", "--no-skills"}
		provider, mid := splitPiModel(model)
		if provider != "" {
			args = append(args, "--provider", provider)
		}
		if mid != "" {
			args = append(args, "--model", mid)
		}
		args = append(args, "--append-system-prompt", "UNATTENDED BENCHMARK MODE: Do not ask the user any questions. The task is fully specified in the user message. Work only in the current working directory. Do not create git worktrees outside this directory. Implement the fix, verify, then stop.", prompt)
		return LaunchSpec{Program: "pi", Args: args}
	case "claude":
		return LaunchSpec{Program: "claude", Args: []string{"-p", "--output-format", "json", "--dangerously-skip-permissions", prompt}}
	case "codex":
		return LaunchSpec{Program: "codex", Args: []string{"exec", "--skip-git-repo-check", prompt}}
	case "cursor":
		mid := model
		if mid == "" {
			mid = "composer-2.5"
		}
		return LaunchSpec{Program: "cursor-agent", Args: []string{"-p", "--output-format", "text", "--force", "--model", mid, prompt}}
	default:
		return LaunchSpec{}
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
