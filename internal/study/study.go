package study

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/clayton/harness-benchmark/internal/adapter"
	"gopkg.in/yaml.v3"
)

const Schema = "hb.study.v1"
const SchemaV2 = "hb.study.v2"
const WinRule = "callout-title-v1"

type Manifest struct {
	Schema         string     `yaml:"schema" json:"schema"`
	ID             string     `yaml:"id" json:"id"`
	Question       string     `yaml:"question" json:"question"`
	Visibility     string     `yaml:"visibility,omitempty" json:"visibility,omitempty"`
	Private        bool       `yaml:"private,omitempty" json:"private,omitempty"`
	Sources        []Source   `yaml:"sources,omitempty" json:"sources,omitempty"`
	ComparisonMode string     `yaml:"comparison_mode" json:"comparison_mode"`
	Scenarios      []Scenario `yaml:"scenarios" json:"scenarios"`
	Arms           []Arm      `yaml:"arms" json:"arms"`
	VariedAxes     []string   `yaml:"varied_axes" json:"varied_axes"`
	Repeats        int        `yaml:"repeats" json:"repeats"`
	Seed           int64      `yaml:"seed" json:"seed"`
	JudgeProtocol  string     `yaml:"judge_protocol" json:"judge_protocol"`
	WinRule        string     `yaml:"win_rule" json:"win_rule"`
	Budget         Budget     `yaml:"budget" json:"budget"`
}

type Source struct {
	URL     string `yaml:"url" json:"url"`
	Author  string `yaml:"author,omitempty" json:"author,omitempty"`
	Summary string `yaml:"summary,omitempty" json:"summary,omitempty"`
}
type Scenario struct {
	ID        string         `yaml:"id" json:"id"`
	Digest    string         `yaml:"digest,omitempty" json:"digest,omitempty"`
	Task      map[string]any `yaml:"task,omitempty" json:"task,omitempty"`
	LocalPath string         `yaml:"-" json:"-"`
}
type Budget struct {
	MaxUSDPerRun *float64 `yaml:"max_usd_per_run,omitempty" json:"max_usd_per_run,omitempty"`
	MaxUSDTotal  *float64 `yaml:"max_usd_total,omitempty" json:"max_usd_total,omitempty"`
	MaxTokens    *int     `yaml:"max_tokens_per_run,omitempty" json:"max_tokens_per_run,omitempty"`
	MaxMinutes   int      `yaml:"max_minutes_per_run" json:"max_minutes_per_run"`
}
type Arm struct {
	ID                string         `yaml:"id" json:"id"`
	Mode              string         `yaml:"mode,omitempty" json:"mode,omitempty"`
	LocalSkills       []string       `yaml:"local_skill_dirs,omitempty" json:"local_skill_dirs,omitempty"`
	LocalSkillDigests []string       `yaml:"local_skill_digests,omitempty" json:"local_skill_digests,omitempty"`
	ConfigDigest      string         `yaml:"config_sha256,omitempty" json:"config_sha256,omitempty"`
	ConfigStatus      string         `yaml:"config_status,omitempty" json:"config_status,omitempty"`
	Harness           string         `yaml:"harness" json:"harness"`
	Version           string         `yaml:"harness_version,omitempty" json:"harness_version,omitempty"`
	Provider          string         `yaml:"provider,omitempty" json:"provider,omitempty"`
	Model             string         `yaml:"model" json:"model"`
	Reasoning         string         `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
	Workflow          string         `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	Skills            []string       `yaml:"skills,omitempty" json:"skills,omitempty"`
	Extensions        []string       `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	Plugins           []string       `yaml:"plugins,omitempty" json:"plugins,omitempty"`
	Tools             []string       `yaml:"tools,omitempty" json:"tools,omitempty"`
	Subagents         string         `yaml:"subagent_topology,omitempty" json:"subagent_topology,omitempty"`
	Environment       string         `yaml:"environment,omitempty" json:"environment,omitempty"`
	Network           string         `yaml:"network,omitempty" json:"network,omitempty"`
	ModelVersion      string         `yaml:"model_version,omitempty" json:"model_version,omitempty"`
	PromptTreatment   map[string]any `yaml:"prompt_treatment,omitempty" json:"prompt_treatment,omitempty"`
	Adapter           map[string]any `yaml:"adapter,omitempty" json:"adapter,omitempty"`
	Assurance         map[string]any `yaml:"assurance,omitempty" json:"assurance,omitempty"`
}

var axes = []string{"harness", "harness_version", "provider", "model", "model_version", "mode", "config", "reasoning", "workflow", "skills", "extensions", "plugins", "tools", "subagent_topology", "environment", "network", "prompt_treatment", "adapter", "assurance"}
var publishedScenarioID = regexp.MustCompile(`^rodeo:[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*@[1-9][0-9]*$`)

func Load(path string) (Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("parse study: %w", err)
	}
	return m, m.Validate()
}

func (m Manifest) Validate() error {
	if m.Schema != Schema && m.Schema != SchemaV2 {
		return fmt.Errorf("schema must be %s or %s", Schema, SchemaV2)
	}
	v2 := m.Schema == SchemaV2
	if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Question) == "" {
		return fmt.Errorf("id and question are required")
	}
	if len(m.ID) > 100 || len(m.Question) > 500 || len(m.Sources) > 20 || len(m.Scenarios) > 20 || len(m.Arms) > 20 {
		return fmt.Errorf("study contract exceeds field or collection limits")
	}
	for _, source := range m.Sources {
		if len(source.URL) > 2048 || len(source.Author) > 200 || len(source.Summary) > 1000 {
			return fmt.Errorf("study source exceeds field limits")
		}
	}
	if strings.TrimSpace(m.JudgeProtocol) == "" || strings.TrimSpace(m.WinRule) == "" {
		return fmt.Errorf("judge_protocol and win_rule are required")
	}
	if m.WinRule != WinRule {
		return fmt.Errorf("win_rule must be %s", WinRule)
	}
	if m.ComparisonMode != "controlled" && m.ComparisonMode != "ecological" {
		return fmt.Errorf("comparison_mode must be controlled or ecological")
	}
	if len(m.Scenarios) == 0 || len(m.Arms) < 2 {
		return fmt.Errorf("at least one scenario and two arms are required")
	}
	if m.Repeats < 1 {
		return fmt.Errorf("repeats must be at least 1")
	}
	if m.Repeats > 10 || m.RunCount() > 200 {
		return fmt.Errorf("study matrix exceeds 200 cells or 10 repeats")
	}
	if m.Budget.MaxMinutes < 1 {
		return fmt.Errorf("budget.max_minutes_per_run must be positive")
	}
	seen := map[string]bool{}
	scenarioSeen := map[string]bool{}
	if m.Visibility != "" && m.Visibility != "public" && m.Visibility != "private" {
		return fmt.Errorf("visibility must be public or private")
	}
	for _, scenario := range m.Scenarios {
		if scenario.ID == "" || len(scenario.ID) > 200 || scenarioSeen[scenario.ID] {
			return fmt.Errorf("scenario ids must be present and unique")
		}
		if !v2 && !m.IsPrivate() && !publishedScenarioID.MatchString(scenario.ID) {
			return fmt.Errorf("scenario %q is not publishable; studies require rodeo:slug@version IDs", scenario.ID)
		}
		if len(scenario.Digest) != 64 && !(v2 && len(scenario.Task) > 0) {
			return fmt.Errorf("scenario %q needs a 64-character digest", scenario.ID)
		}
		if scenario.Digest != "" {
			if _, err := hex.DecodeString(scenario.Digest); err != nil {
				return fmt.Errorf("scenario %q has an invalid digest", scenario.ID)
			}
		}
		if v2 && len(scenario.Task) > 0 {
			if err := ValidatePublicTask(scenario.Task); err != nil {
				return fmt.Errorf("scenario %q task: %w", scenario.ID, err)
			}
			if scenario.Digest != "" {
				digest, err := TaskDigest(scenario.Task)
				if err != nil || digest != scenario.Digest {
					return fmt.Errorf("scenario %q task digest does not match", scenario.ID)
				}
			}
		}
		if !v2 && len(scenario.Task) > 0 {
			return fmt.Errorf("scenario task objects require %s", SchemaV2)
		}
		scenarioSeen[scenario.ID] = true
	}
	for _, a := range m.Arms {
		if (a.Mode != "" || len(a.LocalSkills) > 0 || len(a.LocalSkillDigests) > 0 || a.ConfigDigest != "" || a.ConfigStatus != "") && !m.IsPrivate() && !v2 {
			return fmt.Errorf("arm %q personal setup fields are only allowed in private studies", a.ID)
		}
		if !v2 && (a.ModelVersion != "" || len(a.PromptTreatment) > 0 || len(a.Adapter) > 0 || len(a.Assurance) > 0) {
			return fmt.Errorf("arm %q uses v2-only setup fields", a.ID)
		}
		if v2 && a.Mode != "" && a.Mode != "personal" && a.Mode != "clean-baseline" {
			return fmt.Errorf("arm %q has invalid mode", a.ID)
		}
		if a.ConfigStatus != "" && a.ConfigStatus != "missing" && a.ConfigStatus != "captured" {
			return fmt.Errorf("arm %q has invalid config status", a.ID)
		}
		if a.ConfigStatus == "captured" && len(a.ConfigDigest) != 64 {
			return fmt.Errorf("arm %q captured config needs a 64-character digest", a.ID)
		}
		if (a.ConfigDigest != "" || a.ConfigStatus != "") && !(a.Mode == "personal" && a.Harness == "codex") {
			return fmt.Errorf("arm %q config fingerprint is only supported for personal codex setups", a.ID)
		}
		if a.ConfigStatus == "missing" && a.ConfigDigest != "" {
			return fmt.Errorf("arm %q missing config cannot have a digest", a.ID)
		}
		if a.ConfigDigest != "" {
			if len(a.ConfigDigest) != 64 {
				return fmt.Errorf("arm %q has an invalid config digest", a.ID)
			}
			if _, err := hex.DecodeString(a.ConfigDigest); err != nil {
				return fmt.Errorf("arm %q has an invalid config digest", a.ID)
			}
		}
		if a.Mode == "personal" && a.Harness == "codex" && a.ConfigStatus == "" {
			return fmt.Errorf("arm %q personal codex setup needs a config fingerprint", a.ID)
		}
		if len(a.LocalSkills) > 0 && len(a.LocalSkills) != len(a.LocalSkillDigests) {
			return fmt.Errorf("arm %q local skill paths and digests must match", a.ID)
		}
		if m.IsPrivate() && len(a.LocalSkills) != len(a.LocalSkillDigests) {
			return fmt.Errorf("arm %q private local skill paths and digests must match", a.ID)
		}
		if len(a.LocalSkills) > 0 && a.Mode != "personal" {
			return fmt.Errorf("arm %q local skills require personal mode", a.ID)
		}
		for name, descriptor := range map[string]map[string]any{"prompt_treatment": a.PromptTreatment, "assurance": a.Assurance} {
			if len(descriptor) > 0 {
				if err := validatePublicDescriptor(descriptor, 0); err != nil {
					return fmt.Errorf("arm %q %s: %w", a.ID, name, err)
				}
			}
		}
		if len(a.Adapter) > 0 {
			if _, _, err := adapter.FromMap(a.Adapter); err != nil {
				return fmt.Errorf("arm %q adapter: %w", a.ID, err)
			}
		}
		for _, digest := range a.LocalSkillDigests {
			if len(digest) != 64 {
				return fmt.Errorf("arm %q has an invalid local skill digest", a.ID)
			}
			if _, err := hex.DecodeString(digest); err != nil {
				return fmt.Errorf("arm %q has an invalid local skill digest", a.ID)
			}
		}
		if a.ID == "" || a.Harness == "" || a.Version == "" || a.Model == "" {
			return fmt.Errorf("every arm needs id, harness, harness_version, and model")
		}
		if seen[a.ID] {
			return fmt.Errorf("duplicate arm id %q", a.ID)
		}
		seen[a.ID] = true
	}
	declared := append([]string(nil), m.VariedAxes...)
	sort.Strings(declared)
	for i, axis := range declared {
		if !containsAxis(axis) {
			return fmt.Errorf("unknown varied axis %q", axis)
		}
		if i > 0 && declared[i-1] == axis {
			return fmt.Errorf("duplicate varied axis %q", axis)
		}
	}
	differing := m.DifferingAxes()
	sort.Strings(differing)
	if strings.Join(declared, "\x00") != strings.Join(differing, "\x00") {
		return fmt.Errorf("varied_axes must exactly disclose the differing axes: %s", strings.Join(differing, ", "))
	}
	for _, value := range []*float64{m.Budget.MaxUSDPerRun, m.Budget.MaxUSDTotal} {
		if value != nil && (*value <= 0 || math.IsNaN(*value) || math.IsInf(*value, 0) || math.Abs(*value*1_000_000-math.Round(*value*1_000_000)) > 1e-8) {
			return fmt.Errorf("dollar budgets must be positive and finite")
		}
	}
	if m.Budget.MaxTokens != nil && *m.Budget.MaxTokens <= 0 {
		return fmt.Errorf("token budget must be positive")
	}
	return nil
}

// IsPrivate reports whether this contract intentionally contains local or
// otherwise non-public evidence. Private studies can be run and reported
// locally, but must never be sent to the public Study endpoint.
func (m Manifest) IsPrivate() bool { return m.Private || m.Visibility == "private" }

func containsAxis(want string) bool { return slices.Contains(axes, want) }

func (m Manifest) DifferingAxes() []string {
	if len(m.Arms) < 2 {
		return nil
	}
	base := armValues(m.Arms[0])
	var out []string
	for _, axis := range axes {
		for _, arm := range m.Arms[1:] {
			if armValues(arm)[axis] != base[axis] {
				out = append(out, axis)
				break
			}
		}
	}
	return out
}

func armValues(a Arm) map[string]string {
	join := func(v []string) string {
		c := append([]string(nil), v...)
		sort.Strings(c)
		return strings.Join(c, "\x00")
	}
	skills := append(append([]string(nil), a.Skills...), a.LocalSkillDigests...)
	jsonValue := func(value any) string { raw, _ := json.Marshal(value); return string(raw) }
	return map[string]string{"harness": a.Harness, "harness_version": a.Version, "provider": a.Provider, "model": a.Model, "model_version": a.ModelVersion, "mode": a.Mode, "config": a.ConfigStatus + "\x00" + a.ConfigDigest, "reasoning": a.Reasoning, "workflow": a.Workflow, "skills": join(skills), "extensions": join(a.Extensions), "plugins": join(a.Plugins), "tools": join(a.Tools), "subagent_topology": a.Subagents, "environment": a.Environment, "network": a.Network, "prompt_treatment": jsonValue(a.PromptTreatment), "adapter": jsonValue(a.Adapter), "assurance": jsonValue(a.Assurance)}
}

func (m Manifest) Digest() string {
	copy := m
	copy.Arms = append([]Arm(nil), m.Arms...)
	for i := range copy.Arms {
		// Source paths are operational inputs, not study identity. Content
		// digests remain in the contract and therefore freeze the arm.
		copy.Arms[i].LocalSkills = nil
	}
	structured, _ := json.Marshal(copy)
	var canonical map[string]any
	_ = json.Unmarshal(structured, &canonical)
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(canonical)
	raw := bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

const MaxPublicTaskBytes = 96 * 1024

var (
	publicTaskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,199}$`)
	gitCommitPattern    = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	localPathPattern    = regexp.MustCompile(`(?i)(^|[[:space:]"'=:(])(?:~[/\\]|/Users/|/home/|[A-Z]:\\)`)
	secretValuePattern  = regexp.MustCompile(`(?i)(api[_ -]?key|secret|token|password)[[:space:]"']*[:=][[:space:]"']*[A-Za-z0-9_./+@=-]{12,}|bearer[[:space:]]+[A-Za-z0-9._~+/-]{12,}|(sk|ghp|github_pat|xox[baprs])[-_][A-Za-z0-9_-]{12,}|\$\{?[A-Z0-9_]*(TOKEN|SECRET|PASSWORD|API_KEY)\}?`)
)

// ValidatePublicTask applies the public hb.task.v1 contract shared with Rails.
// The task is deliberately repository-backed and excludes environment data,
// machine paths, secrets, and arbitrary nested extension fields. Bounded
// public evaluator artifacts may be embedded by digest.
func ValidatePublicTask(task map[string]any) error {
	raw, err := canonicalTaskJSON(task)
	if err != nil {
		return err
	}
	if len(raw) > MaxPublicTaskBytes {
		return fmt.Errorf("exceeds %d bytes", MaxPublicTaskBytes)
	}
	allowed := map[string]bool{"schema": true, "id": true, "type": true, "title": true, "description": true, "prompt": true, "language": true, "tags": true, "difficulty": true, "repo": true, "acceptance": true, "requirements": true, "fetches": true, "artifacts": true}
	for key := range task {
		if !allowed[key] {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	if task["schema"] != "hb.task.v1" {
		return fmt.Errorf("schema must be hb.task.v1")
	}
	id, _ := task["id"].(string)
	if !publicTaskIDPattern.MatchString(id) {
		return fmt.Errorf("id is required and invalid")
	}
	if err := validateTaskText("type", task["type"], 1, 64); err != nil {
		return err
	}
	if err := validateTaskText("title", task["title"], 1, 200); err != nil {
		return err
	}
	if err := validateTaskText("description", task["description"], 0, 4000); err != nil {
		return err
	}
	if err := validateTaskText("prompt", task["prompt"], 1, 32768); err != nil {
		return err
	}
	if err := validateTaskText("language", task["language"], 0, 64); err != nil {
		return err
	}
	if err := validateTaskText("difficulty", task["difficulty"], 0, 64); err != nil {
		return err
	}
	if err := validateTaskStringList("tags", task["tags"], 32, 64, false); err != nil {
		return err
	}
	if err := validateTaskRepo(task["repo"]); err != nil {
		return err
	}
	if err := validateTaskAcceptance(task["acceptance"]); err != nil {
		return err
	}
	if err := validateTaskRequirements(task["requirements"]); err != nil {
		return err
	}
	if err := validateTaskFetches(task["fetches"]); err != nil {
		return err
	}
	return validateTaskArtifacts(task["artifacts"])
}

func TaskDigest(task map[string]any) (string, error) {
	if err := ValidatePublicTask(task); err != nil {
		return "", err
	}
	raw, err := canonicalTaskJSON(task)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalTaskJSON(task map[string]any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(task); err != nil {
		return nil, fmt.Errorf("encode task: %w", err)
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func validatePublicDescriptor(value map[string]any, depth int) error {
	if depth > 3 || len(value) > 16 {
		return fmt.Errorf("descriptor is too deeply nested or has too many fields")
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 16*1024 {
		return fmt.Errorf("descriptor exceeds 16384 bytes")
	}
	keyPattern := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
	for key, item := range value {
		if !keyPattern.MatchString(key) {
			return fmt.Errorf("invalid field %q", key)
		}
		switch typed := item.(type) {
		case string:
			if err := validateTaskText(key, typed, 0, 4096); err != nil {
				return err
			}
		case bool, int, int64, float64:
		case []any:
			if len(typed) > 32 {
				return fmt.Errorf("field %q has too many values", key)
			}
			for _, child := range typed {
				if err := validateTaskText(key, child, 0, 4096); err != nil {
					return err
				}
			}
		case map[string]any:
			if err := validatePublicDescriptor(typed, depth+1); err != nil {
				return err
			}
		default:
			return fmt.Errorf("field %q has unsupported value", key)
		}
	}
	return nil
}

func validateTaskText(key string, value any, minimum, maximum int) error {
	text, ok := value.(string)
	if !ok || len(text) < minimum || len(text) > maximum || strings.ContainsRune(text, '\x00') {
		return fmt.Errorf("%s must be a string of %d to %d bytes", key, minimum, maximum)
	}
	if localPathPattern.MatchString(text) || secretValuePattern.MatchString(text) {
		return fmt.Errorf("%s contains private path or secret-like data", key)
	}
	return nil
}

func validateTaskStringList(key string, value any, maximumItems, maximumBytes int, required bool) error {
	if value == nil && !required {
		value = []any{}
	}
	items, ok := value.([]any)
	if !ok {
		// Maps assembled by Go code can contain []string before JSON/YAML round-trip.
		if stringsValue, stringsOK := value.([]string); stringsOK {
			items = make([]any, len(stringsValue))
			for i := range stringsValue {
				items[i] = stringsValue[i]
			}
			ok = true
		}
	}
	if !ok || len(items) > maximumItems || required && len(items) == 0 {
		return fmt.Errorf("%s must be a bounded array of strings", key)
	}
	for _, item := range items {
		if err := validateTaskText(key, item, 0, maximumBytes); err != nil {
			return err
		}
	}
	return nil
}

func validateTaskRepo(value any) error {
	repo, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("repo must be an object")
	}
	allowed := map[string]bool{"url": true, "base_ref": true, "gold_ref": true}
	for key := range repo {
		if !allowed[key] {
			return fmt.Errorf("unknown repo field %q", key)
		}
	}
	rawURL, _ := repo["url"].(string)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("repo.url must be a public HTTPS URL")
	}
	host := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified()) {
		return fmt.Errorf("repo.url must be public")
	}
	baseRef, _ := repo["base_ref"].(string)
	if !gitCommitPattern.MatchString(baseRef) {
		return fmt.Errorf("repo.base_ref must be a 40-character commit")
	}
	if gold, present := repo["gold_ref"]; present {
		goldRef, ok := gold.(string)
		if !ok || goldRef != "" && !gitCommitPattern.MatchString(goldRef) {
			return fmt.Errorf("repo.gold_ref must be a 40-character commit")
		}
	}
	return nil
}

func validateTaskAcceptance(value any) error {
	acceptance, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("acceptance must be an object")
	}
	allowed := map[string]bool{"setup_commands": true, "test_commands": true, "build_commands": true, "fail_to_pass": true, "gold_files": true}
	for key := range acceptance {
		if !allowed[key] {
			return fmt.Errorf("unknown acceptance field %q", key)
		}
	}
	for _, key := range []string{"setup_commands", "test_commands", "build_commands", "fail_to_pass", "gold_files"} {
		value, present := acceptance[key]
		if !present {
			value = []any{}
		}
		maximumBytes := 2048
		if key == "gold_files" {
			maximumBytes = 256
		}
		if err := validateTaskStringList(key, value, 32, maximumBytes, key == "test_commands"); err != nil {
			return err
		}
	}
	return nil
}

func validateTaskRequirements(value any) error {
	if value == nil {
		return nil
	}
	requirements, ok := value.(map[string]any)
	if !ok || len(requirements) != 1 {
		return fmt.Errorf("requirements must contain only commands")
	}
	commands, ok := requirements["commands"].([]any)
	if !ok || len(commands) > 32 {
		return fmt.Errorf("requirements.commands must be a bounded array")
	}
	for _, raw := range commands {
		command, ok := raw.(map[string]any)
		if !ok || len(command) > 3 {
			return fmt.Errorf("requirement commands must be objects")
		}
		for key := range command {
			if key != "name" && key != "minimum_version" && key != "purpose" {
				return fmt.Errorf("unknown requirement field %q", key)
			}
		}
		if err := validateTaskText("requirement name", command["name"], 1, 64); err != nil {
			return err
		}
		if err := validateTaskText("requirement purpose", command["purpose"], 1, 512); err != nil {
			return err
		}
		if minimum, present := command["minimum_version"]; present {
			if err := validateTaskText("requirement minimum_version", minimum, 0, 64); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateTaskFetches(value any) error {
	if value == nil {
		return nil
	}
	fetches, ok := value.([]any)
	if !ok || len(fetches) > 16 {
		return fmt.Errorf("fetches must be a bounded array")
	}
	for _, raw := range fetches {
		fetch, ok := raw.(map[string]any)
		if !ok || len(fetch) > 4 {
			return fmt.Errorf("fetches must contain objects")
		}
		for key := range fetch {
			if key != "kind" && key != "lockfile" && key != "source_lockfile" && key != "reason" {
				return fmt.Errorf("unknown fetch field %q", key)
			}
		}
		kind, _ := fetch["kind"].(string)
		if kind != "cargo" && kind != "npm" && kind != "uv" && kind != "bundler" {
			return fmt.Errorf("fetch kind is unsupported")
		}
		if err := validateRelativeTaskPath("fetch lockfile", fetch["lockfile"]); err != nil {
			return err
		}
		if source, present := fetch["source_lockfile"]; present {
			if err := validateRelativeTaskPath("fetch source_lockfile", source); err != nil {
				return err
			}
		}
		if reason, present := fetch["reason"]; present {
			if err := validateTaskText("fetch reason", reason, 0, 512); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateTaskArtifacts(value any) error {
	if value == nil {
		return nil
	}
	artifacts, ok := value.([]any)
	if !ok || len(artifacts) > 32 {
		return fmt.Errorf("artifacts must be a bounded array")
	}
	for _, raw := range artifacts {
		artifact, ok := raw.(map[string]any)
		if !ok || len(artifact) != 3 {
			return fmt.Errorf("artifacts must contain path, sha256, and content_base64")
		}
		for key := range artifact {
			if key != "path" && key != "sha256" && key != "content_base64" {
				return fmt.Errorf("unknown artifact field %q", key)
			}
		}
		if err := validateRelativeTaskPath("artifact path", artifact["path"]); err != nil {
			return err
		}
		digest, _ := artifact["sha256"].(string)
		if len(digest) != 64 {
			return fmt.Errorf("artifact sha256 is invalid")
		}
		content, ok := artifact["content_base64"].(string)
		decoded, err := base64.StdEncoding.DecodeString(content)
		if !ok || err != nil || len(decoded) > 64*1024 {
			return fmt.Errorf("artifact content is invalid or too large")
		}
		if localPathPattern.Match(decoded) || secretValuePattern.Match(decoded) {
			return fmt.Errorf("artifact content contains private path or secret-like data")
		}
		sum := sha256.Sum256(decoded)
		if hex.EncodeToString(sum[:]) != digest {
			return fmt.Errorf("artifact sha256 does not match content")
		}
	}
	return nil
}

func validateRelativeTaskPath(key string, value any) error {
	valuePath, ok := value.(string)
	clean := path.Clean(valuePath)
	if !ok || valuePath == "" || len(valuePath) > 256 || strings.HasPrefix(valuePath, "/") || strings.Contains(valuePath, "\\") || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != valuePath || strings.ContainsRune(valuePath, '\x00') {
		return fmt.Errorf("%s must be a clean relative path", key)
	}
	return nil
}

func (m Manifest) RunCount() int { return len(m.Scenarios) * len(m.Arms) * m.Repeats }
