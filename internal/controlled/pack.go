package controlled

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var environmentName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var pinnedImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:\-]*@sha256:[0-9a-f]{64}$`)
var relayHosts = map[string]bool{"api.openai.com": true, "api.anthropic.com": true, "openrouter.ai": true, "api.x.ai": true, "generativelanguage.googleapis.com": true}

type Pack struct {
	Schema                 string         `yaml:"schema"`
	ScenarioSlug           string         `yaml:"scenario_slug"`
	ScenarioVersion        int            `yaml:"scenario_version"`
	TargetRef              string         `yaml:"target_ref"`
	EnvironmentImageDigest string         `yaml:"environment_image_digest"`
	ProtocolID             string         `yaml:"protocol_id"`
	EvaluatorCommands      []string       `yaml:"evaluator_commands"`
	Execution              Execution      `yaml:"execution"`
	Relay                  Relay          `yaml:"relay"`
	Budget                 map[string]any `yaml:"budget"`
}

type Execution struct {
	Harness        string            `yaml:"harness"`
	HarnessVersion string            `yaml:"harness_version"`
	Model          string            `yaml:"model"`
	ModelVersion   string            `yaml:"model_version"`
	Command        string            `yaml:"command"`
	Environment    map[string]string `yaml:"environment"`
}

type Relay struct {
	Upstream    string `yaml:"upstream"`
	BaseURLEnv  string `yaml:"base_url_env"`
	SecretEnv   string `yaml:"secret_env"`
	AuthHeader  string `yaml:"auth_header"`
	AuthScheme  string `yaml:"auth_scheme"`
	DummyKeyEnv string `yaml:"dummy_key_env"`
}

func PinnedImage(value string) bool {
	return pinnedImage.MatchString(value)
}

func LoadPack(path string) (Pack, string, error) {
	raw, err := os.ReadFile(filepath.Join(path, "pack.yaml"))
	if err != nil {
		return Pack{}, "", err
	}
	var pack Pack
	if err := yaml.Unmarshal(raw, &pack); err != nil {
		return Pack{}, "", err
	}
	if pack.Schema != "rodeo.evaluator.v1" {
		return Pack{}, "", fmt.Errorf("unsupported evaluator schema %q", pack.Schema)
	}
	if pack.ScenarioSlug == "" || pack.ScenarioVersion < 1 || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(pack.TargetRef) {
		return Pack{}, "", fmt.Errorf("evaluator pack scenario and target are required")
	}
	if !pinnedImage.MatchString(pack.EnvironmentImageDigest) {
		return Pack{}, "", fmt.Errorf("environment image must be pinned by sha256 digest")
	}
	if pack.ProtocolID != "controlled-v3" || len(pack.EvaluatorCommands) == 0 {
		return Pack{}, "", fmt.Errorf("controlled-v3 protocol and evaluator commands are required")
	}
	if pack.Execution.Command != "" {
		upstream, err := url.Parse(pack.Relay.Upstream)
		if err != nil || upstream.Scheme != "https" || !relayHosts[upstream.Hostname()] {
			return Pack{}, "", fmt.Errorf("relay upstream must be an approved HTTPS model provider")
		}
		if !environmentName.MatchString(pack.Relay.BaseURLEnv) || !environmentName.MatchString(pack.Relay.SecretEnv) ||
			!environmentName.MatchString(pack.Relay.DummyKeyEnv) {
			return Pack{}, "", fmt.Errorf("relay environment names are invalid")
		}
		if pack.Relay.BaseURLEnv == pack.Relay.SecretEnv {
			return Pack{}, "", fmt.Errorf("provider secret cannot be exposed to the execution container")
		}
		if pack.Relay.AuthHeader != "Authorization" && pack.Relay.AuthHeader != "x-api-key" {
			return Pack{}, "", fmt.Errorf("relay auth header is not allowed")
		}
		for key := range pack.Execution.Environment {
			upper := strings.ToUpper(key)
			if key == pack.Relay.SecretEnv || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "API_KEY") {
				return Pack{}, "", fmt.Errorf("execution environment %s looks like a credential", key)
			}
		}
	}
	digest, err := DigestDir(path)
	return pack, digest, err
}

func DigestDir(root string) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".DS_Store") {
			return filepath.SkipDir
		}
		if !entry.IsDir() && entry.Name() != ".DS_Store" {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, rel := range paths {
		_, _ = io.WriteString(hash, rel+"\x00")
		file, err := os.Open(filepath.Join(root, rel))
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(hash, file)
		_ = file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		_, _ = io.WriteString(hash, "\x00")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
