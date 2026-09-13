package controlled

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/loop"
)

func TestSelectRuntimeAutoAndExplicit(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "podman")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho podman version test\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	rt, err := SelectRuntime("auto")
	if err != nil || rt.Name != "podman" || rt.Version != "podman version test" || rt.Arch == "" {
		t.Fatalf("runtime=%#v err=%v", rt, err)
	}
	if _, err := SelectRuntime("docker"); err == nil {
		t.Fatal("expected missing explicit runtime error")
	}
	if _, err := SelectRuntime("containerd"); err == nil {
		t.Fatal("expected unsupported runtime error")
	}
}

func TestMinimumRequestUSDIncludesInputAndOutputBounds(t *testing.T) {
	relay := Relay{MaxRequestBytes: 100000, MaxOutputTokens: 32768, MaxPromptUSDPerMillion: 0.075, MaxCompletionUSDPerMillion: 0.25}
	if got := minimumRequestUSD(relay); got < 0.0156919 || got > 0.0156921 {
		t.Fatalf("minimumRequestUSD=%f", got)
	}
}

func TestConfiguredPricingCompletesEstimatedCost(t *testing.T) {
	input, output, cacheRead, cacheWrite := 1_000, 100, 500, 50
	complete := false
	usage := []loop.AgentUsage{{AgentID: "parent"}}
	telemetry := loop.Telemetry{TokensIn: &input, TokensOut: &output, CacheReadTokens: &cacheRead, CacheWriteTokens: &cacheWrite, Complete: &complete, UsageByAgent: &usage}
	applyConfiguredPricing(&telemetry, Pricing{PromptUSDPerMillion: 0.5, CompletionUSDPerMillion: 2.5, CacheReadUSDPerMillion: 0.2, CacheWriteUSDPerMillion: 0.5, Snapshot: "cursor-2026-09-12"})
	if telemetry.EstimatedUSD == nil || *telemetry.EstimatedUSD < 0.0008749 || *telemetry.EstimatedUSD > 0.0008751 {
		t.Fatalf("cost=%v", telemetry.EstimatedUSD)
	}
	if telemetry.Complete == nil || !*telemetry.Complete || telemetry.CostKind != "estimated" || telemetry.PriceSnapshot != "cursor-2026-09-12" || usage[0].EstimatedUSD == nil {
		t.Fatalf("telemetry=%+v usage=%+v", telemetry, usage)
	}
}

func TestPrivateTargetWorkspaceMakesBaseFailAndTargetPass(t *testing.T) {
	scenario := corpus.Scenario{Workspace: corpus.Workspace{Kind: "scaffold", Files: map[string]string{"README.md": "start\n"}}}
	base, cleanupBase, err := checkoutScenarioBaseAt(context.Background(), scenario, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupBase()
	target, cleanupTarget, err := checkoutWorkspace(context.Background(), map[string]string{"README.md": "start\n", "answer.txt": "42\n"}, "hbench-test-target-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTarget()
	check := func(dir string) error {
		command := exec.Command("sh", "-c", "test -f answer.txt && test \"$(tr -d '\\r\\n' < answer.txt)\" = 42")
		command.Dir = dir
		return command.Run()
	}
	if err := check(base); err == nil {
		t.Fatal("private evaluator unexpectedly passed on scaffold base")
	}
	if err := check(target); err != nil {
		t.Fatalf("private target did not pass evaluator: %v", err)
	}
}

func TestRunRejectsUnpinnedRelayBeforeCredentials(t *testing.T) {
	runtimeDir := t.TempDir()
	runtimePath := filepath.Join(runtimeDir, "podman")
	if err := os.WriteFile(runtimePath, []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", runtimeDir)
	_, err := Run(context.Background(), corpus.Scenario{}, Pack{TargetRef: strings.Repeat("b", 40)}, "", "relay:latest", t.TempDir(), "")
	if err == nil || !strings.Contains(err.Error(), "relay image must be pinned") {
		t.Fatalf("err=%v", err)
	}

	relay := "example/relay@sha256:" + strings.Repeat("e", 64)
	other := "example/relay@sha256:" + strings.Repeat("f", 64)
	_, err = Run(context.Background(), corpus.Scenario{RelayImageDigest: other}, Pack{TargetRef: strings.Repeat("b", 40), RelayImageDigest: relay}, "", relay, t.TempDir(), "")
	if err == nil || !strings.Contains(err.Error(), "match the evaluator pack and scenario") {
		t.Fatalf("err=%v", err)
	}
}

func TestPackDigestAndEd25519Envelope(t *testing.T) {
	dir := t.TempDir()
	packYAML := `schema: rodeo.evaluator.v1
scenario_slug: safe-task
scenario_version: 1
target_ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
environment_image_digest: example/image@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
relay_image_digest: example/relay@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
protocol_id: controlled-v3
evaluator_commands: ["test -f /evaluator/hidden.txt"]
budget:
  max_usd: 2.0
execution:
  harness: pi
  harness_version: 1.0.0
  provider: openrouter
  model: openrouter/example/model
  reasoning: high
  command: hbench-pi-openrouter
  environment: {}
relay:
  upstream: https://openrouter.ai
  base_url_env: HB_MODEL_BASE_URL
  secret_env: OPENROUTER_API_KEY
  auth_header: Authorization
  auth_scheme: Bearer
  dummy_key_env: HB_MODEL_API_KEY
  allowed_model: openrouter/example/model
  max_request_usd: 2.0
  max_request_bytes: 1048576
  max_output_tokens: 4096
  max_prompt_usd_per_million: 1
  max_completion_usd_per_million: 1
`
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(packYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hidden.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	pack, digest, err := LoadPack(dir)
	if err != nil || len(digest) != 64 || pack.TargetRef == "" {
		t.Fatalf("pack=%#v digest=%q err=%v", pack, digest, err)
	}
	keyPath := filepath.Join(t.TempDir(), "runner.pem")
	_, _, err = Keygen(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Sign(keyPath, "test-key", map[string]any{"kind": "test"}, "patch", map[string]any{"passed": true})
	if err != nil {
		t.Fatal(err)
	}
	privateRaw, _ := os.ReadFile(keyPath)
	block, _ := pem.Decode(privateRaw)
	parsed, _ := x509.ParsePKCS8PrivateKey(block.Bytes)
	public := parsed.(ed25519.PrivateKey).Public().(ed25519.PublicKey)
	payload, _ := base64.StdEncoding.DecodeString(envelope.Payload)
	signature, _ := base64.StdEncoding.DecodeString(envelope.Signature)
	if !ed25519.Verify(public, payload, signature) {
		t.Fatal("signature did not verify")
	}
}

func TestNamedSetupSelectionKeepsEvaluatorDigest(t *testing.T) {
	dir := t.TempDir()
	packYAML := `schema: rodeo.evaluator.v1
scenario_slug: safe-task
scenario_version: 1
target_ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
environment_image_digest: example/image@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
relay_image_digest: example/relay@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
protocol_id: controlled-v3
evaluator_commands: ["true"]
budget:
  max_usd: 2.0
setups:
  muse:
    execution:
      harness: pi
      harness_version: 1.0.0
      provider: openrouter
      model: openrouter/meta/muse
      reasoning: high
      command: hbench-pi-openrouter
      environment: {}
    relay:
      upstream: https://openrouter.ai
      base_url_env: HB_MODEL_BASE_URL
      secret_env: OPENROUTER_API_KEY
      auth_header: Authorization
      auth_scheme: Bearer
      dummy_key_env: HB_MODEL_API_KEY
      allowed_model: openrouter/meta/muse
      max_request_usd: 2.0
      max_request_bytes: 1048576
      max_output_tokens: 4096
      max_prompt_usd_per_million: 1
      max_completion_usd_per_million: 1
  luna:
    execution:
      harness: pi
      harness_version: 1.0.0
      provider: openrouter
      model: openrouter/openai/luna
      reasoning: high
      command: hbench-pi-openrouter
      environment: {}
    relay:
      upstream: https://openrouter.ai
      base_url_env: HB_MODEL_BASE_URL
      secret_env: OPENROUTER_API_KEY
      auth_header: Authorization
      auth_scheme: Bearer
      dummy_key_env: HB_MODEL_API_KEY
      allowed_model: openrouter/openai/luna
      max_request_usd: 2.0
      max_request_bytes: 1048576
      max_output_tokens: 4096
      max_prompt_usd_per_million: 1
      max_completion_usd_per_million: 1
`
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(packYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	pack, digest, err := LoadPack(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pack.SelectSetup(""); err == nil {
		t.Fatal("accepted a named-setup pack without --setup")
	}
	muse, err := pack.SelectSetup("muse")
	if err != nil || muse.Execution.Model != "openrouter/meta/muse" {
		t.Fatalf("muse=%#v err=%v", muse.Execution, err)
	}
	luna, err := pack.SelectSetup("luna")
	if err != nil || luna.Execution.Model != "openrouter/openai/luna" {
		t.Fatalf("luna=%#v err=%v", luna.Execution, err)
	}
	_, selectedDigest, err := LoadPack(dir)
	if err != nil || selectedDigest != digest {
		t.Fatalf("digest changed after setup selection: before=%s after=%s err=%v", digest, selectedDigest, err)
	}
}

func TestPackRequiresExactlyOnePrivateTarget(t *testing.T) {
	dir := t.TempDir()
	packYAML := `schema: rodeo.evaluator.v1
scenario_slug: safe-task
scenario_version: 1
target_ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
target_workspace:
  answer.txt: "42"
environment_image_digest: example/image@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
relay_image_digest: example/relay@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
protocol_id: controlled-v3
evaluator_commands: ["true"]
`
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(packYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadPack(dir); err == nil {
		t.Fatal("accepted both target_ref and target_workspace")
	}
	pack := Pack{TargetWorkspace: map[string]string{"answer.txt": "42"}}
	if err := pack.ValidateForScenario(corpus.Scenario{Repo: corpus.Repo{URL: "https://github.com/example/task"}}); err == nil {
		t.Fatal("accepted private target workspace for repository scenario")
	}
	if err := pack.ValidateForScenario(corpus.Scenario{Workspace: corpus.Workspace{Kind: "scaffold"}}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSetupRejectsControlledCursorStreaming(t *testing.T) {
	setup := Setup{Execution: Execution{
		Harness: "pi", HarnessVersion: "0.85.1", Provider: "meta", Model: "meta/muse-spark-1.3-contributor", Reasoning: "high",
		Command: "hbench-pi-cursor", Environment: map[string]string{"CURSOR_BACKEND_URL": "${RELAY_URL}"},
	}}
	if err := validateSetup("cursor", setup, map[string]any{"max_usd": 25.0}); err == nil || !strings.Contains(err.Error(), "unsupported Cursor streaming") {
		t.Fatalf("cursor setup error=%v", err)
	}
}

func TestLoadPackRejectsUnpinnedImagesAndExecutionCredentials(t *testing.T) {
	cases := map[string]struct {
		image       string
		environment string
	}{
		"floating image":   {image: "example/image:latest"},
		"agent credential": {image: "example/image@sha256:" + strings.Repeat("c", 64), environment: "    PRIVATE_API_KEY: do-not-pass-this\n"},
	}
	for name, unsafe := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			packYAML := `schema: rodeo.evaluator.v1
scenario_slug: safe-task
scenario_version: 1
target_ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
environment_image_digest: ` + unsafe.image + `
relay_image_digest: example/relay@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
protocol_id: controlled-v3
evaluator_commands: ["true"]
execution:
  harness: pi
  harness_version: 1.0.0
  provider: openrouter
  model: test
  reasoning: high
  command: pi
  environment:
` + unsafe.environment + `relay:
  upstream: https://api.openai.com/v1
  base_url_env: OPENAI_BASE_URL
  secret_env: OPENAI_API_KEY
  auth_header: Authorization
  auth_scheme: Bearer
  dummy_key_env: OPENAI_API_KEY
`
			if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(packYAML), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := LoadPack(dir); err == nil {
				t.Fatal("expected unsafe evaluator pack to be rejected")
			}
		})
	}
}

func TestDockerControlledRunEndToEnd(t *testing.T) {
	if os.Getenv("HB_DOCKER_INTEGRATION") != "1" {
		t.Skip("set HB_DOCKER_INTEGRATION=1")
	}
	repo := t.TempDir()
	command := exec.Command("git", "-C", repo, "init", "-q")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(repo, "result.txt"), []byte("broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "result.txt"}, {"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "base"}} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}
	baseRaw, _ := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	base := strings.TrimSpace(string(baseRaw))
	packDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(packDir, "pack.yaml"), []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	image := os.Getenv("HB_SYNTHETIC_IMAGE")
	if image == "" {
		image = "alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce"
	}
	pack := Pack{
		ScenarioSlug: "synthetic", ScenarioVersion: 1, EnvironmentImageDigest: image, RelayImageDigest: "hbench-model-relay@sha256:" + strings.Repeat("e", 64), ProtocolID: "controlled-v3",
		EvaluatorCommands: []string{`test "$(cat result.txt)" = fixed`}, Budget: map[string]any{"max_minutes": 2},
		Execution: Execution{Harness: "manual", HarnessVersion: "test", Provider: "synthetic", Model: "synthetic", ModelVersion: "1", Reasoning: "none", Command: "test -s HB_PROMPT.txt && printf 'fixed\\n' > result.txt"},
		Relay:     Relay{Upstream: "https://api.openai.com/v1", BaseURLEnv: "OPENAI_BASE_URL", SecretEnv: "OPENAI_API_KEY", AuthHeader: "Authorization", AuthScheme: "Bearer", DummyKeyEnv: "OPENAI_API_KEY"},
	}
	t.Setenv("OPENAI_API_KEY", "synthetic-not-a-real-key")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := Run(ctx, corpus.Scenario{ID: "synthetic@1", Prompt: "fix the result", ManifestDigest: strings.Repeat("d", 64), RelayImageDigest: pack.RelayImageDigest, Repo: corpus.Repo{URL: repo, BaseRef: base}}, pack, packDir, pack.RelayImageDigest, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Report["passed"] != true || !strings.Contains(result.Patch, "+fixed") {
		t.Fatalf("unexpected result report=%#v patch=%s", result.Report, result.Patch)
	}
}

func TestValidateRecreatesBaseAndTargetTwice(t *testing.T) {
	t.Setenv("HB_CONTROLLED_ALLOW_LOCAL_REPO", "1")
	repo := t.TempDir()
	runGit := func(args ...string) string {
		command := exec.Command("git", append([]string{"-C", repo}, args...)...)
		command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	runGit("init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "result.txt"), []byte("broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "result.txt")
	runGit("-c", "commit.gpgsign=false", "commit", "-qm", "base")
	base := runGit("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "result.txt"), []byte("fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "result.txt")
	runGit("-c", "commit.gpgsign=false", "commit", "-qm", "target")
	target := runGit("rev-parse", "HEAD")

	fakeBin := t.TempDir()
	fakeDocker := `#!/bin/sh
workspace=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-v" ]; then
    case "$arg" in *:/workspace) workspace=${arg%%:/workspace} ;; esac
  fi
  prev=$arg
done
test "$(cat "$workspace/result.txt")" = fixed
`
	if err := os.WriteFile(filepath.Join(fakeBin, "docker"), []byte(fakeDocker), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+":"+os.Getenv("PATH"))
	packDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(packDir, "pack.yaml"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	pack := Pack{TargetRef: target, EnvironmentImageDigest: "example/image@sha256:" + strings.Repeat("c", 64), EvaluatorCommands: []string{"test"}}
	scenario := corpus.Scenario{Repo: corpus.Repo{URL: repo, BaseRef: base}}
	result, err := Validate(context.Background(), scenario, pack, packDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.CleanPreparations != 2 || result.BaseFailures != 2 || result.TargetPasses != 2 {
		raw, _ := json.Marshal(result)
		t.Fatalf("unexpected validation %s", raw)
	}
}

func TestScaffoldBaseIsACommittedRepository(t *testing.T) {
	dir, cleanup, err := checkoutScenarioBase(context.Background(), corpus.Scenario{Workspace: corpus.Workspace{Kind: "scaffold", Files: map[string]string{"PLAN.md": "build it\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := os.WriteFile(filepath.Join(dir, "new.rs"), []byte("fn main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", dir, "add", "-N", "-A").Run(); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("git", "-C", dir, "diff", "HEAD").Output()
	if err != nil || !strings.Contains(string(raw), "new.rs") {
		t.Fatalf("diff=%s err=%v", raw, err)
	}
}
