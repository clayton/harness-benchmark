package controlled

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/loop"
)

type RunResult struct {
	Payload map[string]any
	Patch   string
	Report  map[string]any
	LogPath string
}

func ValidationPayload(scenario corpus.Scenario, pack Pack, packDigest string, result ValidationResult) map[string]any {
	return map[string]any{
		"kind": "evaluator_validation", "attestation_id": newID("validation"),
		"scenario":   scenarioClaim(scenario, pack, packDigest),
		"validation": map[string]any{"clean_preparations": result.CleanPreparations, "base_failures": result.BaseFailures, "target_passes": result.TargetPasses},
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
}

func Run(ctx context.Context, scenario corpus.Scenario, pack Pack, packPath, relayImage, artifactDir string) (RunResult, error) {
	if pack.Execution.Command == "" || pack.Execution.Harness == "" || pack.Execution.Model == "" {
		return RunResult{}, fmt.Errorf("controlled execution command, harness, and model are required")
	}
	if pack.Relay.Upstream == "" || pack.Relay.BaseURLEnv == "" || pack.Relay.SecretEnv == "" {
		return RunResult{}, fmt.Errorf("controlled relay configuration is incomplete")
	}
	secret := os.Getenv(pack.Relay.SecretEnv)
	if secret == "" {
		return RunResult{}, fmt.Errorf("missing provider secret environment %s", pack.Relay.SecretEnv)
	}
	packDigest, err := DigestDir(packPath)
	if err != nil {
		return RunResult{}, err
	}
	workspace, cleanupWorkspace, err := checkout(scenario.Repo.URL, scenario.Repo.BaseRef)
	if err != nil {
		return RunResult{}, err
	}
	defer cleanupWorkspace()
	if len(scenario.Acceptance.SetupCommands) > 0 {
		if output, err := dockerRun(ctx, "bridge", workspace, "", pack.EnvironmentImageDigest, scenario.Acceptance.SetupCommands, nil); err != nil {
			return RunResult{}, fmt.Errorf("dependency preparation failed: %w: %s", err, output)
		}
	}

	runID := newID("controlled")
	network := "hbench-" + strings.ReplaceAll(runID, "_", "-")
	relay := network + "-relay"
	if output, err := commandOutput(ctx, "docker", "network", "create", "--internal", network); err != nil {
		return RunResult{}, fmt.Errorf("create isolated network: %w: %s", err, output)
	}
	defer func() { _, _ = commandOutput(context.Background(), "docker", "network", "rm", network) }()

	relayArgs := []string{"run", "-d", "--rm", "--name", relay, "--network", network,
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "-e", "UPSTREAM=" + pack.Relay.Upstream,
		"-e", "AUTH_HEADER=" + pack.Relay.AuthHeader, "-e", "AUTH_SCHEME=" + pack.Relay.AuthScheme,
		"-e", "AUTH_VALUE", relayImage}
	relayCommand := exec.CommandContext(ctx, "docker", relayArgs...)
	relayCommand.Env = append(os.Environ(), "AUTH_VALUE="+secret)
	if output, err := relayCommand.CombinedOutput(); err != nil {
		return RunResult{}, fmt.Errorf("start credential relay: %w: %s", err, output)
	}
	defer func() { _, _ = commandOutput(context.Background(), "docker", "rm", "-f", relay) }()
	if output, err := commandOutput(ctx, "docker", "network", "connect", "bridge", relay); err != nil {
		return RunResult{}, fmt.Errorf("connect credential relay egress: %w: %s", err, output)
	}
	time.Sleep(500 * time.Millisecond)

	environment := map[string]string{}
	for key, value := range pack.Execution.Environment {
		environment[key] = value
	}
	environment[pack.Relay.BaseURLEnv] = "http://" + relay + ":8080"
	if pack.Relay.DummyKeyEnv != "" {
		environment[pack.Relay.DummyKeyEnv] = "relay-injects-credentials"
	}
	started := time.Now()
	log, executionErr := dockerRun(ctx, network, workspace, "", pack.EnvironmentImageDigest, []string{pack.Execution.Command}, environment)
	wallMS := int(time.Since(started).Milliseconds())
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		return RunResult{}, err
	}
	logPath := filepath.Join(artifactDir, runID+".agent.log")
	if err := os.WriteFile(logPath, log, 0o600); err != nil {
		return RunResult{}, err
	}

	patchRaw, _ := exec.Command("git", "-C", workspace, "diff", "--binary", "HEAD").Output()
	judgeOutput, judgeErr := dockerRun(ctx, "none", workspace, packPath, pack.EnvironmentImageDigest, pack.EvaluatorCommands, nil)
	passed := judgeErr == nil && len(strings.TrimSpace(string(patchRaw))) > 0
	status := "completed"
	if !passed {
		status = "failed"
	}
	if ctx.Err() == context.DeadlineExceeded {
		status = "timeout"
	}
	telemetry := loop.ExtractTelemetry(pack.Execution.Harness, logPath)
	telemetry.WallMS = wallMS
	judge := map[string]any{"name": "private_evaluator", "passed": passed, "score": 0.0, "notes": "Private evaluator failed."}
	if passed {
		judge["score"], judge["notes"] = 1.0, "Private evaluator passed."
	}
	report := map[string]any{
		"passed": passed, "checks": []map[string]any{{"name": "private_evaluator", "passed": passed}},
		"execution_exit_ok": executionErr == nil, "evaluator_output_sha256": sha256String(judgeOutput),
	}
	payload := map[string]any{
		"kind": "controlled_run", "attestation_id": runID, "scenario": scenarioClaim(scenario, pack, packDigest),
		"config": map[string]any{"workflow": "baseline", "skills": []string{}, "interaction": "unattended", "budget": pack.Budget, "network": "none", "environment_image_digest": pack.EnvironmentImageDigest},
		"run": map[string]any{
			"id": runID, "status": status, "harness": pack.Execution.Harness, "harness_version": pack.Execution.HarnessVersion,
			"model": pack.Execution.Model, "model_version": pack.Execution.ModelVersion,
			"judges": []map[string]any{judge}, "telemetry": telemetry,
		},
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
	return RunResult{Payload: payload, Patch: string(patchRaw), Report: report, LogPath: logPath}, nil
}

func scenarioClaim(scenario corpus.Scenario, pack Pack, packDigest string) map[string]any {
	return map[string]any{
		"slug": pack.ScenarioSlug, "version": pack.ScenarioVersion, "manifest_digest": scenario.ManifestDigest,
		"evaluator_digest": packDigest, "environment_image_digest": pack.EnvironmentImageDigest, "protocol_id": pack.ProtocolID,
	}
}

func newID(prefix string) string {
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	return prefix + "_" + hex.EncodeToString(raw)
}

func sha256String(raw []byte) string {
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum)
}
