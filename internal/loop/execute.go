package loop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/clayton/harness-benchmark/internal/paths"
)

type ExecResult struct {
	ReturnCode int
	WallMS     int
	LogPath    string
}

func Execute(l paths.Layout, id string, timeout time.Duration) (ExecResult, error) {
	rec, err := Load(l, id)
	if err != nil {
		return ExecResult{}, err
	}
	cmdLine := HeadlessLaunch(rec.Harness, rec.Model)
	if cmdLine == "" {
		return ExecResult{}, fmt.Errorf("%s has no headless launch; stay in this directory and run: hbench finish %s", rec.Harness, id)
	}
	if _, err := os.Stat(rec.Worktree); err != nil {
		return ExecResult{}, fmt.Errorf("workspace missing: %s", rec.Worktree)
	}
	rec.Status = "running"
	_ = Save(l, rec)

	logPath := filepath.Join(l.RunDir(id), "agent.log")
	logF, err := os.Create(logPath)
	if err != nil {
		return ExecResult{}, err
	}

	cmd := exec.Command("sh", "-c", cmdLine)
	cmd.Dir = rec.Worktree
	cmd.Stdout = logF
	cmd.Stderr = logF
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
	select {
	case waitErr = <-done:
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		waitErr = fmt.Errorf("timeout after %s", timeout)
	}
	wall := int(time.Since(start).Milliseconds())
	closeErr := logF.Close()
	rec.Telemetry = ExtractTelemetry(rec.Harness, logPath)
	rec.Telemetry.WallMS = wall
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
	return ExecResult{ReturnCode: rc, WallMS: wall, LogPath: logPath}, waitErr
}
