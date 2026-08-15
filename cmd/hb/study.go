package main

import (
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

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
	"github.com/clayton/harness-benchmark/internal/publish"
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

func cmdStudy(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: hbench study validate|plan|run|status|publish STUDY.yaml")
	}
	action, path := args[0], args[1]
	m, err := studycontract.Load(path)
	if err != nil {
		return err
	}
	switch action {
	case "validate":
		fmt.Printf("Valid %s study %s\ncontract: %s\n", m.ComparisonMode, m.ID, m.Digest())
		return nil
	case "plan":
		return printStudyPlan(m)
	case "status":
		return printStudyStatus(m)
	case "run":
		return runStudy(m, args[2:])
	case "publish":
		return publishStudy(path, m, args[2:])
	default:
		return fmt.Errorf("unknown study command %q", action)
	}
}

func printStudyPlan(m studycontract.Manifest) error {
	fmt.Printf("%s\nmode: %s\ncontract: %s\n", m.Question, m.ComparisonMode, m.Digest())
	fmt.Printf("matrix: %d arms × %d scenarios × %d repeats = %d runs\n", len(m.Arms), len(m.Scenarios), m.Repeats, m.RunCount())
	fmt.Printf("changed axes: %s\n", strings.Join(m.DifferingAxes(), ", "))
	for _, a := range m.Arms {
		fmt.Printf("  %s: %s / %s", a.ID, a.Harness, a.Model)
		if len(a.Plugins) > 0 {
			fmt.Printf(" / plugins %s", strings.Join(a.Plugins, ", "))
		}
		fmt.Println()
	}
	if m.Budget.MaxUSDTotal != nil {
		fmt.Printf("post-run spend stop threshold: $%.2f (one run can overshoot)\n", *m.Budget.MaxUSDTotal)
	} else {
		fmt.Println("post-run spend stop threshold: not declared")
	}
	fmt.Printf("per-run timeout: %d minutes\n", m.Budget.MaxMinutes)
	return nil
}

func statePath(m studycontract.Manifest) string {
	return filepath.Join(layout().OutDir, "studies", safeStudyID(m.ID)+".json")
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
	if err := reconcilePendingStudyCell(m, &s); err != nil {
		return err
	}
	if err := verifyStudyScenarios(m); err != nil {
		return err
	}
	if err := authorizeStudyScenarios(m); err != nil {
		return err
	}
	if err := verifyStudyHarnesses(m); err != nil {
		return err
	}
	if err := verifyStudyExecutionProfiles(m); err != nil {
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
		sc, err := corpus.Resolve(layout().ScenariosDir(), "", c.Scenario)
		if err != nil {
			return err
		}
		profile := loop.Profile{ID: arm.ID, Harness: arm.Harness, HarnessVersion: arm.Version, Provider: arm.Provider, Model: arm.Model, Reasoning: arm.Reasoning, Workflow: arm.Workflow, Skills: arm.Skills, Extensions: arm.Extensions, Plugins: arm.Plugins, Tools: arm.Tools, Subagents: arm.Subagents, Environment: arm.Environment, Network: arm.Network, JudgeProtocol: m.JudgeProtocol, Budget: studyBudgetDescriptor(m), StudyID: m.ID, ContractDigest: m.Digest(), ArmID: c.Arm, Repeat: c.Repeat, ScenarioDigest: frozenScenario.Digest, StudyScenarioID: c.Scenario}
		rec, err := loop.CreateRunWithProfile(layout(), sc, profile, true)
		if err != nil {
			return err
		}
		c.RunID = rec.ID
		s.Pending = &c
		if err := saveStudyState(m, s); err != nil {
			return err
		}
		fmt.Printf("running %s / %s / repeat %d as %s\n", c.Arm, c.Scenario, c.Repeat, rec.ID)
		res, execErr := loop.Execute(layout(), rec.ID, time.Duration(m.Budget.MaxMinutes)*time.Minute)
		if res.LogPath == "" && execErr != nil {
			return execErr
		}
		finished, finishErr := loop.Finish(layout(), rec.ID, sc, res.WallMS, "hbench study run", true)
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
		if execErr != nil {
			return execErr
		}
	}
	return printStudyStatus(m)
}

func enforceStudyBudget(m studycontract.Manifest, s studyState) error {
	totalCost := 0.0
	for _, cell := range s.Completed {
		rec, err := loop.Load(layout(), cell.RunID)
		if err != nil {
			return err
		}
		if rec.Status == "timeout" || rec.Status == "budget_exceeded" {
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
			if rec.Telemetry.EstimatedUSD == nil || rec.Telemetry.Complete == nil || !*rec.Telemetry.Complete {
				return fmt.Errorf("run %s has no complete cost telemetry for its dollar limit", cell.RunID)
			}
			if *rec.Telemetry.EstimatedUSD > *m.Budget.MaxUSDPerRun {
				return fmt.Errorf("run %s exceeded the dollar limit", cell.RunID)
			}
		}
		if m.Budget.MaxUSDTotal != nil && (rec.Telemetry.EstimatedUSD == nil || rec.Telemetry.Complete == nil || !*rec.Telemetry.Complete) {
			return fmt.Errorf("run %s has no complete cost telemetry for the total dollar limit", cell.RunID)
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
	if len(rec.Judges) == 0 || (rec.Status != "completed" && rec.Status != "failed" && rec.Status != "timeout" && rec.Status != "budget_exceeded") {
		return fmt.Errorf("study has unresolved pending run %s with status %s; inspect it before resuming to avoid duplicate spend", rec.ID, rec.Status)
	}
	s.Completed = append(s.Completed, *s.Pending)
	s.Pending = nil
	return saveStudyState(m, *s)
}

func verifyStudyScenarios(m studycontract.Manifest) error {
	for _, frozen := range m.Scenarios {
		sc, err := corpus.Resolve(layout().ScenariosDir(), "", frozen.ID)
		if err != nil {
			return fmt.Errorf("resolve frozen scenario %s: %w", frozen.ID, err)
		}
		digest := sc.ManifestDigest
		if digest == "" {
			digest, err = corpus.TrustDigest(sc)
			if err != nil {
				return err
			}
		}
		if digest != frozen.Digest {
			return fmt.Errorf("scenario %s digest drift: contract has %s, resolved scenario has %s", frozen.ID, frozen.Digest, digest)
		}
	}
	return nil
}

func authorizeStudyScenarios(m studycontract.Manifest) error {
	var scenarios []corpus.Scenario
	for _, frozen := range m.Scenarios {
		sc, err := corpus.Resolve(layout().ScenariosDir(), "", frozen.ID)
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

func verifyStudyHarnesses(m studycontract.Manifest) error {
	seen := map[string]string{}
	for _, arm := range m.Arms {
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
		profile := loop.Profile{Harness: arm.Harness, Provider: arm.Provider, Model: arm.Model, Reasoning: arm.Reasoning, Workflow: arm.Workflow, Skills: arm.Skills, Extensions: arm.Extensions, Plugins: arm.Plugins, Tools: arm.Tools, Subagents: arm.Subagents, Environment: arm.Environment, Network: arm.Network}
		if _, err := loop.ResolveMeasuredProfile(profile); err != nil {
			return fmt.Errorf("arm %s: %w", arm.ID, err)
		}
	}
	return nil
}

func publishStudy(path string, m studycontract.Manifest, args []string) error {
	fs := flag.NewFlagSet("study publish", flag.ContinueOnError)
	calloutSlug := fs.String("callout", "", "Callout slug for this frozen contract")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := loadStudyState(m)
	if err != nil {
		return err
	}
	if len(s.Completed) != m.RunCount() || s.Pending != nil {
		return fmt.Errorf("study is incomplete; run hbench study status")
	}
	if err := verifyStudyScenarios(m); err != nil {
		return err
	}
	if err := enforceStudyBudget(m, s); err != nil {
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
		out, err := publish.Publish(layout(), c.RunID, nil)
		if err != nil {
			return err
		}
		links = append(links, map[string]any{"client_run_id": c.RunID, "arm_id": c.Arm, "scenario_id": c.Scenario, "repeat": c.Repeat, "published_run_id": out["id"]})
	}
	out, err := publish.AuthenticatedJSON(http.MethodPost, "/api/v1/studies", map[string]any{"schema": "hb.study.publish.v1", "manifest": m, "contract_digest": m.Digest(), "callout_slug": *calloutSlug, "runs": links}, nil)
	if err != nil {
		return err
	}
	fmt.Printf("Published study: %v\n", out["url"])
	return nil
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
		sources := m.Sources
		if *source != "" {
			sources = append(sources, studycontract.Source{URL: *source, Author: *author})
		}
		out, err := publish.AuthenticatedJSON(http.MethodPost, "/api/v1/callouts", map[string]any{"statement": *statement, "contract": m, "contract_digest": m.Digest(), "sources": sources}, nil)
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
