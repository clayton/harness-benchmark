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

func Execute(l paths.Layout, id string, timeout time.Duration) (ExecResult, error) {
	rec, err := Load(l, id)
	if err != nil {
		return ExecResult{}, err
	}
	promptRaw, err := os.ReadFile(filepath.Join(rec.Worktree, "HB_PROMPT.txt"))
	if err != nil {
		return ExecResult{}, fmt.Errorf("read run prompt: %w", err)
	}
	launch := HeadlessLaunch(rec.Harness, rec.Model, strings.TrimSpace(string(promptRaw)))
	if launch.Program == "" {
		if rec.Model != "" && !modelIDPattern.MatchString(rec.Model) {
			return ExecResult{}, fmt.Errorf("invalid model id %q", rec.Model)
		}
		return ExecResult{}, fmt.Errorf("%s has no headless launch; stay in this directory and run: hbench finish %s", rec.Harness, id)
	}
	if _, err := os.Stat(rec.Worktree); err != nil {
		return ExecResult{}, fmt.Errorf("workspace missing: %s", rec.Worktree)
	}
	if rec.HarnessVersion == "" {
		rec.HarnessVersion = detectHarnessVersion(rec.Harness)
	}
	if rec.Metadata == nil {
		rec.Metadata = map[string]any{}
	}
	rec.Metadata["budget"] = map[string]any{"max_minutes": int(timeout.Minutes())}
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

func detectHarnessVersion(harness string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, harness, "--version").Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0])
	if len(line) > 200 {
		line = line[:200]
	}
	return line
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
	config["budget"] = map[string]any{"max_minutes": int(timeout.Minutes())}
	updated, err := json.MarshalIndent(snapshot, "", "  ")
	if err == nil {
		_ = WriteFileAtomic(path, append(updated, '\n'), 0o644)
	}
}
