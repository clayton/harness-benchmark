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

	"github.com/clayton/harness-benchmark/internal/corpus"
	"gopkg.in/yaml.v3"
)

var environmentName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var pinnedImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:\-]*@sha256:[0-9a-f]{64}$`)
var relayHosts = map[string]bool{"api.openai.com": true, "api.anthropic.com": true, "openrouter.ai": true, "api.x.ai": true, "api.meta.ai": true, "generativelanguage.googleapis.com": true}

type Pack struct {
	Schema          string `yaml:"schema"`
	ScenarioSlug    string `yaml:"scenario_slug"`
	ScenarioVersion int    `yaml:"scenario_version"`
	TargetRef       string `yaml:"target_ref"`
	// TargetWorkspace keeps an original scaffold's private solution inside the
	// evaluator pack. It is never copied into the public scenario manifest.
	TargetWorkspace        map[string]string `yaml:"target_workspace,omitempty"`
	EnvironmentImageDigest string            `yaml:"environment_image_digest"`
	RelayImageDigest       string            `yaml:"relay_image_digest"`
	ProtocolID             string            `yaml:"protocol_id"`
	EvaluatorCommands      []string          `yaml:"evaluator_commands"`
	Execution              Execution         `yaml:"execution,omitempty"`
	Relay                  Relay             `yaml:"relay,omitempty"`
	Setups                 map[string]Setup  `yaml:"setups,omitempty"`
	Budget                 map[string]any    `yaml:"budget"`
}

type Setup struct {
	Execution Execution `yaml:"execution"`
	Relay     Relay     `yaml:"relay"`
}

type Execution struct {
	Harness        string            `yaml:"harness"`
	HarnessVersion string            `yaml:"harness_version"`
	Provider       string            `yaml:"provider"`
	Model          string            `yaml:"model"`
	ModelVersion   string            `yaml:"model_version"`
	Reasoning      string            `yaml:"reasoning"`
	Extensions     []string          `yaml:"extensions,omitempty"`
	Plugins        []string          `yaml:"plugins,omitempty"`
	Command        string            `yaml:"command"`
	Environment    map[string]string `yaml:"environment"`
	Pricing        Pricing           `yaml:"pricing,omitempty"`
}

type Pricing struct {
	PromptUSDPerMillion     float64 `yaml:"prompt_usd_per_million"`
	CompletionUSDPerMillion float64 `yaml:"completion_usd_per_million"`
	CacheReadUSDPerMillion  float64 `yaml:"cache_read_usd_per_million"`
	CacheWriteUSDPerMillion float64 `yaml:"cache_write_usd_per_million"`
	Snapshot                string  `yaml:"snapshot"`
}

type Relay struct {
	Upstream                   string  `yaml:"upstream"`
	BaseURLEnv                 string  `yaml:"base_url_env"`
	SecretEnv                  string  `yaml:"secret_env"`
	AuthHeader                 string  `yaml:"auth_header"`
	AuthScheme                 string  `yaml:"auth_scheme"`
	DummyKeyEnv                string  `yaml:"dummy_key_env"`
	AllowedModel               string  `yaml:"allowed_model"`
	MaxRequestUSD              float64 `yaml:"max_request_usd"`
	MaxRequestBytes            int     `yaml:"max_request_bytes"`
	MaxOutputTokens            int     `yaml:"max_output_tokens"`
	MaxPromptUSDPerMillion     float64 `yaml:"max_prompt_usd_per_million"`
	MaxCompletionUSDPerMillion float64 `yaml:"max_completion_usd_per_million"`
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
	if pack.ScenarioSlug == "" || pack.ScenarioVersion < 1 || (pack.TargetRef == "" && len(pack.TargetWorkspace) == 0) || (pack.TargetRef != "" && len(pack.TargetWorkspace) > 0) || (pack.TargetRef != "" && !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(pack.TargetRef)) {
		return Pack{}, "", fmt.Errorf("evaluator pack scenario and target are required")
	}
	if len(pack.TargetWorkspace) > 0 {
		if _, err := corpus.ScaffoldPaths(pack.TargetWorkspace); err != nil {
			return Pack{}, "", fmt.Errorf("invalid private target workspace: %w", err)
		}
	}
	if !pinnedImage.MatchString(pack.EnvironmentImageDigest) {
		return Pack{}, "", fmt.Errorf("environment image must be pinned by sha256 digest")
	}
	if !pinnedImage.MatchString(pack.RelayImageDigest) {
		return Pack{}, "", fmt.Errorf("relay image must be pinned by sha256 digest")
	}
	if pack.ProtocolID != "controlled-v3" || len(pack.EvaluatorCommands) == 0 {
		return Pack{}, "", fmt.Errorf("controlled-v3 protocol and evaluator commands are required")
	}
	if len(pack.Setups) > 0 && pack.Execution.Command != "" {
		return Pack{}, "", fmt.Errorf("evaluator pack cannot combine a default execution with named setups")
	}
	if len(pack.Setups) == 0 {
		if err := validateSetup("default", Setup{Execution: pack.Execution, Relay: pack.Relay}, pack.Budget); err != nil {
			return Pack{}, "", err
		}
	} else {
		for id, setup := range pack.Setups {
			if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`).MatchString(id) {
				return Pack{}, "", fmt.Errorf("invalid controlled setup ID %q", id)
			}
			if err := validateSetup(id, setup, pack.Budget); err != nil {
				return Pack{}, "", err
			}
		}
	}
	digest, err := DigestDir(path)
	return pack, digest, err
}

func validateSetup(id string, setup Setup, budget map[string]any) error {
	execution, relay := setup.Execution, setup.Relay
	if execution.Command == "" || execution.Harness == "" || execution.HarnessVersion == "" || execution.Provider == "" || execution.Model == "" || execution.Reasoning == "" {
		return fmt.Errorf("controlled setup %s requires command, harness, harness version, provider, model, and reasoning", id)
	}
	if strings.EqualFold(execution.Provider, "cursor") || strings.Contains(strings.ToLower(execution.Command), "hbench-pi-cursor") || execution.Environment["CURSOR_BACKEND_URL"] != "" {
		return fmt.Errorf("controlled setup %s uses unsupported Cursor streaming; bounded accounting is required before controlled use", id)
	}
	pricing := execution.Pricing
	priced := pricing.PromptUSDPerMillion != 0 || pricing.CompletionUSDPerMillion != 0 || pricing.CacheReadUSDPerMillion != 0 || pricing.CacheWriteUSDPerMillion != 0
	if priced || pricing.Snapshot != "" {
		if pricing.Snapshot == "" || len(pricing.Snapshot) > 200 || pricing.PromptUSDPerMillion <= 0 || pricing.CompletionUSDPerMillion <= 0 || pricing.CacheReadUSDPerMillion < 0 || pricing.CacheWriteUSDPerMillion < 0 {
			return fmt.Errorf("controlled setup %s pricing snapshot and positive prompt/completion rates are required", id)
		}
	}
	upstream, err := url.Parse(relay.Upstream)
	if err != nil || upstream.Scheme != "https" || !relayHosts[upstream.Hostname()] {
		return fmt.Errorf("controlled setup %s relay upstream must be an approved HTTPS model provider", id)
	}
	if !environmentName.MatchString(relay.BaseURLEnv) || !environmentName.MatchString(relay.SecretEnv) || !environmentName.MatchString(relay.DummyKeyEnv) {
		return fmt.Errorf("controlled setup %s relay environment names are invalid", id)
	}
	if relay.BaseURLEnv == relay.SecretEnv {
		return fmt.Errorf("controlled setup %s provider secret cannot be exposed to the execution container", id)
	}
	if relay.AuthHeader != "Authorization" && relay.AuthHeader != "x-api-key" {
		return fmt.Errorf("controlled setup %s relay auth header is not allowed", id)
	}
	for key := range execution.Environment {
		upper := strings.ToUpper(key)
		if key == relay.SecretEnv || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "API_KEY") {
			return fmt.Errorf("controlled setup %s execution environment %s looks like a credential", id, key)
		}
	}
	maxUSD, capped := budgetNumber(budget, "max_usd")
	if !capped || maxUSD <= 0 || relay.AllowedModel == "" || relay.AllowedModel != execution.Model || relay.MaxRequestUSD <= 0 || relay.MaxRequestUSD > maxUSD ||
		relay.MaxRequestBytes <= 0 || relay.MaxRequestBytes > 20*1024*1024 || relay.MaxOutputTokens <= 0 ||
		relay.MaxPromptUSDPerMillion <= 0 || relay.MaxCompletionUSDPerMillion <= 0 ||
		relay.MaxRequestUSD+1e-12 < minimumRequestUSD(relay) {
		return fmt.Errorf("controlled setup %s requires model-bound relay and per-run/request limits", id)
	}
	return nil
}

func (pack Pack) SelectSetup(id string) (Pack, error) {
	if len(pack.Setups) == 0 {
		if id != "" && id != "default" {
			return Pack{}, fmt.Errorf("evaluator pack has no named setup %q", id)
		}
		return pack, nil
	}
	if id == "" {
		return Pack{}, fmt.Errorf("--setup is required; available setups: %s", strings.Join(sortedSetupIDs(pack.Setups), ", "))
	}
	setup, ok := pack.Setups[id]
	if !ok {
		return Pack{}, fmt.Errorf("unknown controlled setup %q; available setups: %s", id, strings.Join(sortedSetupIDs(pack.Setups), ", "))
	}
	pack.Execution = setup.Execution
	pack.Relay = setup.Relay
	return pack, nil
}

func sortedSetupIDs(setups map[string]Setup) []string {
	ids := make([]string, 0, len(setups))
	for id := range setups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ValidateForScenario applies the scenario-dependent private target rules
// after a pack is loaded. A target workspace is valid only for a scaffold
// base; ordinary repository scenarios use exactly one target commit.
func (pack Pack) ValidateForScenario(scenario corpus.Scenario) error {
	if len(pack.TargetWorkspace) > 0 && scenario.Workspace.Kind != "scaffold" {
		return fmt.Errorf("private target workspace requires a scaffold scenario")
	}
	if len(pack.TargetWorkspace) == 0 && pack.TargetRef == "" {
		return fmt.Errorf("evaluator pack requires exactly one private target")
	}
	if len(pack.TargetWorkspace) > 0 && pack.TargetRef != "" {
		return fmt.Errorf("evaluator pack cannot define both target_ref and target_workspace")
	}
	return nil
}

func minimumRequestUSD(relay Relay) float64 {
	return (float64(relay.MaxRequestBytes)*relay.MaxPromptUSDPerMillion + float64(relay.MaxOutputTokens)*relay.MaxCompletionUSDPerMillion) / 1_000_000
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
