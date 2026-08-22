package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func CreateRun(l paths.Layout, sc corpus.Scenario, harness string, runSetup bool) (RunRecord, error) {
	return CreateRunWithModel(l, sc, harness, "", runSetup)
}

func CreateRunWithModel(l paths.Layout, sc corpus.Scenario, harness, model string, runSetup bool) (RunRecord, error) {
	return CreateRunWithProfile(l, sc, Profile{Harness: harness, Model: model}, runSetup)
}

type Profile struct {
	ID              string         `json:"id,omitempty"`
	Harness         string         `json:"harness"`
	HarnessVersion  string         `json:"harness_version,omitempty"`
	Provider        string         `json:"provider,omitempty"`
	Model           string         `json:"model"`
	Reasoning       string         `json:"reasoning,omitempty"`
	Workflow        string         `json:"workflow,omitempty"`
	Skills          []string       `json:"skills,omitempty"`
	Extensions      []string       `json:"extensions,omitempty"`
	Plugins         []string       `json:"plugins,omitempty"`
	Tools           []string       `json:"tools,omitempty"`
	Subagents       string         `json:"subagent_topology,omitempty"`
	Environment     string         `json:"environment,omitempty"`
	Network         string         `json:"network,omitempty"`
	JudgeProtocol   string         `json:"judge_protocol,omitempty"`
	Budget          map[string]any `json:"budget,omitempty"`
	StudyID         string         `json:"study_id,omitempty"`
	ContractDigest  string         `json:"contract_digest,omitempty"`
	ArmID           string         `json:"arm_id,omitempty"`
	Repeat          int            `json:"repeat,omitempty"`
	ScenarioDigest  string         `json:"scenario_digest,omitempty"`
	StudyScenarioID string         `json:"study_scenario_id,omitempty"`
}

func CreateRunWithProfile(l paths.Layout, sc corpus.Scenario, profile Profile, runSetup bool) (RunRecord, error) {
	id := NewID()
	wt := l.Worktree(id)
	if profile.Model == "" {
		profile.Model = defaultModel(profile.Harness)
	}
	if profile.Workflow == "" {
		profile.Workflow = "baseline"
	}
	if profile.ID == "" {
		profile.ID = profile.Harness + "-" + profile.Workflow
	}
	interaction := "unattended"
	if profile.Harness == "manual" {
		interaction = "human"
	}
	sum := sha256.Sum256([]byte(stringsTrim(sc.Prompt)))
	rec := RunRecord{
		ID:             id,
		ScenarioID:     sc.ID,
		ConfigID:       profile.ID,
		Status:         "preparing",
		Worktree:       wt,
		Harness:        profile.Harness,
		HarnessVersion: profile.HarnessVersion,
		Model:          profile.Model,
		Metadata: map[string]any{
			"workflow":         profile.Workflow,
			"skills":           profile.Skills,
			"profile":          profile,
			"interaction":      interaction,
			"prompt_sha256_16": hex.EncodeToString(sum[:])[:16],
			"base_ref":         sc.Repo.BaseRef,
			"gold_ref":         sc.Repo.GoldRef,
		},
		CreatedAt: Now(),
	}
	if err := Save(l, rec); err != nil {
		return RunRecord{}, err
	}
	snap := map[string]any{
		"schema":           "hb.snapshot.v1",
		"run_id":           id,
		"prompt_sha256_16": hex.EncodeToString(sum[:])[:16],
		"scenario":         sc,
		"config": map[string]any{
			"id":                rec.ConfigID,
			"harness":           profile.Harness,
			"harness_version":   profile.HarnessVersion,
			"provider":          profile.Provider,
			"model":             profile.Model,
			"reasoning":         profile.Reasoning,
			"workflow":          profile.Workflow,
			"skills":            profile.Skills,
			"extensions":        profile.Extensions,
			"plugins":           profile.Plugins,
			"tools":             profile.Tools,
			"subagent_topology": profile.Subagents,
			"environment":       profile.Environment,
			"network":           profile.Network,
			"judge_protocol":    profile.JudgeProtocol,
			"budget":            profile.Budget,
			"interaction":       interaction,
		},
		"repo": map[string]any{
			"url":      sc.Repo.URL,
			"base_ref": sc.Repo.BaseRef,
			"gold_ref": sc.Repo.GoldRef,
		},
	}
	if profile.StudyID != "" {
		snap["study"] = map[string]any{
			"id": profile.StudyID, "contract_digest": profile.ContractDigest,
			"arm_id": profile.ArmID, "scenario_id": profile.StudyScenarioID, "repeat": profile.Repeat,
			"scenario_digest": profile.ScenarioDigest,
		}
	}
	raw, _ := json.MarshalIndent(snap, "", "  ")
	_ = WriteFileAtomic(filepath.Join(l.RunDir(id), "snapshot.json"), append(raw, '\n'), 0o644)
	_ = WriteFileAtomic(filepath.Join(l.RunDir(id), "prompt.txt"), []byte(stringsTrim(sc.Prompt)+"\n"), 0o600)
	if err := SetLatest(l, id); err != nil {
		return RunRecord{}, err
	}
	started := time.Now()
	prepared, err := PrepareWorktree(l, sc, id, runSetup)
	setupWall := int(time.Since(started).Milliseconds())
	rec.Metadata["setup_wall_ms"] = setupWall
	if err != nil {
		rec.Status = "setup_failed"
		rec.Error = err.Error()
		rec.FinishedAt = Now()
		rec.Metadata["failed_phase"] = "setup"
		_ = Save(l, rec)
		return rec, fmt.Errorf("run %s setup failed: %w", id, err)
	}
	rec.Worktree = prepared
	rec.Status = "pending"
	if err := Save(l, rec); err != nil {
		return rec, err
	}
	return rec, nil
}

func defaultModel(harness string) string {
	switch harness {
	case "grok", "pi":
		return "grok-4.5"
	case "claude":
		return "claude-sonnet-4"
	case "codex":
		return "gpt-5"
	case "cursor":
		return "composer-2.5"
	default:
		return ""
	}
}

func stringsTrim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

type LaunchSpec struct {
	Program string
	Args    []string
	Env     []string
}

var modelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

func NormalizeDirectProfile(profile Profile) (Profile, error) {
	if profile.Model == "" {
		profile.Model = defaultModel(profile.Harness)
	}
	if i := strings.LastIndex(profile.Model, ":"); i >= 0 {
		suffix := profile.Model[i+1:]
		if isReasoningLevel(suffix) {
			if profile.Reasoning != "" && profile.Reasoning != suffix {
				return Profile{}, fmt.Errorf("model reasoning %q conflicts with --reasoning %q", suffix, profile.Reasoning)
			}
			profile.Reasoning = suffix
			profile.Model = profile.Model[:i]
		}
	}
	if profile.Harness == "pi" {
		derivedProvider, model := splitPiModel(profile.Model)
		if profile.Provider != "" && derivedProvider != "" && profile.Provider != derivedProvider {
			return Profile{}, fmt.Errorf("model provider %q conflicts with --provider %q", derivedProvider, profile.Provider)
		}
		if profile.Provider == "" {
			profile.Provider = derivedProvider
		}
		if model != "" {
			profile.Model = model
		}
		if profile.Provider == "" {
			return Profile{}, fmt.Errorf("Pi runs must set --provider or use a provider/model identifier")
		}
		if profile.Reasoning == "ultra" {
			return Profile{}, fmt.Errorf("Pi does not support reasoning level ultra")
		}
	}
	if profile.Provider == "" && profile.Harness == "codex" {
		profile.Provider = "openai"
	}
	if profile.Reasoning != "" && !isReasoningLevel(profile.Reasoning) {
		return Profile{}, fmt.Errorf("unsupported reasoning level %q", profile.Reasoning)
	}
	if profile.Workflow == "" {
		profile.Workflow = "baseline"
	}
	if profile.Reasoning == "" && (profile.Harness == "pi" || profile.Harness == "codex") {
		profile.Reasoning = "default"
	}
	if err := ValidateMeasuredProfile(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func isReasoningLevel(value string) bool {
	switch value {
	case "default", "off", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		return true
	default:
		return false
	}
}

func HeadlessCommand(harness string) string {
	return HeadlessLaunch(harness, "", "").Program
}

// HeadlessLaunch returns argv, never a shell command. Prompt text is passed as
// one argument (or via the harness' prompt-file option), so model and prompt
// values cannot be interpreted by a shell.
func HeadlessLaunch(harness, model, prompt string) LaunchSpec {
	return HeadlessLaunchProfile(Profile{Harness: harness, Model: model}, prompt)
}

func HeadlessLaunchProfile(p Profile, prompt string) LaunchSpec {
	harness, model := p.Harness, p.Model
	if model != "" && !modelIDPattern.MatchString(model) {
		return LaunchSpec{}
	}
	switch harness {
	case "grok":
		args := []string{"--always-approve", "--max-turns", "80", "--output-format", "json", "--permission-mode", "bypassPermissions", "--prompt-file", "HB_PROMPT.txt"}
		if model != "" {
			args = append([]string{"-m", model}, args...)
		}
		return LaunchSpec{Program: harnessProgram(harness), Args: args}
	case "pi":
		args := []string{"-p", "--mode", "json", "--approve", "--no-extensions", "--no-skills"}
		if !profileNeedsChildUsage(p) {
			args = append(args, "--no-session")
		}
		for _, v := range p.Extensions {
			args = append(args, "--extension", v)
		}
		for _, v := range p.Plugins {
			args = append(args, "--extension", v)
		}
		for _, v := range p.Skills {
			args = append(args, "--skill", v)
		}
		provider, mid := splitPiModel(model)
		if p.Provider != "" {
			provider = p.Provider
			if provider == "openrouter" {
				mid = model
			}
		}
		if provider != "" {
			args = append(args, "--provider", provider)
		}
		if mid != "" {
			args = append(args, "--model", mid)
		}
		if p.Reasoning != "" && p.Reasoning != "default" {
			args = append(args, "--thinking", p.Reasoning)
		}
		args = append(args, "--append-system-prompt", "UNATTENDED BENCHMARK MODE: Do not ask the user any questions. The task is fully specified in the user message. Work only in the current working directory. Do not create git worktrees outside this directory. Implement the fix, verify, then stop.", prompt)
		return LaunchSpec{Program: harnessProgram(harness), Args: args}
	case "claude":
		args := []string{"-p", "--output-format", "json", "--dangerously-skip-permissions"}
		if model != "" {
			args = append(args, "--model", model)
		}
		return LaunchSpec{Program: harnessProgram(harness), Args: append(args, prompt)}
	case "codex":
		args := []string{"exec", "--skip-git-repo-check", "--json", "--ephemeral", "--ignore-user-config", "--sandbox", "workspace-write"}
		if model != "" {
			args = append(args, "-m", model)
		}
		if p.Reasoning != "" && p.Reasoning != "default" {
			args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", p.Reasoning))
		}
		if p.Subagents != "" {
			count, childModel, childEffort, ok := parseCodexTopology(p.Subagents)
			if !ok {
				return LaunchSpec{}
			}
			args = append(args,
				"-c", "agents.enabled=true",
				"-c", "agents.max_concurrent_threads_per_session="+strconv.Itoa(count),
				"-c", fmt.Sprintf("agents.default_subagent_model=%q", childModel),
				"-c", fmt.Sprintf("agents.default_subagent_reasoning_effort=%q", childEffort),
			)
			prompt = fmt.Sprintf("MEASURED WORKFLOW: Spawn exactly %d subagents using the configured default child model and reasoning effort. Divide independent work among them, wait for all of them, integrate their results, verify the task, then stop.\n\n%s", count, prompt)
		}
		args = append(args, prompt)
		return LaunchSpec{Program: harnessProgram(harness), Args: args}
	case "cursor":
		mid := model
		if mid == "" {
			mid = "composer-2.5"
		}
		return LaunchSpec{Program: harnessProgram(harness), Args: []string{"-p", "--output-format", "stream-json", "--force", "--model", mid, prompt}}
	default:
		return LaunchSpec{}
	}
}

var codexTopologyPattern = regexp.MustCompile(`^([1-9]|1[0-6])x:([A-Za-z0-9][A-Za-z0-9._/-]{0,127}):(low|medium|high|xhigh|max|ultra)$`)

func parseCodexTopology(value string) (count int, model, effort string, ok bool) {
	match := codexTopologyPattern.FindStringSubmatch(value)
	if match == nil {
		return 0, "", "", false
	}
	count, _ = strconv.Atoi(match[1])
	return count, match[2], match[3], true
}

var packageVersionPattern = regexp.MustCompile(`^(?:@[A-Za-z0-9._-]+/)?[A-Za-z0-9._-]+@[0-9][A-Za-z0-9._+-]*$`)

// ValidateMeasuredProfile rejects setup labels the adapter cannot enforce.
// A public study must measure behavior, not merely describe it in metadata.
func ValidateMeasuredProfile(p Profile) error {
	if p.Network != "" {
		return fmt.Errorf("%s adapter cannot enforce network=%q in local study execution", p.Harness, p.Network)
	}
	if p.Environment != "" {
		return fmt.Errorf("%s adapter cannot enforce environment=%q in local study execution", p.Harness, p.Environment)
	}
	if len(p.Tools) > 0 {
		return fmt.Errorf("%s adapter cannot lock declared tools", p.Harness)
	}
	switch p.Harness {
	case "codex":
		if p.Provider != "" && p.Provider != "openai" {
			return fmt.Errorf("codex adapter uses the OpenAI provider, not %q", p.Provider)
		}
		if len(p.Skills)+len(p.Extensions)+len(p.Plugins) > 0 {
			return fmt.Errorf("codex adapter cannot yet lock declared skills, extensions, or plugins")
		}
		if p.Subagents != "" {
			if _, _, _, ok := parseCodexTopology(p.Subagents); !ok {
				return fmt.Errorf("invalid Codex subagent_topology %q; use COUNTx:MODEL:EFFORT", p.Subagents)
			}
			if p.Workflow != "codex-subagents" {
				return fmt.Errorf("Codex subagent topology requires workflow=codex-subagents")
			}
		} else if p.Workflow != "" && p.Workflow != "baseline" {
			return fmt.Errorf("codex adapter cannot enforce workflow=%q", p.Workflow)
		}
	case "pi":
		if p.Provider == "" {
			return fmt.Errorf("Pi studies must pin provider instead of using Pi's default")
		}
		derivedProvider, _ := splitPiModel(p.Model)
		if p.Provider != "openrouter" && derivedProvider != "" && derivedProvider != p.Provider {
			return fmt.Errorf("Pi provider %q conflicts with model %q", p.Provider, p.Model)
		}
		if p.Workflow != "" && p.Workflow != "baseline" {
			return fmt.Errorf("pi adapter cannot enforce workflow=%q", p.Workflow)
		}
		if p.Subagents != "" {
			return fmt.Errorf("Pi child topology must be supplied by a pinned plugin, not a descriptive subagent_topology")
		}
		for _, artifact := range append(append(append([]string{}, p.Skills...), p.Extensions...), p.Plugins...) {
			if !packageVersionPattern.MatchString(artifact) {
				return fmt.Errorf("Pi artifact %q must pin an exact package version", artifact)
			}
		}
	case "cursor", "claude", "grok":
		if p.Provider != "" || p.Reasoning != "" || len(p.Skills)+len(p.Extensions)+len(p.Plugins) > 0 || p.Subagents != "" {
			return fmt.Errorf("%s adapter cannot enforce one or more declared setup axes", p.Harness)
		}
		if p.Workflow != "" && p.Workflow != "baseline" {
			return fmt.Errorf("%s adapter cannot enforce workflow=%q", p.Harness, p.Workflow)
		}
	default:
		return fmt.Errorf("unsupported study harness %q", p.Harness)
	}
	return nil
}

type piPackageManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Pi      struct {
		Extensions []string `json:"extensions"`
		Skills     []string `json:"skills"`
	} `json:"pi"`
}

// ResolveMeasuredProfile converts exact Pi package versions into explicit
// filesystem paths. Pi then runs with discovery disabled, so undeclared global
// extensions and skills cannot enter the measured setup.
func ResolveMeasuredProfile(p Profile) (Profile, error) {
	if err := ValidateMeasuredProfile(p); err != nil {
		return Profile{}, err
	}
	if p.Harness != "pi" {
		return p, nil
	}
	var err error
	if p.Extensions, err = resolvePiPackages(p.Extensions, "extensions"); err != nil {
		return Profile{}, err
	}
	if p.Plugins, err = resolvePiPackages(p.Plugins, "extensions"); err != nil {
		return Profile{}, err
	}
	if p.Skills, err = resolvePiPackages(p.Skills, "skills"); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func resolvePiPackages(specs []string, resource string) ([]string, error) {
	var resolved []string
	for _, spec := range specs {
		at := strings.LastIndex(spec, "@")
		if at <= 0 || at == len(spec)-1 {
			return nil, fmt.Errorf("Pi artifact %q must pin an exact package version", spec)
		}
		name, version := spec[:at], spec[at+1:]
		home, _ := os.UserHomeDir()
		piRoot := os.Getenv("PI_CODING_AGENT_DIR")
		if piRoot == "" {
			piRoot = filepath.Join(home, ".pi", "agent")
		}
		packageRoot := filepath.Join(piRoot, "npm", "node_modules", filepath.FromSlash(name))
		raw, err := os.ReadFile(filepath.Join(packageRoot, "package.json"))
		if err != nil {
			return nil, fmt.Errorf("Pi package %s@%s is not installed: %w", name, version, err)
		}
		var manifest piPackageManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, fmt.Errorf("parse Pi package %s: %w", name, err)
		}
		if manifest.Name != name || manifest.Version != version {
			return nil, fmt.Errorf("Pi package drift for %s: contract has %s, installed package is %s@%s", name, spec, manifest.Name, manifest.Version)
		}
		entries := manifest.Pi.Extensions
		if resource == "skills" {
			entries = manifest.Pi.Skills
		}
		if len(entries) == 0 {
			return nil, fmt.Errorf("Pi package %s declares no %s", spec, resource)
		}
		for _, entry := range entries {
			clean := filepath.Clean(filepath.FromSlash(entry))
			if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("Pi package %s has unsafe %s path %q", spec, resource, entry)
			}
			path := filepath.Join(packageRoot, clean)
			if _, err := os.Stat(path); err != nil {
				return nil, fmt.Errorf("Pi package %s is missing %s path %q", spec, resource, entry)
			}
			resolved = append(resolved, path)
		}
	}
	return resolved, nil
}

func harnessProgram(harness string) string {
	if harness == "cursor" {
		return "cursor-agent"
	}
	return harness
}

func splitPiModel(model string) (provider, id string) {
	model = stringsTrim(model)
	if model == "" {
		return "", ""
	}
	if i := indexByte(model, '/'); i >= 0 {
		left, right := model[:i], model[i+1:]
		if left == "x-ai" || left == "openrouter" {
			return "openrouter", model
		}
		return left, right
	}
	if len(model) >= 4 && model[:4] == "grok" {
		return "xai", model
	}
	return "", model
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func ScenarioForRun(l paths.Layout, officialDir, id string) (corpus.Scenario, error) {
	rec, err := Load(l, id)
	if err != nil {
		return corpus.Scenario{}, err
	}
	if rec.ScenarioID != "" {
		if sc, err := corpus.Find(officialDir, rec.ScenarioID); err == nil {
			return sc, nil
		}
	}
	return scenarioFromSnapshot(l, id)
}

func scenarioFromSnapshot(l paths.Layout, id string) (corpus.Scenario, error) {
	raw, err := os.ReadFile(filepath.Join(l.RunDir(id), "snapshot.json"))
	if err != nil {
		return corpus.Scenario{}, fmt.Errorf("scenario not found for run %s", id)
	}
	var wrap struct {
		Scenario json.RawMessage `json:"scenario"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return corpus.Scenario{}, err
	}
	sc, err := corpus.UnmarshalScenarioJSON(wrap.Scenario)
	if err != nil {
		return corpus.Scenario{}, err
	}
	if sc.ID == "" {
		return corpus.Scenario{}, fmt.Errorf("scenario not found for run %s", id)
	}
	return sc, nil
}

func ResolveRunID(l paths.Layout, id string) (string, error) {
	if id != "" && id != "latest" {
		return id, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		if rid := RunIDFromPath(cwd, l.OutDir); rid != "" {
			return rid, nil
		}
	}
	return LatestID(l)
}

func MustOutDir(l paths.Layout) error {
	return os.MkdirAll(l.OutDir, 0o755)
}
