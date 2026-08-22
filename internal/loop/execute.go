package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/clayton/harness-benchmark/internal/paths"
)

type ExecResult struct {
	ReturnCode int
	WallMS     int
	LogPath    string
	TimedOut   bool
}

type HarnessIdentity struct {
	Path    string
	Version string
}

func Execute(l paths.Layout, id string, timeout time.Duration) (ExecResult, error) {
	rec, err := Load(l, id)
	if err != nil {
		return ExecResult{}, err
	}
	promptRaw, err := os.ReadFile(filepath.Join(rec.Worktree, "HB_PROMPT.txt"))
	if err != nil {
		return ExecResult{}, fmt.Errorf("read run prompt: %w", err)
	}
	profile := Profile{Harness: rec.Harness, Model: rec.Model}
	if raw, ok := rec.Metadata["profile"]; ok {
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &profile)
	}
	resolvedProfile, resolveErr := ResolveMeasuredProfile(profile)
	if resolveErr != nil {
		return ExecResult{}, resolveErr
	}
	launch := HeadlessLaunchProfile(resolvedProfile, strings.TrimSpace(string(promptRaw)))
	if launch.Program == "" {
		if rec.Model != "" && !modelIDPattern.MatchString(rec.Model) {
			return ExecResult{}, fmt.Errorf("invalid model id %q", rec.Model)
		}
		return ExecResult{}, fmt.Errorf("%s has no headless launch; stay in this directory and run: hbench finish %s", rec.Harness, id)
	}
	if _, err := os.Stat(rec.Worktree); err != nil {
		return ExecResult{}, fmt.Errorf("workspace missing: %s", rec.Worktree)
	}
	actualVersion := DetectHarnessVersion(rec.Harness)
	if actualVersion == "" {
		return ExecResult{}, fmt.Errorf("could not resolve %s harness version", rec.Harness)
	}
	if rec.HarnessVersion != "" && rec.HarnessVersion != actualVersion {
		return ExecResult{}, fmt.Errorf("%s harness version drift: contract has %q, installed binary is %q", rec.Harness, rec.HarnessVersion, actualVersion)
	}
	rec.HarnessVersion = actualVersion
	if rec.Metadata == nil {
		rec.Metadata = map[string]any{}
	}
	budget := cloneAnyMap(profile.Budget)
	budget["max_minutes_per_run"] = int(timeout.Minutes())
	rec.Metadata["budget"] = budget
	updateSnapshotExecution(l, rec, timeout)
	rec.Status = "running"
	_ = Save(l, rec)

	logPath := filepath.Join(l.RunDir(id), "agent.log")
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return ExecResult{}, err
	}

	cmd := exec.Command(launch.Program, launch.Args...)
	cmd.Dir = rec.Worktree
	harnessEnv, envErr := isolatedHarnessEnv(l, rec)
	if envErr != nil {
		_ = logF.Close()
		return ExecResult{}, envErr
	}
	cmd.Env = append(append(minimalCommandEnv(), harnessEnv...), launch.Env...)
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	start := time.Now()
	err = cmd.Start()
	if err != nil {
		_ = logF.Close()
		rec.Status = "failed"
		rec.Error = err.Error()
		_ = Save(l, rec)
		return ExecResult{}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	timedOut := false
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case waitErr = <-done:
	case <-timer.C:
		timedOut = true
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
		waitErr = fmt.Errorf("timeout after %s", timeout)
	}
	wall := int(time.Since(start).Milliseconds())
	closeErr := logF.Close()
	rec.Telemetry = ExtractTelemetry(rec.Harness, logPath)
	if rec.Harness == "pi" {
		freezePiPriceSnapshot(&rec.Telemetry, filepath.Join(l.RunDir(id), "harness-home", "pi", "models-store.json"), profile.Provider, profile.Model)
	}
	if !profileChildUsageComplete(profile, rec.Telemetry) {
		complete := false
		rec.Telemetry.Complete = &complete
		rec.Telemetry.TokenComplete = &complete
	}
	rec.Telemetry.WallMS = wall
	if timedOut {
		rec.Status = "timeout"
		rec.Error = waitErr.Error()
	} else if waitErr != nil {
		rec.Status = "failed"
		rec.Error = waitErr.Error()
	}
	if saveErr := Save(l, rec); saveErr != nil && waitErr == nil {
		waitErr = saveErr
	}
	if closeErr != nil && waitErr == nil {
		waitErr = closeErr
	}
	rc := 0
	if waitErr != nil {
		if cmd.ProcessState != nil {
			rc = cmd.ProcessState.ExitCode()
		} else {
			rc = 1
		}
	}
	return ExecResult{ReturnCode: rc, WallMS: wall, LogPath: logPath, TimedOut: timedOut}, waitErr
}

func isolatedHarnessEnv(l paths.Layout, rec RunRecord) ([]string, error) {
	dir := filepath.Join(l.RunDir(rec.ID), "harness-home")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	xdg := filepath.Join(dir, ".config")
	if err := os.MkdirAll(xdg, 0o700); err != nil {
		return nil, err
	}
	base := []string{"HOME=" + dir, "XDG_CONFIG_HOME=" + xdg, "XDG_DATA_HOME=" + filepath.Join(dir, ".local", "share")}
	home, _ := os.UserHomeDir()
	switch rec.Harness {
	case "codex":
		codexHome := filepath.Join(dir, "codex")
		if err := os.MkdirAll(codexHome, 0o700); err != nil {
			return nil, err
		}
		if err := copyAuthFile(filepath.Join(home, ".codex", "auth.json"), filepath.Join(codexHome, "auth.json")); err != nil {
			return nil, err
		}
		return append(base, "CODEX_HOME="+codexHome), nil
	case "pi":
		piHome := filepath.Join(dir, "pi")
		if err := os.MkdirAll(piHome, 0o700); err != nil {
			return nil, err
		}
		if err := copyAuthFile(filepath.Join(home, ".pi", "agent", "auth.json"), filepath.Join(piHome, "auth.json")); err != nil {
			return nil, err
		}
		if err := copyAuthFile(filepath.Join(home, ".pi", "agent", "models.json"), filepath.Join(piHome, "models.json")); err != nil {
			return nil, err
		}
		if err := copyAuthFile(filepath.Join(home, ".pi", "agent", "models-store.json"), filepath.Join(piHome, "models-store.json")); err != nil {
			return nil, err
		}
		piEnv := append(base, "PI_CODING_AGENT_DIR="+piHome, "PI_CODING_AGENT_SESSION_DIR="+filepath.Join(piHome, "sessions"))
		if envName := providerAPIKeyEnv(profileProvider(rec)); envName != "" {
			if key := os.Getenv(envName); key != "" {
				piEnv = append(piEnv, envName+"="+key)
			}
		}
		return piEnv, nil
	case "cursor":
		if err := copyJSONFields(filepath.Join(home, ".cursor", "cli-config.json"), filepath.Join(dir, ".cursor", "cli-config.json"), "authInfo"); err != nil {
			return nil, err
		}
		return base, nil
	case "claude":
		if err := copyJSONFields(filepath.Join(home, ".claude.json"), filepath.Join(dir, ".claude.json"), "oauthAccount", "hasCompletedOnboarding", "userID"); err != nil {
			return nil, err
		}
		return base, nil
	default:
		return base, nil
	}
}

func freezePiPriceSnapshot(telemetry *Telemetry, path, provider, model string) {
	raw, err := os.ReadFile(path)
	if err != nil || telemetry.TokensIn == nil || telemetry.TokensOut == nil || telemetry.EstimatedUSD == nil {
		return
	}
	var store map[string]struct {
		Models    []json.RawMessage `json:"models"`
		CheckedAt int64             `json:"checkedAt"`
	}
	if json.Unmarshal(raw, &store) != nil {
		return
	}
	for _, rawModel := range store[provider].Models {
		var entry struct {
			ID   string         `json:"id"`
			Cost map[string]any `json:"cost"`
		}
		if json.Unmarshal(rawModel, &entry) != nil || entry.ID != model || len(entry.Cost) == 0 {
			continue
		}
		snapshot, _ := json.Marshal(map[string]any{"provider": provider, "model": model, "checked_at": store[provider].CheckedAt, "cost_per_million_tokens": entry.Cost})
		telemetry.PriceSnapshot = string(snapshot)
		complete := true
		telemetry.Complete = &complete
		return
	}
}

func profileProvider(rec RunRecord) string {
	profile := Profile{}
	if raw, ok := rec.Metadata["profile"]; ok {
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &profile)
	}
	return profile.Provider
}

func providerAPIKeyEnv(provider string) string {
	return map[string]string{
		"anthropic":  "ANTHROPIC_API_KEY",
		"google":     "GEMINI_API_KEY",
		"openai":     "OPENAI_API_KEY",
		"openrouter": "OPENROUTER_API_KEY",
		"xai":        "XAI_API_KEY",
	}[provider]
}

func copyAuthFile(source, dest string) error {
	raw, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return WriteFileAtomic(dest, raw, 0o600)
}

func copyJSONFields(source, dest string, fields ...string) error {
	raw, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Errorf("parse auth metadata %s: %w", source, err)
	}
	output := map[string]any{}
	for _, field := range fields {
		if value, ok := input[field]; ok {
			output[field] = value
		}
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	return WriteFileAtomic(dest, append(encoded, '\n'), 0o600)
}

func DetectHarnessVersion(harness string) string {
	identity, err := DetectHarnessIdentity(harness)
	if err != nil {
		return ""
	}
	return identity.Version
}

func DetectHarnessIdentity(harness string) (HarnessIdentity, error) {
	program := harnessProgram(harness)
	path, err := exec.LookPath(program)
	if err != nil {
		return HarnessIdentity{}, fmt.Errorf("%s harness executable %q was not found on PATH", harness, program)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return HarnessIdentity{}, fmt.Errorf("read %s harness version from %s: %w", harness, path, err)
	}
	line := strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0])
	if line == "" {
		return HarnessIdentity{}, fmt.Errorf("%s harness at %s returned an empty version", harness, path)
	}
	if len(line) > 200 {
		line = line[:200]
	}
	return HarnessIdentity{Path: path, Version: line}, nil
}

func updateSnapshotExecution(l paths.Layout, rec RunRecord, timeout time.Duration) {
	path := filepath.Join(l.RunDir(rec.ID), "snapshot.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var snapshot map[string]any
	if json.Unmarshal(raw, &snapshot) != nil {
		return
	}
	config, _ := snapshot["config"].(map[string]any)
	if config == nil {
		config = map[string]any{}
		snapshot["config"] = config
	}
	if rec.HarnessVersion != "" {
		config["harness_version"] = rec.HarnessVersion
	}
	profile := Profile{}
	if raw, ok := rec.Metadata["profile"]; ok {
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &profile)
	}
	budget := cloneAnyMap(profile.Budget)
	budget["max_minutes_per_run"] = int(timeout.Minutes())
	config["budget"] = budget
	updated, err := json.MarshalIndent(snapshot, "", "  ")
	if err == nil {
		_ = WriteFileAtomic(path, append(updated, '\n'), 0o644)
	}
}

func cloneAnyMap(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source)+1)
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func profileNeedsChildUsage(profile Profile) bool {
	if profile.Subagents != "" {
		return true
	}
	for _, plugin := range profile.Plugins {
		if strings.Contains(strings.ToLower(plugin), "subagent") {
			return true
		}
	}
	return false
}

func profileChildUsageComplete(profile Profile, telemetry Telemetry) bool {
	if !profileNeedsChildUsage(profile) {
		return true
	}
	if telemetry.UsageByAgent == nil {
		return false
	}
	usage := *telemetry.UsageByAgent
	if profile.Subagents == "" {
		return len(usage) >= 2
	}
	count, model, _, ok := parseCodexTopology(profile.Subagents)
	if !ok || len(usage) != count+1 {
		return false
	}
	for _, child := range usage[1:] {
		if child.Model != model {
			return false
		}
	}
	return true
}
