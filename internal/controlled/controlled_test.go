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
)

func TestPackDigestAndEd25519Envelope(t *testing.T) {
	dir := t.TempDir()
	packYAML := `schema: rodeo.evaluator.v1
scenario_slug: safe-task
scenario_version: 1
target_ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
environment_image_digest: example/image@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
protocol_id: controlled-v2
evaluator_commands: ["test -f /evaluator/hidden.txt"]
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
protocol_id: controlled-v2
evaluator_commands: ["true"]
execution:
  harness: pi
  model: test
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
		ScenarioSlug: "synthetic", ScenarioVersion: 1, EnvironmentImageDigest: image, ProtocolID: "controlled-v2",
		EvaluatorCommands: []string{`test "$(cat result.txt)" = fixed`}, Budget: map[string]any{"max_minutes": 2},
		Execution: Execution{Harness: "manual", HarnessVersion: "test", Model: "synthetic", ModelVersion: "1", Command: "printf 'fixed\\n' > result.txt"},
		Relay:     Relay{Upstream: "https://api.openai.com/v1", BaseURLEnv: "OPENAI_BASE_URL", SecretEnv: "OPENAI_API_KEY", AuthHeader: "Authorization", AuthScheme: "Bearer", DummyKeyEnv: "OPENAI_API_KEY"},
	}
	t.Setenv("OPENAI_API_KEY", "synthetic-not-a-real-key")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := Run(ctx, corpus.Scenario{ID: "synthetic@1", ManifestDigest: strings.Repeat("d", 64), Repo: corpus.Repo{URL: repo, BaseRef: base}}, pack, packDir, "hbench-model-relay:v0.4.0", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result.Report["passed"] != true || !strings.Contains(result.Patch, "+fixed") {
		t.Fatalf("unexpected result report=%#v patch=%s", result.Report, result.Patch)
	}
}

func TestValidateRecreatesBaseAndTargetTwice(t *testing.T) {
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
