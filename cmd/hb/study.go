package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/clayton/harness-benchmark/internal/adapter"
	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/fetchconsent"
	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
	"github.com/clayton/harness-benchmark/internal/publish"
	"github.com/clayton/harness-benchmark/internal/report"
	studycontract "github.com/clayton/harness-benchmark/internal/study"
	"gopkg.in/yaml.v3"
)

type studyState struct {
	Schema    string      `json:"schema"`
	StudyID   string      `json:"study_id"`
	Digest    string      `json:"digest"`
	Completed []studyCell `json:"completed"`
	Pending   *studyCell  `json:"pending,omitempty"`
}
type studyCell struct {
	Arm      string `json:"arm"`
	Scenario string `json:"scenario"`
	Repeat   int    `json:"repeat"`
	RunID    string `json:"run_id"`
}

type studyLocalInputs struct {
	Schema      string              `json:"schema"`
	StudyID     string              `json:"study_id"`
	Digest      string              `json:"digest"`
	LocalSkills map[string][]string `json:"local_skill_dirs"`
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			*s = append(*s, item)
		}
	}
	return nil
}

// initStudy creates a contract from saved setup profiles or explicit recipe
// flags. It resolves unversioned public scenario slugs to the latest runnable
// published version and digest, while retaining local paths only for private
// studies.
func initStudy(args []string) error {
	fs := flag.NewFlagSet("study init", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	question := fs.String("question", "", "testable comparison question")
	outPath := fs.String("out", "study.yaml", "output manifest path")
	mode := fs.String("mode", "controlled", "controlled or ecological")
	visibility := fs.String("visibility", "public", "public or private")
	privateStudy := fs.Bool("private", false, "allow local scenario paths; never publish this study")
	harness := fs.String("harness", "pi", "harness for explicit model recipes")
	provider := fs.String("provider", "", "provider for explicit model recipes")
	reasoning := fs.String("reasoning", "", "reasoning level for explicit model recipes")
	workflow := fs.String("workflow", "baseline", "workflow for explicit model recipes")
	repeats := fs.Int("repeats", 1, "repeats per arm/scenario")
	seed := fs.Int64("seed", time.Now().Unix(), "randomized cell order seed")
	judge := fs.String("judge-protocol", "scenario-default", "judge protocol")
	maxMinutes := fs.Int("max-minutes", 45, "maximum minutes per run")
	maxUSDRun := fs.Float64("max-usd-per-run", 0, "optional maximum USD per run")
	maxUSDTotal := fs.Float64("max-usd-total", 0, "optional maximum USD for the study")
	var scenarioRefs, setupRefs, models, skillsOn stringList
	fs.Var(&scenarioRefs, "scenario", "scenario ID, rodeo:slug[@version], or local YAML path (repeatable)")
	fs.Var(&setupRefs, "setup", "saved setup profile ID or YAML path (repeatable)")
	fs.Var(&models, "model", "model override (repeatable; creates model-only arms)")
	fs.Var(&models, "models", "comma-separated model overrides (alias for --model)")
	fs.Var(&skillsOn, "skill", "skill treatment (repeatable; creates skill off/on arms)")
	fs.Var(&skillsOn, "skill-on", "skill treatment (alias for --skill)")
	fs.Var(&skillsOn, "skills-on", "comma-separated skill treatment (alias for --skill)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *question == "" || len(scenarioRefs) == 0 {
		return fmt.Errorf("usage: hbench study init --question TEXT --scenario ID [--setup PROFILE | --model MODEL] [--private]")
	}
	if *mode != "controlled" && *mode != "ecological" {
		return fmt.Errorf("--mode must be controlled or ecological")
	}
	if *visibility != "public" && *visibility != "private" {
		return fmt.Errorf("--visibility must be public or private")
	}
	if *privateStudy {
		*visibility = "private"
	}
	if _, err := os.Stat(*outPath); err == nil {
		return fmt.Errorf("refusing to overwrite %s", *outPath)
	} else if !os.IsNotExist(err) {
		return err
	}

	profiles := make([]loop.Profile, 0, len(setupRefs))
	for _, ref := range setupRefs {
		profile, err := loadStudyProfile(ref)
		if err != nil {
			return err
		}
		profiles = append(profiles, profile)
	}
	if len(profiles) == 0 {
		profiles = append(profiles, loop.Profile{ID: "base", Harness: *harness, Provider: *provider, Reasoning: *reasoning, Workflow: *workflow})
	}
	profiles = recipeProfiles(profiles, models, skillsOn)
	if len(profiles) < 2 {
		return fmt.Errorf("study init needs at least two arms; provide multiple --setup/--model values or --skill")
	}
	if err := ensureCorpus(layout()); err != nil {
		return err
	}

	scenarios := make([]studycontract.Scenario, 0, len(scenarioRefs))
	for _, ref := range scenarioRefs {
		scenario, err := resolveStudyInitScenario(ref)
		if err != nil {
			return err
		}
		id := scenario.ID
		digest := scenario.ManifestDigest
		var task map[string]any
		switch {
		case strings.HasPrefix(ref, "rodeo:"):
			if digest == "" {
				return fmt.Errorf("published scenario %s did not include a manifest digest", id)
			}
			id = "rodeo:" + id
		case *visibility == "public":
			task, err = publicScenarioTask(scenario)
			if err != nil {
				return fmt.Errorf("public task %s: %w", scenario.ID, err)
			}
			digest, err = studycontract.TaskDigest(task)
			if err != nil {
				return fmt.Errorf("public task %s: %w", scenario.ID, err)
			}
			id = "local:" + digest[:16]
		default:
			// Private contracts retain the local path needed to execute local
			// files. Public contracts embed a path-free hb.task.v1 object above.
			id = filepath.Clean(ref)
			if digest == "" {
				digest, err = corpus.TrustDigest(scenario)
				if err != nil {
					return err
				}
			}
		}
		scenarios = append(scenarios, studycontract.Scenario{ID: id, Digest: digest, Task: task})
	}

	budget := studycontract.Budget{MaxMinutes: *maxMinutes}
	if *maxUSDRun > 0 {
		budget.MaxUSDPerRun = maxUSDRun
	}
	if *maxUSDTotal > 0 {
		budget.MaxUSDTotal = maxUSDTotal
	}
	manifestVisibility := ""
	if *visibility == "private" {
		manifestVisibility = "private"
	}
	schema := studycontract.Schema
	if *visibility == "public" {
		for _, scenario := range scenarios {
			if len(scenario.Task) > 0 {
				schema = studycontract.SchemaV2
				break
			}
		}
	}
	manifest := studycontract.Manifest{Schema: schema, ID: filepath.Base(*outPath), Question: *question, ComparisonMode: *mode, Visibility: manifestVisibility, Scenarios: scenarios, Repeats: *repeats, Seed: *seed, JudgeProtocol: *judge, WinRule: studycontract.WinRule, Budget: budget}
	for i, profile := range profiles {
		preparedProfile, prepareErr := prepareStudyAdapter(profile)
		if prepareErr != nil {
			return prepareErr
		}
		profile = preparedProfile
		if profile.HarnessVersion == "" {
			if profile.Harness == "manual" {
				profile.HarnessVersion = "human"
			} else {
				profile.HarnessVersion = loop.DetectHarnessVersion(profile.Harness)
				if profile.HarnessVersion == "" {
					return fmt.Errorf("could not resolve %s harness version; save harness_version in the setup profile", profile.Harness)
				}
			}
		}
		arm, err := profileToStudyArm(profile, i)
		if err != nil {
			return err
		}
		manifest.Arms = append(manifest.Arms, arm)
		if *visibility == "public" && (arm.Mode != "" || len(arm.LocalSkillDigests) > 0 || arm.ModelVersion != "" || len(arm.PromptTreatment) > 0 || len(arm.Adapter) > 0 || len(arm.Assurance) > 0) {
			manifest.Schema = studycontract.SchemaV2
		}
	}
	manifest.VariedAxes = manifest.DifferingAxes()
	if err := manifest.Validate(); err != nil {
		return err
	}
	if *visibility == "public" {
		if err := saveStudyLocalInputs(manifest); err != nil {
			return err
		}
		manifest = publicStudyManifest(manifest)
		if err := manifest.Validate(); err != nil {
			return err
		}
	}
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*outPath, raw, 0o644); err != nil {
		return err
	}
	fmt.Printf("Wrote %s\n", *outPath)
	return printStudyPlan(manifest)
}

func publicScenarioTask(s corpus.Scenario) (map[string]any, error) {
	stringList := func(items []string) []string {
		if items == nil {
			return []string{}
		}
		return items
	}
	value := map[string]any{
		"schema": "hb.task.v1", "id": s.ID, "type": s.Type, "title": s.Title,
		"description": s.Description, "prompt": s.Prompt, "language": s.Language,
		"tags": stringList(s.Tags), "difficulty": s.Difficulty,
		"repo": map[string]any{"url": s.Repo.URL, "base_ref": s.Repo.BaseRef, "gold_ref": s.Repo.GoldRef},
		"acceptance": map[string]any{
			"setup_commands": stringList(s.Acceptance.SetupCommands), "test_commands": stringList(s.Acceptance.TestCommands),
			"build_commands": stringList(s.Acceptance.BuildCommands), "fail_to_pass": stringList(s.Acceptance.FailToPass),
		},
	}
	// Normalize concrete slices and structs to the same JSON value types read
	// from a published contract before applying the shared validator.
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var task map[string]any
	if err := json.Unmarshal(raw, &task); err != nil {
		return nil, err
	}
	if err := studycontract.ValidatePublicTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

func loadStudyProfile(ref string) (loop.Profile, error) {
	if setup, err := loop.LoadSetup(layout(), ref); err == nil {
		return setup.Profile, nil
	}
	path := ref
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		candidates := []string{
			filepath.Join(layout().DataDir, "setups", ref+".json"),
			filepath.Join(layout().DataDir, "setups", ref+".yaml"),
			filepath.Join(layout().DataDir, "setups", ref+".yml"),
			filepath.Join("setups", ref+".yaml"),
			filepath.Join("configs", ref+".yaml"),
		}
		for _, candidate := range candidates {
			if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
				path = candidate
				break
			}
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return loop.Profile{}, fmt.Errorf("load setup profile %q: %w", ref, err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return loop.Profile{}, fmt.Errorf("parse setup profile %q: %w", ref, err)
	}
	if nested, ok := document["profile"].(map[string]any); ok {
		document = nested
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return loop.Profile{}, err
	}
	var profile loop.Profile
	if err := json.Unmarshal(encoded, &profile); err != nil {
		return loop.Profile{}, fmt.Errorf("decode setup profile %q: %w", ref, err)
	}
	if profile.ID == "" {
		profile.ID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if profile.Workflow == "" {
		profile.Workflow = "baseline"
	}
	return profile, nil
}

func recipeProfiles(base []loop.Profile, models, skillsOn []string) []loop.Profile {
	var out []loop.Profile
	for _, original := range base {
		candidates := models
		if len(candidates) == 0 {
			candidates = []string{original.Model}
		}
		for modelIndex, model := range candidates {
			p := original
			if model != "" {
				p.Model = model
			}
			if len(candidates) > 1 {
				label := regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(model, "-")
				p.ID = original.ID + "-" + label
				if label == "" {
					p.ID = fmt.Sprintf("%s-model-%d", original.ID, modelIndex+1)
				}
			}
			if len(skillsOn) > 0 {
				off := p
				off.Skills = nil
				off.ID = p.ID + "-skills-off"
				on := p
				on.Skills = append([]string(nil), skillsOn...)
				on.ID = p.ID + "-skills-on"
				out = append(out, off, on)
			} else {
				out = append(out, p)
			}
		}
	}
	return out
}

func prepareStudyAdapter(profile loop.Profile) (loop.Profile, error) {
	if profile.AdapterManifest == "" {
		return profile, nil
	}
	manifest, digest, err := adapter.Load(profile.AdapterManifest)
	if err != nil {
		return loop.Profile{}, err
	}
	raw, _ := json.Marshal(manifest)
	if err := json.Unmarshal(raw, &profile.Adapter); err != nil {
		return loop.Profile{}, err
	}
	profile.AdapterDigest = digest
	if profile.Harness == "" {
		profile.Harness = "adapter:" + manifest.ID
	}
	if profile.HarnessVersion == "" {
		profile.HarnessVersion = "adapter/" + digest[:12]
	}
	return profile, nil
}

func profileToStudyArm(p loop.Profile, index int) (studycontract.Arm, error) {
	id := p.ID
	if id == "" {
		id = fmt.Sprintf("arm-%d", index+1)
	}
	if len(p.LocalSkills) > 0 && p.Harness != "pi" {
		return studycontract.Arm{}, fmt.Errorf("local skill directories are currently supported by the pi adapter only")
	}
	if len(p.LocalSkills) > 0 && p.Mode != "personal" {
		return studycontract.Arm{}, fmt.Errorf("local skill directories require personal mode")
	}
	mode := ""
	configDigest, configStatus := "", ""
	if p.Mode == "personal" {
		if p.Harness == "codex" {
			var err error
			configDigest, configStatus, err = loop.CodexConfigFingerprint()
			if err != nil {
				return studycontract.Arm{}, err
			}
		}
		mode = p.Mode
	}
	digests := make([]string, 0, len(p.LocalSkills))
	for _, path := range p.LocalSkills {
		digest, err := loop.LocalSkillDigest(path)
		if err != nil {
			return studycontract.Arm{}, err
		}
		digests = append(digests, digest)
	}
	return studycontract.Arm{ID: id, Mode: mode, LocalSkills: p.LocalSkills, LocalSkillDigests: digests, ConfigDigest: configDigest, ConfigStatus: configStatus, Harness: p.Harness, Version: p.HarnessVersion, Provider: p.Provider, Model: p.Model, Reasoning: p.Reasoning, Workflow: p.Workflow, Skills: p.Skills, Extensions: p.Extensions, Plugins: p.Plugins, Tools: p.Tools, Subagents: p.Subagents, Environment: p.Environment, Network: p.Network, ModelVersion: p.ModelVersion, PromptTreatment: p.PromptTreatment, Adapter: p.Adapter, Assurance: p.Assurance}, nil
}

func resolveStudyInitScenario(ref string) (corpus.Scenario, error) {
	if !strings.HasPrefix(ref, "rodeo:") {
		return corpus.Resolve(layout().ScenariosDir(), "", ref)
	}
	identifier := strings.TrimPrefix(ref, "rodeo:")
	if !strings.Contains(identifier, "@") {
		latest, err := latestPublishedScenario(identifier)
		if err != nil {
			return corpus.Scenario{}, err
		}
		identifier = latest
	}
	return corpus.FetchRodeo(layout().ScenariosDir(), identifier, nil)
}

func latestPublishedScenario(slug string) (string, error) {
	origin, err := publish.ValidatedRodeoURL()
	if err != nil {
		return "", err
	}
	request, err := http.NewRequest(http.MethodGet, origin+"/api/v1/scenarios", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch published scenarios: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch published scenarios: HTTP %d", response.StatusCode)
	}
	var entries []struct {
		Slug    string `json:"slug"`
		Version int    `json:"version"`
		Status  string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&entries); err != nil {
		return "", err
	}
	best := 0
	for _, entry := range entries {
		if entry.Slug == slug && entry.Version > best && entry.Status != "retired" && entry.Status != "superseded" {
			best = entry.Version
		}
	}
	if best == 0 {
		return "", fmt.Errorf("published scenario %q was not found", slug)
	}
	return fmt.Sprintf("%s@%d", slug, best), nil
}

func cmdStudy(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hbench study init|validate|plan|run|status|report|publish STUDY.yaml")
	}
	action := args[0]
	if action == "init" {
		return initStudy(args[1:])
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: hbench study init|validate|plan|run|status|report|publish STUDY.yaml")
	}
	path := args[1]
	actionArgs := args[2:]
	// Accept the conventional flag-before-positional form for publication
	// preview as well as the documented `publish STUDY.yaml --preview` form.
	if action == "publish" && path == "--preview" && len(args) >= 3 {
		path = args[2]
		actionArgs = append([]string{"--preview"}, args[3:]...)
	}
	if path == "--help" || path == "-h" {
		fmt.Println("usage: hbench study init|validate|plan|run|status|report|publish STUDY.yaml")
		return nil
	}
	m, err := studycontract.Load(path)
	if err != nil {
		return err
	}
	if action == "run" || action == "publish" {
		m, err = loadStudyLocalInputs(m)
		if err != nil {
			return err
		}
	}
	switch action {
	case "validate":
		label := "publishable"
		if m.IsPrivate() {
			label = "private, non-publishable"
		}
		fmt.Printf("Valid %s %s study %s\ncontract: %s\n", label, m.ComparisonMode, m.ID, m.Digest())
		return nil
	case "plan":
		return printStudyPlan(m)
	case "status":
		return printStudyStatus(m)
	case "report":
		return writeStudyReport(path, m)
	case "export":
		return exportStudy(m, actionArgs)
	case "run":
		return runStudy(m, actionArgs)
	case "publish":
		return publishStudy(path, m, actionArgs)
	default:
		return fmt.Errorf("unknown study command %q", action)
	}
}

func exportStudy(m studycontract.Manifest, args []string) error {
	format, out := "json", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return fmt.Errorf("--format requires json or csv")
			}
			format = args[i+1]
			i++
		case "--out":
			if i+1 >= len(args) {
				return fmt.Errorf("--out requires a path")
			}
			out = args[i+1]
			i++
		}
	}
	if format != "json" && format != "csv" {
		return fmt.Errorf("study export format must be json or csv")
	}
	s, err := loadStudyState(m)
	if err != nil {
		return err
	}
	type row struct {
		RunID, ArmID, ScenarioID string
		Repeat                   int
		Status                   string
		Quality                  float64
		Tokens                   *int
		Cost                     *float64
		WallMS                   int
	}
	rows := make([]row, 0, len(s.Completed))
	for _, cell := range s.Completed {
		record, loadErr := loop.Load(layout(), cell.RunID)
		if loadErr != nil {
			return loadErr
		}
		rows = append(rows, row{record.ID, cell.Arm, cell.Scenario, cell.Repeat, record.Status, loop.Quality(record), record.Telemetry.TotalTokens, record.Telemetry.EstimatedUSD, record.Telemetry.WallMS})
	}
	if out == "" {
		out = m.ID + "." + format
	}
	if format == "csv" {
		file, err := os.Create(out)
		if err != nil {
			return err
		}
		defer file.Close()
		w := csv.NewWriter(file)
		_ = w.Write([]string{"run_id", "arm_id", "scenario_id", "repeat", "status", "quality", "tokens", "estimated_usd", "wall_ms"})
		for _, r := range rows {
			_ = w.Write([]string{r.RunID, r.ArmID, r.ScenarioID, fmt.Sprint(r.Repeat), r.Status, fmt.Sprintf("%.4f", r.Quality), fmt.Sprint(r.Tokens), fmt.Sprint(r.Cost), fmt.Sprint(r.WallMS)})
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return err
		}
	} else {
		encoded, err := json.MarshalIndent(map[string]any{"schema": "hb.study.export.v1", "manifest": m, "runs": rows}, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(out, append(encoded, '\n'), 0o600); err != nil {
			return err
		}
	}
	fmt.Printf("Wrote %s (%d runs). Nothing was uploaded.\n", out, len(rows))
	return nil
}

func writeStudyReport(manifestPath string, m studycontract.Manifest) error {
	s, err := loadStudyState(m)
	if err != nil {
		return err
	}
	runIDs := make(map[string]bool, len(s.Completed))
	for _, cell := range s.Completed {
		runIDs[cell.RunID] = true
	}
	path, n, err := report.WriteStudyComparison(layout(), m, runIDs, manifestPath)
	if err != nil {
		return err
	}
	fmt.Printf("Wrote %s (%d study runs). Nothing was uploaded.\n", path, n)
	return nil
}

func printStudyPlan(m studycontract.Manifest) error {
	publishable := "yes"
	if m.IsPrivate() {
		publishable = "no (private; local report/preview only)"
	}
	fmt.Printf("%s\nmode: %s\npublishable: %s\ncontract: %s\n", m.Question, m.ComparisonMode, publishable, m.Digest())
	fmt.Printf("matrix: %d arms × %d scenarios × %d repeats = %d runs\n", len(m.Arms), len(m.Scenarios), m.Repeats, m.RunCount())
	fmt.Printf("changed axes: %s\n", strings.Join(m.DifferingAxes(), ", "))
	fmt.Printf("confounds: held constant except %s\n", strings.Join(m.DifferingAxes(), ", "))
	for _, a := range m.Arms {
		fmt.Printf("  %s: harness=%s@%s provider=%s model=%s reasoning=%s workflow=%s environment=%s network=%s skills=%s extensions=%s plugins=%s tools=%s subagents=%s\n",
			a.ID, a.Harness, a.Version, a.Provider, a.Model, a.Reasoning, a.Workflow, a.Environment, a.Network,
			strings.Join(a.Skills, ","), strings.Join(a.Extensions, ","), strings.Join(a.Plugins, ","), strings.Join(a.Tools, ","), a.Subagents)
	}
	if m.Budget.MaxUSDTotal != nil {
		fmt.Printf("total spend stop threshold: $%.2f (one run can overshoot)\n", *m.Budget.MaxUSDTotal)
	} else {
		fmt.Println("total spend stop threshold: not declared")
	}
	if m.Budget.MaxUSDPerRun != nil {
		fmt.Printf("per-run spend limit: $%.2f\n", *m.Budget.MaxUSDPerRun)
	}
	if m.Budget.MaxTokens != nil {
		fmt.Printf("per-run token limit: %d\n", *m.Budget.MaxTokens)
	}
	fmt.Printf("per-run timeout: %d minutes\n", m.Budget.MaxMinutes)
	return nil
}

func statePath(m studycontract.Manifest) string {
	return filepath.Join(layout().OutDir, "studies", safeStudyID(m.ID)+".json")
}

func studyLocalInputsPath(m studycontract.Manifest) string {
	return filepath.Join(layout().OutDir, "studies", safeStudyID(m.ID)+".inputs.json")
}

func saveStudyLocalInputs(m studycontract.Manifest) error {
	inputs := studyLocalInputs{Schema: "hb.study.inputs.v1", StudyID: m.ID, Digest: m.Digest(), LocalSkills: map[string][]string{}}
	for _, arm := range m.Arms {
		if len(arm.LocalSkills) > 0 {
			inputs.LocalSkills[arm.ID] = append([]string(nil), arm.LocalSkills...)
		}
	}
	if len(inputs.LocalSkills) == 0 {
		return nil
	}
	raw, err := json.MarshalIndent(inputs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(studyLocalInputsPath(m)), 0o700); err != nil {
		return err
	}
	return loop.WriteFileAtomic(studyLocalInputsPath(m), append(raw, '\n'), 0o600)
}

func loadStudyLocalInputs(m studycontract.Manifest) (studycontract.Manifest, error) {
	needsInputs := false
	for _, arm := range m.Arms {
		needsInputs = needsInputs || len(arm.LocalSkillDigests) > 0 && len(arm.LocalSkills) == 0
	}
	if !needsInputs {
		return m, nil
	}
	raw, err := os.ReadFile(studyLocalInputsPath(m))
	if err != nil {
		return studycontract.Manifest{}, fmt.Errorf("study uses local skill hashes but local path mapping is unavailable: %w", err)
	}
	var inputs studyLocalInputs
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return studycontract.Manifest{}, err
	}
	if inputs.Schema != "hb.study.inputs.v1" || inputs.StudyID != m.ID || inputs.Digest != m.Digest() {
		return studycontract.Manifest{}, fmt.Errorf("local study inputs do not match this contract")
	}
	m.Arms = append([]studycontract.Arm(nil), m.Arms...)
	for i := range m.Arms {
		m.Arms[i].LocalSkills = append([]string(nil), inputs.LocalSkills[m.Arms[i].ID]...)
	}
	if err := m.Validate(); err != nil {
		return studycontract.Manifest{}, err
	}
	return m, nil
}
func safeStudyID(id string) string {
	return regexp.MustCompile(`[^a-zA-Z0-9._-]+`).ReplaceAllString(id, "-")
}
func loadStudyState(m studycontract.Manifest) (studyState, error) {
	s := studyState{Schema: "hb.study.state.v1", StudyID: m.ID, Digest: m.Digest()}
	raw, err := os.ReadFile(statePath(m))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return studyState{}, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return studyState{}, fmt.Errorf("parse saved study state: %w", err)
	}
	if s.Schema != "hb.study.state.v1" || s.StudyID != m.ID || s.Digest != m.Digest() {
		return studyState{}, fmt.Errorf("saved state does not match this study contract")
	}
	return s, nil
}
func saveStudyState(m studycontract.Manifest, s studyState) error {
	if err := os.MkdirAll(filepath.Dir(statePath(m)), 0o755); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(s, "", "  ")
	return loop.WriteFileAtomic(statePath(m), append(raw, '\n'), 0o644)
}
func printStudyStatus(m studycontract.Manifest) error {
	s, err := loadStudyState(m)
	if err != nil {
		return err
	}
	fmt.Printf("study %s: %d/%d runs complete\n", m.ID, len(s.Completed), m.RunCount())
	for _, c := range s.Completed {
		fmt.Printf("  %s %s repeat %d: %s\n", c.Arm, c.Scenario, c.Repeat, c.RunID)
	}
	if s.Pending != nil {
		fmt.Printf("  pending %s %s repeat %d: %s\n", s.Pending.Arm, s.Pending.Scenario, s.Pending.Repeat, s.Pending.RunID)
	}
	return nil
}

func runStudy(m studycontract.Manifest, args []string) error {
	fs := flag.NewFlagSet("study run", flag.ContinueOnError)
	approve := fs.Bool("approve-spend", false, "confirm that agent runs may spend tokens and money")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*approve {
		printStudyPlan(m)
		return fmt.Errorf("review the plan, then rerun with --approve-spend")
	}
	if err := ensureCorpus(layout()); err != nil {
		return err
	}
	s, err := loadStudyState(m)
	if err != nil {
		return err
	}
	// A pending run already contains a frozen skill copy. Its source may have
	// changed after the run was created, so only new cells require source
	// verification here; Execute verifies the pending copy itself.
	if s.Pending == nil {
		if err := verifyStudyLocalSkills(m); err != nil {
			return err
		}
	}
	if err := verifyStudyScenarios(m); err != nil {
		return err
	}
	if err := authorizeStudyScenarios(m); err != nil {
		return err
	}
	if err := prepareStudyInputs(m); err != nil {
		return err
	}
	if err := verifyStudyHarnesses(m); err != nil {
		return err
	}
	if err := verifyStudyExecutionProfiles(m); err != nil {
		return err
	}
	if err := reconcilePendingStudyCell(m, &s); err != nil {
		return err
	}
	done := map[string]bool{}
	for _, c := range s.Completed {
		done[cellKey(c.Arm, c.Scenario, c.Repeat)] = true
	}
	if err := enforceStudyBudget(m, s); err != nil {
		return err
	}
	var cells []studyCell
	for _, sc := range m.Scenarios {
		for _, a := range m.Arms {
			for r := 1; r <= m.Repeats; r++ {
				c := studyCell{Arm: a.ID, Scenario: sc.ID, Repeat: r}
				cells = append(cells, c)
			}
		}
	}
	rand.New(rand.NewSource(m.Seed)).Shuffle(len(cells), func(i, j int) { cells[i], cells[j] = cells[j], cells[i] })
	for _, c := range cells {
		if done[cellKey(c.Arm, c.Scenario, c.Repeat)] {
			continue
		}
		arm := findArm(m, c.Arm)
		frozenScenario := findScenario(m, c.Scenario)
		l := layout()
		sc, err := resolveFrozenStudyScenario(l, frozenScenario)
		if err != nil {
			return err
		}
		if err := verifyStudyScenario(sc, frozenScenario); err != nil {
			return err
		}
		pending := s.Pending != nil && cellKey(s.Pending.Arm, s.Pending.Scenario, s.Pending.Repeat) == cellKey(c.Arm, c.Scenario, c.Repeat)
		if pending {
			c = *s.Pending
		} else {
			// Verify immediately before creating each run. A multi-cell study can
			// otherwise copy a changed local skill into later runs after the
			// invocation-level check has already passed.
			if err := verifyStudyArmLocalSkills(arm); err != nil {
				return err
			}
			profile := loop.Profile{ID: arm.ID, Mode: arm.Mode, Harness: arm.Harness, HarnessVersion: arm.Version, Provider: arm.Provider, Model: arm.Model, ModelVersion: arm.ModelVersion, PromptTreatment: arm.PromptTreatment, Adapter: arm.Adapter, Assurance: arm.Assurance, Task: frozenScenario.Task, Reasoning: arm.Reasoning, Workflow: arm.Workflow, Skills: arm.Skills, LocalSkills: arm.LocalSkills, ConfigDigest: arm.ConfigDigest, ConfigStatus: arm.ConfigStatus, Extensions: arm.Extensions, Plugins: arm.Plugins, Tools: arm.Tools, Subagents: arm.Subagents, Environment: arm.Environment, Network: arm.Network, JudgeProtocol: m.JudgeProtocol, Budget: studyBudgetDescriptor(m), StudyID: m.ID, ContractDigest: m.Digest(), ArmID: c.Arm, Repeat: c.Repeat, ScenarioDigest: frozenScenario.Digest, StudyScenarioID: c.Scenario}
			rec, err := loop.CreateRunWithProfile(layout(), sc, profile, true)
			if err != nil {
				return err
			}
			c.RunID = rec.ID
			s.Pending = &c
			if err := saveStudyState(m, s); err != nil {
				return err
			}
		}
		fmt.Printf("running %s / %s / repeat %d as %s\n", c.Arm, c.Scenario, c.Repeat, c.RunID)
		res, execErr := loop.Execute(layout(), c.RunID, time.Duration(m.Budget.MaxMinutes)*time.Minute)
		if res.LogPath == "" && execErr != nil {
			return execErr
		}
		finished, finishErr := loop.Finish(layout(), c.RunID, sc, res.WallMS, "hbench study run", true)
		if finishErr != nil {
			return finishErr
		}
		if reason := perRunBudgetViolation(m, finished); reason != "" {
			finished.Status = "budget_exceeded"
			finished.Error = reason
			if err := loop.Save(layout(), finished); err != nil {
				return err
			}
		}
		c.RunID = finished.ID
		s.Completed = append(s.Completed, c)
		s.Pending = nil
		if err := saveStudyState(m, s); err != nil {
			return err
		}
		if budgetErr := enforceStudyBudget(m, s); budgetErr != nil {
			if strings.Contains(budgetErr.Error(), "total dollar limit") {
				finished.Status = "budget_exceeded"
				finished.Error = budgetErr.Error()
				_ = loop.Save(layout(), finished)
			}
			return budgetErr
		}
		if execErr != nil && finished.Status != "timeout" {
			return execErr
		}
	}
	return printStudyStatus(m)
}

func verifyStudyLocalSkills(m studycontract.Manifest) error {
	for _, arm := range m.Arms {
		if err := verifyStudyArmLocalSkills(arm); err != nil {
			return err
		}
	}
	return nil
}

func verifyStudyArmLocalSkills(arm studycontract.Arm) error {
	if len(arm.LocalSkills) != len(arm.LocalSkillDigests) {
		return fmt.Errorf("arm %s local skill paths and digests do not match", arm.ID)
	}
	for i, path := range arm.LocalSkills {
		digest, err := loop.LocalSkillDigest(path)
		if err != nil {
			return fmt.Errorf("arm %s local skill %q: %w", arm.ID, path, err)
		}
		if digest != arm.LocalSkillDigests[i] {
			return fmt.Errorf("arm %s local skill drift at %q: contract has %s, current content is %s", arm.ID, path, arm.LocalSkillDigests[i], digest)
		}
	}
	if arm.Mode == "personal" && arm.Harness == "codex" {
		digest, status, err := loop.CodexConfigFingerprint()
		if err != nil {
			return err
		}
		if digest != arm.ConfigDigest || status != arm.ConfigStatus {
			return fmt.Errorf("arm %s personal Codex config drift: contract has %s/%s, current config is %s/%s", arm.ID, arm.ConfigStatus, arm.ConfigDigest, status, digest)
		}
	}
	return nil
}

func enforceStudyBudget(m studycontract.Manifest, s studyState) error {
	totalCost := 0.0
	for _, cell := range s.Completed {
		rec, err := loop.Load(layout(), cell.RunID)
		if err != nil {
			return err
		}
		if rec.Status == "budget_exceeded" {
			return fmt.Errorf("run %s ended with status %s", cell.RunID, rec.Status)
		}
		if m.Budget.MaxTokens != nil {
			if rec.Telemetry.TotalTokens == nil || rec.Telemetry.TokenComplete == nil || !*rec.Telemetry.TokenComplete {
				return fmt.Errorf("run %s has no complete token telemetry for its token limit", cell.RunID)
			}
			if *rec.Telemetry.TotalTokens > *m.Budget.MaxTokens {
				return fmt.Errorf("run %s exceeded the token limit", cell.RunID)
			}
		}
		if m.Budget.MaxUSDPerRun != nil {
			if rec.Telemetry.EstimatedUSD == nil {
				return fmt.Errorf("run %s has no cost estimate for its dollar limit", cell.RunID)
			}
			if *rec.Telemetry.EstimatedUSD > *m.Budget.MaxUSDPerRun {
				return fmt.Errorf("run %s exceeded the dollar limit", cell.RunID)
			}
		}
		if m.Budget.MaxUSDTotal != nil && rec.Telemetry.EstimatedUSD == nil {
			return fmt.Errorf("run %s has no cost estimate for the total dollar limit", cell.RunID)
		}
		if rec.Telemetry.EstimatedUSD != nil {
			totalCost += *rec.Telemetry.EstimatedUSD
		}
	}
	if m.Budget.MaxUSDTotal != nil && totalCost > *m.Budget.MaxUSDTotal {
		return fmt.Errorf("study exceeded the total dollar limit")
	}
	return nil
}

func cellKey(a, s string, r int) string { return fmt.Sprintf("%s\x00%s\x00%d", a, s, r) }
func findArm(m studycontract.Manifest, id string) studycontract.Arm {
	for _, a := range m.Arms {
		if a.ID == id {
			return a
		}
	}
	return studycontract.Arm{}
}

func findScenario(m studycontract.Manifest, id string) studycontract.Scenario {
	for _, scenario := range m.Scenarios {
		if scenario.ID == id {
			return scenario
		}
	}
	return studycontract.Scenario{}
}

func studyBudgetDescriptor(m studycontract.Manifest) map[string]any {
	budget := map[string]any{"max_minutes_per_run": m.Budget.MaxMinutes}
	if m.Budget.MaxTokens != nil {
		budget["max_tokens_per_run"] = *m.Budget.MaxTokens
	}
	if m.Budget.MaxUSDPerRun != nil {
		budget["max_usd_per_run"] = *m.Budget.MaxUSDPerRun
	}
	return budget
}

func perRunBudgetViolation(m studycontract.Manifest, rec loop.RunRecord) string {
	if m.Budget.MaxTokens != nil && rec.Telemetry.TotalTokens != nil && *rec.Telemetry.TotalTokens > *m.Budget.MaxTokens {
		return fmt.Sprintf("token limit exceeded: %d > %d", *rec.Telemetry.TotalTokens, *m.Budget.MaxTokens)
	}
	if m.Budget.MaxUSDPerRun != nil && rec.Telemetry.EstimatedUSD != nil && *rec.Telemetry.EstimatedUSD > *m.Budget.MaxUSDPerRun {
		return fmt.Sprintf("dollar limit exceeded: %.6f > %.6f", *rec.Telemetry.EstimatedUSD, *m.Budget.MaxUSDPerRun)
	}
	return ""
}

func reconcilePendingStudyCell(m studycontract.Manifest, s *studyState) error {
	if s.Pending == nil {
		return nil
	}
	rec, err := loop.Load(layout(), s.Pending.RunID)
	if err != nil {
		return fmt.Errorf("study has pending run %s that cannot be loaded: %w", s.Pending.RunID, err)
	}
	switch rec.Status {
	case "pending":
		return nil
	case "completed", "failed", "timeout", "budget_exceeded":
		if len(rec.Judges) == 0 {
			return fmt.Errorf("study has unresolved pending run %s with status %s and no judge result", rec.ID, rec.Status)
		}
		s.Completed = append(s.Completed, *s.Pending)
		s.Pending = nil
		return saveStudyState(m, *s)
	default:
		return fmt.Errorf("study has unresolved pending run %s with status %s; inspect it before resuming to avoid duplicate spend", rec.ID, rec.Status)
	}
}

func verifyStudyScenarios(m studycontract.Manifest) error {
	l := layout()
	for _, frozen := range m.Scenarios {
		sc, err := resolveFrozenStudyScenario(l, frozen)
		if err != nil {
			return fmt.Errorf("resolve frozen scenario %s: %w", frozen.ID, err)
		}
		if err := verifyStudyScenario(sc, frozen); err != nil {
			return err
		}
	}
	return nil
}

func resolveFrozenStudyScenario(l paths.Layout, frozen studycontract.Scenario) (corpus.Scenario, error) {
	if len(frozen.Task) == 0 {
		return corpus.Resolve(l.ScenariosDir(), "", frozen.ID)
	}
	if err := studycontract.ValidatePublicTask(frozen.Task); err != nil {
		return corpus.Scenario{}, err
	}
	var task struct {
		ID          string            `json:"id"`
		Type        string            `json:"type"`
		Title       string            `json:"title"`
		Description string            `json:"description"`
		Prompt      string            `json:"prompt"`
		Language    string            `json:"language"`
		Tags        []string          `json:"tags"`
		Difficulty  string            `json:"difficulty"`
		Repo        corpus.Repo       `json:"repo"`
		Acceptance  corpus.Acceptance `json:"acceptance"`
	}
	raw, err := json.Marshal(frozen.Task)
	if err != nil {
		return corpus.Scenario{}, err
	}
	if err := json.Unmarshal(raw, &task); err != nil {
		return corpus.Scenario{}, err
	}
	return corpus.Scenario{
		ID: frozen.ID, Type: task.Type, Title: task.Title, Description: task.Description,
		Prompt: task.Prompt, Language: task.Language, Tags: task.Tags, Difficulty: task.Difficulty,
		Repo: task.Repo, Acceptance: task.Acceptance, ManifestDigest: frozen.Digest, External: true,
	}, nil
}

func verifyStudyScenario(sc corpus.Scenario, frozen studycontract.Scenario) error {
	if len(frozen.Task) > 0 {
		digest, err := studycontract.TaskDigest(frozen.Task)
		if err != nil {
			return err
		}
		if digest != frozen.Digest {
			return fmt.Errorf("scenario %s task digest drift: contract has %s, task has %s", frozen.ID, frozen.Digest, digest)
		}
		return nil
	}
	digest := sc.ManifestDigest
	if digest == "" {
		var err error
		digest, err = corpus.TrustDigest(sc)
		if err != nil {
			return err
		}
	}
	if digest != frozen.Digest {
		return fmt.Errorf("scenario %s digest drift: contract has %s, resolved scenario has %s", frozen.ID, frozen.Digest, digest)
	}
	return nil
}

func authorizeStudyScenarios(m studycontract.Manifest) error {
	l := layout()
	var scenarios []corpus.Scenario
	for _, frozen := range m.Scenarios {
		sc, err := resolveFrozenStudyScenario(l, frozen)
		if err != nil {
			return fmt.Errorf("resolve scenario trust for %s: %w", frozen.ID, err)
		}
		scenarios = append(scenarios, sc)
	}
	return authorizeResolvedStudyScenarios(layout(), scenarios)
}

func authorizeResolvedStudyScenarios(l paths.Layout, scenarios []corpus.Scenario) error {
	for _, sc := range scenarios {
		if sc.External {
			if err := authorizeExternalScenario(l, sc, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

func prepareStudyInputs(m studycontract.Manifest) error {
	l := layout()
	for _, frozen := range m.Scenarios {
		sc, err := resolveFrozenStudyScenario(l, frozen)
		if err != nil {
			return err
		}
		if _, err := loop.CheckRequirements(sc); err != nil {
			return err
		}
		if err := loop.PrepareInputs(l, sc, true, func(plan fetchconsent.Plan) error { return authorizeFetch(l, plan) }); err != nil {
			return err
		}
	}
	return nil
}

func verifyStudyHarnesses(m studycontract.Manifest) error {
	seen := map[string]string{}
	for _, arm := range m.Arms {
		if len(arm.Adapter) > 0 {
			_, digest, err := adapter.FromMap(arm.Adapter)
			if err != nil {
				return fmt.Errorf("arm %s adapter: %w", arm.ID, err)
			}
			expected := "adapter/" + digest[:12]
			if arm.Version != expected {
				return fmt.Errorf("arm %s adapter version drift: contract has %q, manifest is %q", arm.ID, arm.Version, expected)
			}
			continue
		}
		actual, ok := seen[arm.Harness]
		if !ok {
			actual = loop.DetectHarnessVersion(arm.Harness)
			seen[arm.Harness] = actual
		}
		if actual == "" {
			return fmt.Errorf("could not resolve %s harness version", arm.Harness)
		}
		if actual != arm.Version {
			return fmt.Errorf("%s harness version drift: contract has %q, installed binary is %q", arm.Harness, arm.Version, actual)
		}
	}
	return nil
}

func verifyStudyExecutionProfiles(m studycontract.Manifest) error {
	for _, arm := range m.Arms {
		profile := loop.Profile{Mode: arm.Mode, Harness: arm.Harness, Provider: arm.Provider, Model: arm.Model, ModelVersion: arm.ModelVersion, PromptTreatment: arm.PromptTreatment, Adapter: arm.Adapter, Assurance: arm.Assurance, Reasoning: arm.Reasoning, Workflow: arm.Workflow, Skills: arm.Skills, Extensions: arm.Extensions, Plugins: arm.Plugins, Tools: arm.Tools, Subagents: arm.Subagents, Environment: arm.Environment, Network: arm.Network}
		if _, err := loop.ResolveMeasuredProfile(profile); err != nil {
			return fmt.Errorf("arm %s: %w", arm.ID, err)
		}
	}
	return nil
}

func publicStudyManifest(m studycontract.Manifest) studycontract.Manifest {
	copy := m
	copy.Arms = append([]studycontract.Arm(nil), m.Arms...)
	for i := range copy.Arms {
		copy.Arms[i].LocalSkills = nil
	}
	return copy
}

func publishStudy(path string, m studycontract.Manifest, args []string) error {
	fs := flag.NewFlagSet("study publish", flag.ContinueOnError)
	calloutSlug := fs.String("callout", "", "Callout slug for this frozen contract")
	preview := fs.Bool("preview", false, "show the publication payload without uploading")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := loadStudyState(m)
	if err != nil {
		return err
	}
	if err := verifyStudyLocalSkills(m); err != nil {
		return err
	}
	if err := validateStudyStateCells(m, s); err != nil {
		return err
	}
	if len(s.Completed) != m.RunCount() || s.Pending != nil {
		return fmt.Errorf("study is incomplete; run hbench study status")
	}
	if err := enforceStudyBudget(m, s); err != nil {
		return err
	}
	if m.IsPrivate() && !*preview {
		return fmt.Errorf("private studies cannot be published; use --preview for a local publication check")
	}
	if *preview {
		return printStudyPublicationPreview(path, m, s)
	}
	if err := verifyStudyScenarios(m); err != nil {
		return err
	}
	if *calloutSlug == "" {
		if raw, readErr := os.ReadFile(calloutSidecar(path)); readErr == nil {
			*calloutSlug = strings.TrimSpace(string(raw))
		}
	}
	if *calloutSlug == "" {
		return fmt.Errorf("a Callout slug is required; use --callout SLUG or hbench callout challenge URL")
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`).MatchString(*calloutSlug) {
		return fmt.Errorf("invalid Callout slug %q", *calloutSlug)
	}
	links := make([]map[string]any, 0, len(s.Completed))
	for _, c := range s.Completed {
		_, err := publish.Publish(layout(), c.RunID, nil)
		if err != nil {
			return err
		}
		links = append(links, studyRunLink(c))
	}
	publicManifest := publicStudyManifest(m)
	out, err := publish.AuthenticatedJSON(http.MethodPost, "/api/v1/studies", map[string]any{"schema": "hb.study.publish.v1", "manifest": publicManifest, "contract_digest": m.Digest(), "callout_slug": *calloutSlug, "runs": links}, nil)
	if err != nil {
		return err
	}
	fmt.Printf("Published study: %v\n", out["url"])
	return nil
}

func validateStudyStateCells(m studycontract.Manifest, s studyState) error {
	if s.Pending != nil {
		return fmt.Errorf("study is incomplete; run hbench study status")
	}
	allowedArms, allowedScenarios := map[string]bool{}, map[string]bool{}
	for _, arm := range m.Arms {
		allowedArms[arm.ID] = true
	}
	for _, scenario := range m.Scenarios {
		allowedScenarios[scenario.ID] = true
	}
	seen := map[string]bool{}
	for _, cell := range s.Completed {
		if !allowedArms[cell.Arm] || !allowedScenarios[cell.Scenario] || cell.Repeat < 1 || cell.Repeat > m.Repeats {
			return fmt.Errorf("saved state contains undeclared study cell %s/%s repeat %d", cell.Arm, cell.Scenario, cell.Repeat)
		}
		key := cellKey(cell.Arm, cell.Scenario, cell.Repeat)
		if seen[key] {
			return fmt.Errorf("saved state contains duplicate study cell %s/%s repeat %d", cell.Arm, cell.Scenario, cell.Repeat)
		}
		seen[key] = true
	}
	return nil
}

func printStudyPublicationPreview(path string, m studycontract.Manifest, s studyState) error {
	type armPreview struct {
		Arm           string  `json:"arm"`
		Runs          int     `json:"runs"`
		Expected      int     `json:"expected"`
		CostUSD       float64 `json:"cost_usd"`
		CostComplete  bool    `json:"cost_complete"`
		TokenComplete bool    `json:"token_complete"`
	}
	byArm := map[string]*armPreview{}
	for _, arm := range m.Arms {
		byArm[arm.ID] = &armPreview{Arm: arm.ID, Expected: len(m.Scenarios) * m.Repeats, CostComplete: true, TokenComplete: true}
	}
	for _, cell := range s.Completed {
		rec, err := loop.Load(layout(), cell.RunID)
		if err != nil {
			return err
		}
		p := byArm[cell.Arm]
		if p == nil {
			p = &armPreview{Arm: cell.Arm, Expected: len(m.Scenarios) * m.Repeats, CostComplete: true, TokenComplete: true}
			byArm[cell.Arm] = p
		}
		p.Runs++
		if rec.Telemetry.EstimatedUSD == nil || rec.Telemetry.Complete == nil || !*rec.Telemetry.Complete ||
			(rec.Telemetry.CostKind != "actual" && rec.Telemetry.CostKind != "estimated") ||
			(rec.Telemetry.CostKind == "estimated" && rec.Telemetry.PriceSnapshot == "") {
			p.CostComplete = false
		} else {
			p.CostUSD += *rec.Telemetry.EstimatedUSD
		}
		if rec.Telemetry.TokenComplete == nil || !*rec.Telemetry.TokenComplete {
			p.TokenComplete = false
		}
	}
	arms := make([]armPreview, 0, len(byArm))
	for _, arm := range m.Arms {
		arms = append(arms, *byArm[arm.ID])
	}
	payload := map[string]any{
		"schema": "hb.study.publish-preview.v1", "manifest": publicStudyManifest(m), "contract_digest": m.Digest(),
		"private": m.IsPrivate(), "runs": arms,
		"reproduction": fmt.Sprintf("hbench study run %s --approve-spend", path),
		"publication":  "hbench study publish " + path,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	fmt.Println("Nothing was uploaded.")
	return nil
}

func studyRunLink(c studyCell) map[string]any {
	return map[string]any{"client_run_id": c.RunID, "arm_id": c.Arm, "scenario_id": c.Scenario, "repeat": c.Repeat}
}

func cmdCallout(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hbench callout create STUDY.yaml --statement TEXT | challenge URL")
	}
	switch args[0] {
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("study manifest is required")
		}
		fs := flag.NewFlagSet("callout create", flag.ContinueOnError)
		statement := fs.String("statement", "", "testable public statement")
		source := fs.String("source", "", "HTTPS source URL")
		author := fs.String("source-author", "", "source author")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *statement == "" {
			return fmt.Errorf("--statement is required")
		}
		m, err := studycontract.Load(args[1])
		if err != nil {
			return err
		}
		if m.IsPrivate() {
			return fmt.Errorf("private studies cannot create public Callouts")
		}
		sources := m.Sources
		if *source != "" {
			sources = append(sources, studycontract.Source{URL: *source, Author: *author})
		}
		out, err := publish.AuthenticatedJSON(http.MethodPost, "/api/v1/callouts", map[string]any{"statement": *statement, "contract": publicStudyManifest(m), "contract_digest": m.Digest(), "sources": sources}, nil)
		if err != nil {
			return err
		}
		if slug := slugFromCalloutURL(fmt.Sprint(out["url"])); slug != "" {
			if err := os.WriteFile(calloutSidecar(args[1]), []byte(slug+"\n"), 0o644); err != nil {
				return err
			}
		}
		fmt.Printf("Created Callout: %v\n", out["url"])
		return nil
	case "challenge":
		if len(args) != 2 {
			return fmt.Errorf("Callout URL is required")
		}
		return fetchCalloutContract(args[1])
	default:
		return fmt.Errorf("unknown callout command %q", args[0])
	}
}

func fetchCalloutContract(rawURL string) error {
	page, err := url.Parse(rawURL)
	if err != nil || page.Scheme != "https" || page.User != nil || page.RawQuery != "" || page.Fragment != "" {
		return fmt.Errorf("Callout URL must be HTTPS")
	}
	origin, err := publish.ValidatedRodeoURL()
	if err != nil {
		return err
	}
	allowed, _ := url.Parse(origin)
	if !strings.EqualFold(page.Host, allowed.Host) {
		return fmt.Errorf("Callout URL must use %s", allowed.Host)
	}
	parts := strings.Split(strings.Trim(page.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "callouts" || parts[1] == "" {
		return fmt.Errorf("expected a /callouts/SLUG URL")
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(origin + "/api/v1/callouts/" + url.PathEscape(parts[1]))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("rodeo %d: %s", response.StatusCode, raw)
	}
	var payload struct {
		Contract studycontract.Manifest `json:"contract"`
		Digest   string                 `json:"contract_digest"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	if err := payload.Contract.Validate(); err != nil {
		return err
	}
	if payload.Contract.Digest() != payload.Digest {
		return fmt.Errorf("Callout contract digest mismatch")
	}
	out, err := yaml.Marshal(payload.Contract)
	if err != nil {
		return err
	}
	path := parts[1] + ".study.yaml"
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite %s", path)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(calloutSidecar(path), []byte(parts[1]+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("Saved frozen contract to %s\nNext: hbench study plan %s\n", path, path)
	return nil
}

func calloutSidecar(studyPath string) string { return studyPath + ".callout" }

func slugFromCalloutURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 2 && parts[0] == "callouts" {
		return parts[1]
	}
	return ""
}
