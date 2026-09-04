package controlled

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
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

func Run(ctx context.Context, scenario corpus.Scenario, pack Pack, packPath, relayImage, artifactDir, workspace string) (RunResult, error) {
	if err := pack.ValidateForScenario(scenario); err != nil {
		return RunResult{}, err
	}
	rt, err := SelectRuntime(os.Getenv("HB_RUNTIME"))
	if err != nil {
		return RunResult{}, err
	}
	if !PinnedImage(relayImage) || relayImage != pack.RelayImageDigest || relayImage != scenario.RelayImageDigest {
		return RunResult{}, fmt.Errorf("credential relay image must be pinned and match the evaluator pack and scenario")
	}
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
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		return RunResult{}, err
	}
	tempRoot := filepath.Join(artifactDir, ".tmp")
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return RunResult{}, err
	}
	defer os.RemoveAll(tempRoot)
	if workspace == "" {
		var cleanupWorkspace func()
		workspace, cleanupWorkspace, err = checkoutScenarioBaseAt(ctx, scenario, tempRoot)
		if err != nil {
			return RunResult{}, err
		}
		defer cleanupWorkspace()
		if err := os.WriteFile(filepath.Join(workspace, "HB_PROMPT.txt"), []byte(strings.TrimSpace(scenario.Prompt)+"\n"), 0o644); err != nil {
			return RunResult{}, err
		}
		if err := os.WriteFile(filepath.Join(workspace, ".git", "info", "exclude"), []byte("HB_PROMPT.txt\n"), 0o644); err != nil {
			return RunResult{}, err
		}
	}
	if len(scenario.Acceptance.SetupCommands) > 0 {
		if output, err := dockerRun(ctx, rt, "none", workspace, "", pack.EnvironmentImageDigest, scenario.Acceptance.SetupCommands, nil); err != nil {
			return RunResult{}, fmt.Errorf("dependency preparation failed: %w: %s", err, output)
		}
	}

	runID := newID("controlled")
	network := "hbench-" + strings.ReplaceAll(runID, "_", "-")
	egressNetwork := network + "-egress"
	relay := network + "-relay"
	if output, err := rt.Run(ctx, "network", "create", "--internal", network); err != nil {
		return RunResult{}, fmt.Errorf("create isolated network: %w: %s", err, output)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = rt.Run(cleanupCtx, "network", "rm", network)
	}()
	if output, err := rt.Run(ctx, "network", "create", egressNetwork); err != nil {
		return RunResult{}, fmt.Errorf("create relay egress network: %w: %s", err, output)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = rt.Run(cleanupCtx, "network", "rm", egressNetwork)
	}()

	secretDir, err := os.MkdirTemp(tempRoot, "hbench-relay-secrets-")
	if err != nil {
		return RunResult{}, err
	}
	defer os.RemoveAll(secretDir)
	accountingDir, err := os.MkdirTemp(tempRoot, "hbench-relay-accounting-")
	if err != nil {
		return RunResult{}, err
	}
	defer os.RemoveAll(accountingDir)
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return RunResult{}, err
	}
	clientToken := hex.EncodeToString(tokenBytes)
	if err := os.WriteFile(filepath.Join(secretDir, "provider"), []byte(secret), 0o600); err != nil {
		return RunResult{}, err
	}
	if err := os.WriteFile(filepath.Join(secretDir, "client-token"), []byte(clientToken), 0o600); err != nil {
		return RunResult{}, err
	}

	runUser, err := hostUser()
	if err != nil {
		return RunResult{}, err
	}
	relayArgs := []string{"run", "-d", "--rm", "--name", relay, "--network", egressNetwork,
		"--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "64", "--memory", "256m", "--cpus", "1", "--tmpfs", "/tmp:rw,noexec,nosuid,size=32m",
		"--user", runUser,
		"-v", secretDir + ":/run/secrets:ro", "-v", accountingDir + ":/run/accounting:rw", "-e", "UPSTREAM=" + pack.Relay.Upstream,
		"-e", "AUTH_HEADER=" + pack.Relay.AuthHeader, "-e", "AUTH_SCHEME=" + pack.Relay.AuthScheme,
		"-e", "AUTH_FILE=/run/secrets/provider", "-e", "CLIENT_TOKEN_FILE=/run/secrets/client-token", "-e", "ACCOUNTING_FILE=/run/accounting/usage.json"}
	if maxUSD, ok := budgetNumber(pack.Budget, "max_usd"); ok {
		if pack.Relay.MaxRequestUSD <= 0 || pack.Relay.MaxRequestUSD > maxUSD || pack.Relay.AllowedModel == "" || pack.Relay.MaxRequestBytes <= 0 || pack.Relay.MaxOutputTokens <= 0 || pack.Relay.MaxPromptUSDPerMillion <= 0 || pack.Relay.MaxCompletionUSDPerMillion <= 0 || pack.Relay.MaxRequestUSD+1e-12 < minimumRequestUSD(pack.Relay) {
			return RunResult{}, fmt.Errorf("capped relay requires model, request, token, price, and reservation bounds")
		}
		relayArgs = append(relayArgs,
			"-e", fmt.Sprintf("MAX_USD=%.9f", maxUSD), "-e", fmt.Sprintf("MAX_REQUEST_USD=%.9f", pack.Relay.MaxRequestUSD),
			"-e", "ALLOWED_MODEL="+pack.Relay.AllowedModel, "-e", fmt.Sprintf("MAX_REQUEST_BYTES=%d", pack.Relay.MaxRequestBytes),
			"-e", fmt.Sprintf("MAX_OUTPUT_TOKENS=%d", pack.Relay.MaxOutputTokens),
			"-e", fmt.Sprintf("MAX_PROMPT_USD_PER_MILLION=%.9f", pack.Relay.MaxPromptUSDPerMillion),
			"-e", fmt.Sprintf("MAX_COMPLETION_USD_PER_MILLION=%.9f", pack.Relay.MaxCompletionUSDPerMillion))
	}
	relayArgs = append(relayArgs, relayImage)
	relayCommand := exec.CommandContext(ctx, rt.Command, relayArgs...)
	if output, err := relayCommand.CombinedOutput(); err != nil {
		return RunResult{}, fmt.Errorf("start credential relay: %w: %s", err, output)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = rt.Run(cleanupCtx, "rm", "-f", relay)
	}()
	if output, err := rt.Run(ctx, "network", "connect", network, relay); err != nil {
		return RunResult{}, fmt.Errorf("connect credential relay to agent network: %w: %s", err, output)
	}
	if err := waitForHealthy(ctx, rt, relay); err != nil {
		return RunResult{}, err
	}

	environment := map[string]string{}
	for key, value := range pack.Execution.Environment {
		environment[key] = value
	}
	environment[pack.Relay.BaseURLEnv] = "http://" + relay + ":8080"
	environment[pack.Relay.DummyKeyEnv] = clientToken
	started := time.Now()
	log, executionErr := dockerRun(ctx, rt, network, workspace, "", pack.EnvironmentImageDigest, []string{pack.Execution.Command}, environment)
	wallMS := int(time.Since(started).Milliseconds())
	logPath := filepath.Join(artifactDir, runID+".agent.log")
	if err := os.WriteFile(logPath, log, 0o600); err != nil {
		return RunResult{}, err
	}

	patchRaw, patchErr := dockerRun(ctx, rt, "none", workspace, "", pack.EnvironmentImageDigest, []string{
		"git -c safe.directory=/workspace -c core.hooksPath=/dev/null add -N -A",
		"git -c safe.directory=/workspace -c core.hooksPath=/dev/null diff --binary HEAD",
	}, nil)
	judgeOutput, judgeErr := dockerRun(ctx, rt, "none", workspace, packPath, pack.EnvironmentImageDigest, pack.EvaluatorCommands, nil)
	telemetry := loop.ExtractTelemetry(pack.Execution.Harness, logPath)
	telemetry.WallMS = wallMS
	accounting := relayAccounting{}
	if raw, readErr := os.ReadFile(filepath.Join(accountingDir, "usage.json")); readErr == nil && json.Unmarshal(raw, &accounting) == nil {
		telemetry.EstimatedUSD = &accounting.EstimatedUSD
		telemetry.Complete = &accounting.Complete
		if telemetry.UsageByAgent != nil && len(*telemetry.UsageByAgent) == 1 {
			(*telemetry.UsageByAgent)[0].EstimatedUSD = &accounting.EstimatedUSD
		}
		if accounting.Complete {
			telemetry.CostKind = "actual"
		} else {
			telemetry.CostKind = "estimated"
			telemetry.PriceSnapshot = fmt.Sprintf("relay reservation upper bound %.9f USD/request", pack.Relay.MaxRequestUSD)
		}
	} else if telemetry.EstimatedUSD != nil && *telemetry.EstimatedUSD == 0 && (telemetry.Complete == nil || !*telemetry.Complete) {
		telemetry.EstimatedUSD = nil
	}
	passed := executionErr == nil && patchErr == nil && judgeErr == nil && !accounting.Exceeded && len(strings.TrimSpace(string(patchRaw))) > 0
	status := "completed"
	if !passed {
		status = "failed"
	}
	if ctx.Err() == context.DeadlineExceeded {
		status = "timeout"
	} else if accounting.Exceeded {
		status = "budget_exceeded"
	}
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
		"config": map[string]any{"workflow": "baseline", "skills": []string{}, "interaction": "unattended", "budget": pack.Budget, "network": "none", "environment_image_digest": pack.EnvironmentImageDigest, "relay_image_digest": pack.RelayImageDigest, "runtime": rt},
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
		"evaluator_digest": packDigest, "environment_image_digest": pack.EnvironmentImageDigest, "relay_image_digest": pack.RelayImageDigest, "protocol_id": pack.ProtocolID,
	}
}

func newID(prefix string) string {
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	return prefix + "_" + hex.EncodeToString(raw)
}

type relayAccounting struct {
	EstimatedUSD float64 `json:"estimated_usd"`
	Complete     bool    `json:"complete"`
	Exceeded     bool    `json:"exceeded"`
}

func budgetNumber(budget map[string]any, key string) (float64, bool) {
	switch value := budget[key].(type) {
	case float64:
		return value, value > 0
	case int:
		return float64(value), value > 0
	default:
		return 0, false
	}
}

func hostUser() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("resolve relay host user: %w", err)
	}
	if current.Uid == "" || current.Gid == "" {
		return "", fmt.Errorf("resolve relay host user: missing UID or GID")
	}
	return current.Uid + ":" + current.Gid, nil
}

func sha256String(raw []byte) string {
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum)
}

func waitForHealthy(ctx context.Context, rt Runtime, container string) error {
	healthCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		output, err := rt.Run(healthCtx, "inspect", "--format", "{{.State.Health.Status}}", container)
		if err == nil && strings.TrimSpace(string(output)) == "healthy" {
			return nil
		}
		select {
		case <-healthCtx.Done():
			return fmt.Errorf("credential relay did not become healthy: %w", healthCtx.Err())
		case <-ticker.C:
		}
	}
}
