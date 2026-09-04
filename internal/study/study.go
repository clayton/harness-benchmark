package study

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const Schema = "hb.study.v1"
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
	ID     string `yaml:"id" json:"id"`
	Digest string `yaml:"digest,omitempty" json:"digest,omitempty"`
}
type Budget struct {
	MaxUSDPerRun *float64 `yaml:"max_usd_per_run,omitempty" json:"max_usd_per_run,omitempty"`
	MaxUSDTotal  *float64 `yaml:"max_usd_total,omitempty" json:"max_usd_total,omitempty"`
	MaxTokens    *int     `yaml:"max_tokens_per_run,omitempty" json:"max_tokens_per_run,omitempty"`
	MaxMinutes   int      `yaml:"max_minutes_per_run" json:"max_minutes_per_run"`
}
type Arm struct {
	ID                string   `yaml:"id" json:"id"`
	Mode              string   `yaml:"mode,omitempty" json:"mode,omitempty"`
	LocalSkills       []string `yaml:"local_skill_dirs,omitempty" json:"local_skill_dirs,omitempty"`
	LocalSkillDigests []string `yaml:"local_skill_digests,omitempty" json:"local_skill_digests,omitempty"`
	ConfigDigest      string   `yaml:"config_sha256,omitempty" json:"config_sha256,omitempty"`
	ConfigStatus      string   `yaml:"config_status,omitempty" json:"config_status,omitempty"`
	Harness           string   `yaml:"harness" json:"harness"`
	Version           string   `yaml:"harness_version,omitempty" json:"harness_version,omitempty"`
	Provider          string   `yaml:"provider,omitempty" json:"provider,omitempty"`
	Model             string   `yaml:"model" json:"model"`
	Reasoning         string   `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
	Workflow          string   `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	Skills            []string `yaml:"skills,omitempty" json:"skills,omitempty"`
	Extensions        []string `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	Plugins           []string `yaml:"plugins,omitempty" json:"plugins,omitempty"`
	Tools             []string `yaml:"tools,omitempty" json:"tools,omitempty"`
	Subagents         string   `yaml:"subagent_topology,omitempty" json:"subagent_topology,omitempty"`
	Environment       string   `yaml:"environment,omitempty" json:"environment,omitempty"`
	Network           string   `yaml:"network,omitempty" json:"network,omitempty"`
}

var axes = []string{"harness", "harness_version", "provider", "model", "mode", "config", "reasoning", "workflow", "skills", "extensions", "plugins", "tools", "subagent_topology", "environment", "network"}
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
	if m.Schema != Schema {
		return fmt.Errorf("schema must be %s", Schema)
	}
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
		if !m.IsPrivate() && !publishedScenarioID.MatchString(scenario.ID) {
			return fmt.Errorf("scenario %q is not publishable; studies require rodeo:slug@version IDs", scenario.ID)
		}
		if len(scenario.Digest) != 64 {
			return fmt.Errorf("scenario %q needs a 64-character digest", scenario.ID)
		}
		if _, err := hex.DecodeString(scenario.Digest); err != nil {
			return fmt.Errorf("scenario %q has an invalid digest", scenario.ID)
		}
		scenarioSeen[scenario.ID] = true
	}
	for _, a := range m.Arms {
		if (a.Mode != "" || len(a.LocalSkills) > 0 || len(a.LocalSkillDigests) > 0 || a.ConfigDigest != "" || a.ConfigStatus != "") && !m.IsPrivate() {
			return fmt.Errorf("arm %q personal setup fields are only allowed in private studies", a.ID)
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
		if len(a.LocalSkills) != len(a.LocalSkillDigests) {
			return fmt.Errorf("arm %q local skill paths and digests must match", a.ID)
		}
		if len(a.LocalSkills) > 0 && a.Mode != "personal" {
			return fmt.Errorf("arm %q local skills require personal mode", a.ID)
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

func containsAxis(want string) bool {
	for _, axis := range axes {
		if axis == want {
			return true
		}
	}
	return false
}

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
	return map[string]string{"harness": a.Harness, "harness_version": a.Version, "provider": a.Provider, "model": a.Model, "mode": a.Mode, "config": a.ConfigStatus + "\x00" + a.ConfigDigest, "reasoning": a.Reasoning, "workflow": a.Workflow, "skills": join(skills), "extensions": join(a.Extensions), "plugins": join(a.Plugins), "tools": join(a.Tools), "subagent_topology": a.Subagents, "environment": a.Environment, "network": a.Network}
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

func (m Manifest) RunCount() int { return len(m.Scenarios) * len(m.Arms) * m.Repeats }
