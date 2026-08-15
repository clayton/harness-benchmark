package controlled

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/clayton/harness-benchmark/internal/corpus"
)

type ValidationResult struct {
	CleanPreparations int      `json:"clean_preparations"`
	BaseFailures      int      `json:"base_failures"`
	TargetPasses      int      `json:"target_passes"`
	Notes             []string `json:"notes"`
}

func Validate(ctx context.Context, scenario corpus.Scenario, pack Pack, packPath string) (ValidationResult, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return ValidationResult{}, fmt.Errorf("docker is required")
	}
	result := ValidationResult{}
	for attempt := 1; attempt <= 2; attempt++ {
		baseDir, cleanupBase, err := checkoutScenarioBase(scenario)
		if err != nil {
			return result, err
		}
		basePassed, baseLog, err := prepareAndEvaluate(ctx, baseDir, scenario, pack, packPath)
		cleanupBase()
		if err != nil {
			return result, fmt.Errorf("base attempt %d: %w", attempt, err)
		}

		targetDir, cleanupTarget, err := checkout(scenario.Repo.URL, pack.TargetRef)
		if err != nil {
			return result, err
		}
		targetPassed, targetLog, err := prepareAndEvaluate(ctx, targetDir, scenario, pack, packPath)
		cleanupTarget()
		if err != nil {
			return result, fmt.Errorf("target attempt %d: %w", attempt, err)
		}
		result.CleanPreparations++
		if !basePassed {
			result.BaseFailures++
		}
		if targetPassed {
			result.TargetPasses++
		}
		result.Notes = append(result.Notes, fmt.Sprintf("attempt %d base_pass=%t target_pass=%t base=%s target=%s", attempt, basePassed, targetPassed, compact(baseLog), compact(targetLog)))
	}
	if result.BaseFailures < 2 || result.TargetPasses < 2 {
		return result, fmt.Errorf("evaluator did not reproduce twice: base_failures=%d target_passes=%d", result.BaseFailures, result.TargetPasses)
	}
	return result, nil
}

func checkoutScenarioBase(scenario corpus.Scenario) (string, func(), error) {
	if scenario.Workspace.Kind != "scaffold" {
		return checkout(scenario.Repo.URL, scenario.Repo.BaseRef)
	}
	dir, err := os.MkdirTemp("", "hbench-controlled-scaffold-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	paths, err := corpus.ScaffoldPaths(scenario.Workspace.Files)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	for _, clean := range paths {
		contents := scenario.Workspace.Files[clean]
		path := filepath.Join(dir, clean)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			cleanup()
			return "", func() {}, err
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "hbench@local"}, {"config", "user.name", "hbench"}, {"config", "commit.gpgsign", "false"}, {"add", "-A"}, {"commit", "-qm", "hbench scaffold base"}} {
		command := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("git scaffold: %w: %s", err, output)
		}
	}
	return dir, cleanup, nil
}

func checkout(repoURL, ref string) (string, func(), error) {
	if err := validateCheckout(repoURL, ref); err != nil {
		return "", func() {}, err
	}
	dir, err := os.MkdirTemp("", "hbench-controlled-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	commands := [][]string{{"init", "-q"}, {"remote", "add", "origin", repoURL}, {"fetch", "--depth=1", "origin", ref}, {"checkout", "-q", "--detach", "FETCH_HEAD"}, {"remote", "remove", "origin"}}
	for _, args := range commands {
		filePolicy := "never"
		if os.Getenv("HB_CONTROLLED_ALLOW_LOCAL_REPO") == "1" {
			filePolicy = "always"
		}
		gitArgs := []string{"-c", "credential.helper=", "-c", "protocol.file.allow=" + filePolicy, "-C", dir}
		command := exec.Command("git", append(gitArgs, args...)...)
		command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never", "GIT_ASKPASS=true")
		if output, err := command.CombinedOutput(); err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, output)
		}
	}
	return dir, cleanup, nil
}

func validateCheckout(repoURL, ref string) error {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(ref) {
		return fmt.Errorf("controlled checkout ref must be a full lowercase commit hash")
	}
	if strings.HasPrefix(repoURL, "https://github.com/") {
		return nil
	}
	if os.Getenv("HB_CONTROLLED_ALLOW_LOCAL_REPO") == "1" && filepath.IsAbs(repoURL) {
		return nil
	}
	return fmt.Errorf("controlled repository must use https://github.com/")
}

func prepareAndEvaluate(ctx context.Context, workspace string, scenario corpus.Scenario, pack Pack, packPath string) (bool, string, error) {
	if len(scenario.Acceptance.SetupCommands) > 0 {
		if output, err := dockerRun(ctx, "none", workspace, "", pack.EnvironmentImageDigest, scenario.Acceptance.SetupCommands, nil); err != nil {
			return false, string(output), fmt.Errorf("dependency preparation failed: %w: %s", err, output)
		}
	}
	output, err := dockerRun(ctx, "none", workspace, packPath, pack.EnvironmentImageDigest, pack.EvaluatorCommands, nil)
	return err == nil, string(output), nil
}

func dockerRun(ctx context.Context, network, workspace, evaluator, image string, commands []string, environment map[string]string) ([]byte, error) {
	name := "hbench-job-" + newID("")
	args := []string{"run", "--rm", "--name", name, "--network", network, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "512", "--memory", "4g", "--cpus", "4", "--tmpfs", "/tmp:rw,noexec,nosuid,size=1g", "-v", workspace + ":/workspace", "-w", "/workspace"}
	if evaluator != "" {
		absolute, _ := filepath.Abs(evaluator)
		args = append(args, "-v", absolute+":/evaluator:ro")
	}
	for key, value := range environment {
		args = append(args, "-e", key+"="+value)
	}
	args = append(args, image, "sh", "-lc", "set -eu\n"+strings.Join(commands, "\n"))
	command := exec.CommandContext(ctx, "docker", args...)
	defer func() { _, _ = commandOutput(context.Background(), "docker", "rm", "-f", name) }()
	return command.CombinedOutput()
}

func compact(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")[:min(len(strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")), 300)]
}

func RunTimeout(minutes int) (context.Context, context.CancelFunc) {
	if minutes <= 0 {
		minutes = 45
	}
	return context.WithTimeout(context.Background(), time.Duration(minutes)*time.Minute)
}

func commandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	var output bytes.Buffer
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout, command.Stderr = &output, &output
	err := command.Run()
	return output.Bytes(), err
}
