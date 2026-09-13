package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/clayton/harness-benchmark/internal/adapter"
	"github.com/clayton/harness-benchmark/internal/controlled"
	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/doctor"
	"github.com/clayton/harness-benchmark/internal/fetchconsent"
	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
	"github.com/clayton/harness-benchmark/internal/publish"
	"github.com/clayton/harness-benchmark/internal/report"
	studycontract "github.com/clayton/harness-benchmark/internal/study"
	"github.com/clayton/harness-benchmark/internal/trust"
	"github.com/clayton/harness-benchmark/skills"
	"gopkg.in/yaml.v3"
)

const (
	version           = "0.7.0"
	defaultRelayImage = "docker.io/claytonlz/agent-rodeo-model-relay@sha256:bcb8fa0938bc93d1c029d21978b7e8339ed24adf179109d5a79f48a5a6958dfa"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return cmdSuggest()
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("hbench %s (go)\n", version)
		return nil
	case "doctor":
		return cmdDoctor(args[1:])
	case "fetch":
		return cmdFetch(args[1:])
	case "help", "-h", "--help":
		printHelp()
		return nil
	case "list":
		return cmdList(args[1:])
	case "run":
		return cmdRunMode(args[1:], false)
	case "ride":
		return cmdRunMode(args[1:], true)
	case "execute":
		return cmdExecute(args[1:])
	case "finish":
		return cmdFinish(args[1:])
	case "report":
		return cmdReport(args[1:])
	case "publish":
		return cmdPublish(args[1:])
	case "inspect":
		return cmdInspect(args[1:])
	case "trust":
		return cmdTrust(args[1:])
	case "sandbox-command":
		return cmdSandboxCommand(args[1:])
	case "controlled":
		return cmdControlled(args[1:])
	case "study":
		return cmdStudy(args[1:])
	case "callout":
		return cmdCallout(args[1:])
	case "skill":
		return cmdSkill(args[1:])
	case "setup":
		return cmdSetup(args[1:])
	case "scenario":
		return cmdScenario(args[1:])
	case "adapter":
		return cmdAdapter(args[1:])
	case "reproduce":
		return cmdReproduce(args[1:])
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

func cmdControlled(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hbench controlled keygen|validate|run")
	}
	switch args[0] {
	case "keygen":
		fs := flag.NewFlagSet("controlled keygen", flag.ContinueOnError)
		key := fs.String("key", defaultRunnerKey(), "private key path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		publicPath, fingerprint, err := controlled.Keygen(*key)
		if err != nil {
			return err
		}
		fmt.Printf("Created runner key %s\n  private: %s\n  public:  %s\n", fingerprint, *key, publicPath)
		return nil
	case "validate", "run":
		return cmdControlledAction(args[0], args[1:])
	default:
		return fmt.Errorf("unknown controlled command %q", args[0])
	}
}

func cmdControlledAction(action string, args []string) error {
	fs := flag.NewFlagSet("controlled "+action, flag.ContinueOnError)
	scenarioID := fs.String("scenario", "", "rodeo:slug@version")
	packPath := fs.String("pack", "", "private evaluator pack directory")
	keyPath := fs.String("key", defaultRunnerKey(), "runner private key path")
	keyID := fs.String("key-id", "", "registered runner key ID")
	setupID := fs.String("setup", "", "digest-bound named setup from the evaluator pack")
	relayImage := fs.String("relay-image", os.Getenv("HB_RELAY_IMAGE"), "pinned credential relay image")
	runtimeName := fs.String("runtime", os.Getenv("HB_RUNTIME"), "OCI runtime: auto, docker, podman, or nerdctl")
	approveSpend := fs.Bool("approve-spend", false, "confirm this credential-backed run may spend money")
	yes := fs.Bool("yes", false, "approve the displayed immutable plan")
	artifactDir := fs.String("artifacts", filepath.Join(layout().OutDir, "controlled"), "private artifact directory")
	studyPath := fs.String("study", "", "frozen Study YAML to bind before execution")
	studyScenario := fs.String("study-scenario-id", "", "Study scenario cell id")
	studyArm := fs.String("study-arm-id", "", "Study arm cell id")
	studyRepeat := fs.Int("study-repeat", 0, "Study repeat number")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scenarioID == "" || *packPath == "" || *keyID == "" {
		return fmt.Errorf("--scenario, --pack, and --key-id are required")
	}
	if action == "run" && !*approveSpend {
		return fmt.Errorf("controlled run requires --approve-spend; this run can use model credentials and incur provider charges")
	}
	rt, err := controlled.SelectRuntime(*runtimeName)
	if err != nil {
		return err
	}
	os.Setenv("HB_RUNTIME", rt.Name)
	l := layout()
	scenario, err := corpus.Resolve(l.ScenariosDir(), "", *scenarioID)
	if err != nil {
		return err
	}
	pack, packDigest, err := controlled.LoadPack(*packPath)
	if err != nil {
		return err
	}
	if scenario.ID != fmt.Sprintf("%s@%d", pack.ScenarioSlug, pack.ScenarioVersion) {
		return fmt.Errorf("evaluator pack does not match scenario %s", scenario.ID)
	}
	minutes := 45
	if value, ok := controlledBudgetNumber(pack.Budget, "max_minutes"); ok {
		minutes = int(value)
	}
	if action == "validate" {
		if *setupID != "" || *studyPath != "" || *studyArm != "" || *studyScenario != "" || *studyRepeat != 0 {
			return fmt.Errorf("--setup and Study binding flags apply only to controlled run")
		}
		ctx, cancel := controlled.RunTimeout(minutes)
		defer cancel()
		result, err := controlled.Validate(ctx, scenario, pack, *packPath)
		if err != nil {
			return err
		}
		envelope, err := controlled.Sign(*keyPath, *keyID, controlled.ValidationPayload(scenario, pack, packDigest, result), "", map[string]any{"passed": true})
		if err != nil {
			return err
		}
		rodeoURL, err := publish.ValidatedRodeoURL()
		if err != nil {
			return err
		}
		response, err := controlled.Upload(rodeoURL, envelope, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Validated %s twice and uploaded attestation: %v\n", scenario.ID, response["accepted"])
		return nil
	}
	pack, err = pack.SelectSetup(*setupID)
	if err != nil {
		return err
	}
	studyBinding, studyConfig, err := controlledStudyCell(*studyPath, *studyArm, *studyScenario, *studyRepeat, scenario, pack)
	if err != nil {
		return err
	}
	if *relayImage == "" || !controlled.PinnedImage(*relayImage) {
		return fmt.Errorf("--relay-image must be pinned by sha256 digest")
	}
	inputPlan, err := loop.InputPlan(l, scenario, false)
	if err != nil {
		return err
	}
	items := append([]fetchconsent.Item(nil), inputPlan.Items...)
	budgetJSON, _ := json.Marshal(pack.Budget)
	items = append(items,
		fetchconsent.Item{Kind: "Scenario contract", Source: scenario.ID, Ref: scenario.ManifestDigest, Reason: "immutable controlled definition", Destination: l.ScenariosDir(), Size: "metadata only"},
		fetchconsent.Item{Kind: "Evaluator pack", Source: *packPath, Ref: packDigest, Checksum: "sha256:" + packDigest, Reason: "private judge and execution contract", Destination: *artifactDir, Size: "local files"},
		imagePlanItem("OCI environment", pack.EnvironmentImageDigest, rt.Name+" image store", "sealed scenario tools and dependencies"),
		imagePlanItem("Credential relay", *relayImage, rt.Name+" image store", "isolated model API access"),
		fetchconsent.Item{Kind: "Model execution", Source: pack.Relay.Upstream, Ref: pack.Execution.Model, Reason: "model inference; provider charges apply", Destination: "credential relay", Size: fmt.Sprintf("at most %d minutes", minutes)},
		fetchconsent.Item{Kind: "Spend boundary", Source: string(budgetJSON), Reason: "immutable controlled-run limits", Destination: "credential relay", Size: "metadata only"},
		fetchconsent.Item{Kind: "Environment variable", Source: pack.Relay.SecretEnv, Reason: "provider authentication", Destination: "credential relay secret file", Size: "value is not recorded"},
	)
	fullPlan := fetchconsent.New(items...)
	if *yes {
		err = approveFetchPlan(l, fullPlan)
	} else {
		err = authorizeFetch(l, fullPlan)
	}
	if err != nil {
		return err
	}
	if err := loop.PrepareApprovedInputs(l, scenario, false); err != nil {
		return err
	}
	workspaceID := loop.NewID()
	workspace, err := loop.PrepareWorktree(l, scenario, workspaceID, false)
	if err != nil {
		return err
	}
	defer os.RemoveAll(l.RunDir(workspaceID))
	ctx, cancel := controlled.RunTimeout(minutes)
	defer cancel()
	result, err := controlled.Run(ctx, scenario, pack, *packPath, *relayImage, *artifactDir, workspace)
	if err != nil {
		return err
	}
	if studyBinding != nil {
		result.Payload["study"] = studyBinding
		config, _ := result.Payload["config"].(map[string]any)
		for _, key := range []string{"mode", "provider", "reasoning", "workflow", "skills", "extensions", "plugins", "tools", "subagent_topology", "interaction", "budget", "environment", "network", "config_sha256", "config_status", "prompt_treatment", "adapter", "assurance", "judge_protocol"} {
			delete(config, key)
		}
		for key, value := range studyConfig {
			config[key] = value
		}
		result.Payload["config"] = config
	}
	envelope, err := controlled.Sign(*keyPath, *keyID, result.Payload, result.Patch, result.Report)
	if err != nil {
		return err
	}
	rodeoURL, err := publish.ValidatedRodeoURL()
	if err != nil {
		return err
	}
	response, err := controlled.Upload(rodeoURL, envelope, nil)
	if err != nil {
		return err
	}
	fmt.Printf("Uploaded Controlled run %v\n  private log: %s (retain 90 days)\n", response["run_url"], result.LogPath)
	return nil
}

func controlledStudyCell(path, armID, scenarioID string, repeat int, scenario corpus.Scenario, pack controlled.Pack) (map[string]any, map[string]any, error) {
	provided := 0
	for _, value := range []string{path, armID, scenarioID} {
		if value != "" {
			provided++
		}
	}
	if repeat != 0 {
		provided++
	}
	if provided == 0 {
		return nil, nil, nil
	}
	if provided != 4 || repeat < 1 {
		return nil, nil, fmt.Errorf("--study, --study-arm-id, --study-scenario-id, and --study-repeat are required together")
	}
	manifest, err := studycontract.Load(path)
	if err != nil {
		return nil, nil, err
	}
	if manifest.IsPrivate() {
		return nil, nil, fmt.Errorf("controlled evidence cannot bind a private Study")
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,99}$`).MatchString(manifest.ID) {
		return nil, nil, fmt.Errorf("controlled Study ID is not accepted by the attestation endpoint")
	}
	if repeat > manifest.Repeats {
		return nil, nil, fmt.Errorf("Study repeat is outside the frozen contract")
	}
	var studyScenario *studycontract.Scenario
	for i := range manifest.Scenarios {
		if manifest.Scenarios[i].ID == scenarioID {
			studyScenario = &manifest.Scenarios[i]
			break
		}
	}
	if studyScenario == nil || strings.TrimPrefix(scenarioID, "rodeo:") != scenario.ID || studyScenario.Digest != scenario.ManifestDigest {
		return nil, nil, fmt.Errorf("controlled scenario does not match the frozen Study cell")
	}
	var arm *studycontract.Arm
	for i := range manifest.Arms {
		if manifest.Arms[i].ID == armID {
			arm = &manifest.Arms[i]
			break
		}
	}
	if arm == nil {
		return nil, nil, fmt.Errorf("Study arm is not in the frozen contract")
	}
	workflow := arm.Workflow
	if workflow == "" {
		workflow = "baseline"
	}
	if arm.Harness != pack.Execution.Harness || arm.Version != pack.Execution.HarnessVersion || arm.Provider != pack.Execution.Provider || arm.Model != pack.Execution.Model || arm.ModelVersion != pack.Execution.ModelVersion || arm.Reasoning != pack.Execution.Reasoning || workflow != "baseline" || !sameStringSet(arm.Extensions, pack.Execution.Extensions) || !sameStringSet(arm.Plugins, pack.Execution.Plugins) {
		return nil, nil, fmt.Errorf("controlled evaluator setup does not match the frozen Study arm")
	}
	if len(arm.Skills) > 0 || len(arm.LocalSkillDigests) > 0 || len(arm.Tools) > 0 || arm.Subagents != "" || len(arm.PromptTreatment) > 0 || len(arm.Adapter) > 0 || len(arm.Assurance) > 0 || arm.ConfigDigest != "" || arm.ConfigStatus != "" {
		return nil, nil, fmt.Errorf("controlled evaluator does not enforce the Study arm's custom setup fields")
	}
	if manifest.Schema == studycontract.SchemaV2 && arm.Mode != "clean-baseline" {
		return nil, nil, fmt.Errorf("controlled Study arms must use clean-baseline mode")
	}
	if arm.Environment != "" && arm.Environment != pack.EnvironmentImageDigest {
		return nil, nil, fmt.Errorf("Study environment does not match the controlled image digest")
	}
	if arm.Network != "none" {
		return nil, nil, fmt.Errorf("controlled Study network must be explicitly none")
	}
	for key := range pack.Budget {
		if key != "max_minutes" && key != "max_usd" {
			return nil, nil, fmt.Errorf("controlled evaluator has an undeclared Study budget key %q", key)
		}
	}
	packMinutes, ok := controlledBudgetNumber(pack.Budget, "max_minutes")
	if !ok || packMinutes != float64(manifest.Budget.MaxMinutes) {
		return nil, nil, fmt.Errorf("controlled timeout does not equal the frozen Study budget")
	}
	packUSD, ok := controlledBudgetNumber(pack.Budget, "max_usd")
	if !ok || manifest.Budget.MaxUSDPerRun == nil || packUSD != *manifest.Budget.MaxUSDPerRun {
		return nil, nil, fmt.Errorf("controlled spend cap does not equal the frozen Study per-run budget")
	}
	budget := map[string]any{"max_minutes_per_run": manifest.Budget.MaxMinutes}
	if manifest.Budget.MaxTokens != nil {
		budget["max_tokens_per_run"] = *manifest.Budget.MaxTokens
	}
	if manifest.Budget.MaxUSDPerRun != nil {
		budget["max_usd_per_run"] = *manifest.Budget.MaxUSDPerRun
	}
	config := map[string]any{
		"provider": arm.Provider, "reasoning": arm.Reasoning, "workflow": workflow,
		"skills": []string{}, "extensions": arm.Extensions, "plugins": arm.Plugins,
		"tools": []string{}, "interaction": "unattended", "budget": budget,
		"judge_protocol": manifest.JudgeProtocol,
	}
	if arm.Mode != "" {
		config["mode"] = arm.Mode
	}
	if arm.Environment != "" {
		config["environment"] = arm.Environment
	}
	config["network"] = arm.Network
	binding := map[string]any{
		"id": manifest.ID, "contract_digest": manifest.Digest(), "scenario_id": studyScenario.ID,
		"scenario_digest": studyScenario.Digest, "arm_id": arm.ID, "repeat": repeat,
	}
	return binding, config, nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := map[string]int{}
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func controlledBudgetNumber(budget map[string]any, key string) (float64, bool) {
	switch value := budget[key].(type) {
	case int:
		return float64(value), value > 0
	case float64:
		return value, value > 0
	default:
		return 0, false
	}
}

func defaultRunnerKey() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "hb", "runner-ed25519.pem")
}

func usage() string {
	return `hbench — compare coding-agent systems on fixed, repeatable tasks.

Commands:
  hbench                      print one suggested command
  hbench doctor [-s <scenario>] what this machine has
  hbench fetch show|approve|revoke <plan_digest>
  hbench version
  hbench list scenarios
  hbench list runs
  hbench ride -s <id> --harness <name> --approve-spend
  hbench run -s <id> --harness <name>
  hbench execute [run_id]
  hbench finish [run_id] [--force]
  hbench report
  hbench publish [run_id]
  hbench inspect -s <scenario>
  hbench trust -s <scenario>
  hbench sandbox-command -s <scenario> --harness <name> --image <name@sha256:digest>
  hbench controlled keygen|validate|run [--runtime auto|docker|podman|nerdctl]
  hbench study validate|plan|run|status|report|publish STUDY.yaml
  hbench callout create STUDY.yaml --statement "..."
  hbench callout challenge <url>
  hbench skill install [--target DIR]
  hbench setup save <id> [flags]
  hbench setup list
  hbench setup show <id>
  hbench scenario new|validate [flags]
  hbench adapter validate PATH
`
}

func cmdFetch(args []string) error {
	if len(args) != 2 || (args[0] != "show" && args[0] != "approve" && args[0] != "revoke") {
		return fmt.Errorf("usage: hbench fetch show|approve|revoke <plan_digest>")
	}
	l := layout()
	digest := args[1]
	switch args[0] {
	case "show":
		plan, err := fetchconsent.LoadPlan(l.DataDir, digest)
		if err != nil {
			return err
		}
		fmt.Print(fetchconsent.Format(plan))
	case "approve":
		if err := fetchconsent.Approve(l.DataDir, digest); err != nil {
			return err
		}
		fmt.Printf("Approved fetch plan %s. Approval applies only to this immutable plan.\n", digest)
	case "revoke":
		if err := fetchconsent.Revoke(l.DataDir, digest); err != nil {
			return err
		}
		fmt.Printf("Revoked fetch plan %s.\n", digest)
	}
	return nil
}

func authorizeFetch(l paths.Layout, plan fetchconsent.Plan) error {
	digest := plan.Digest()
	if fetchconsent.Approved(l.DataDir, digest) {
		return nil
	}
	if err := fetchconsent.SavePlan(l.DataDir, plan); err != nil {
		return err
	}
	fmt.Print(fetchconsent.Format(plan))
	info, _ := os.Stdin.Stat()
	if info == nil || info.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("network fetch requires approval; run: hbench fetch approve %s\nthen rerun the original command", digest)
	}
	fmt.Print("Proceed? [Y/n] ")
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if !approvedFetchAnswer(answer, err) {
		return fmt.Errorf("fetch declined; hbench did not access the network")
	}
	return fetchconsent.Approve(l.DataDir, digest)
}

func approveFetchPlan(l paths.Layout, plan fetchconsent.Plan) error {
	if fetchconsent.Approved(l.DataDir, plan.Digest()) {
		return nil
	}
	if err := fetchconsent.SavePlan(l.DataDir, plan); err != nil {
		return err
	}
	fmt.Print(fetchconsent.Format(plan))
	if err := fetchconsent.Approve(l.DataDir, plan.Digest()); err != nil {
		return err
	}
	fmt.Println("Approved by --yes; approval applies only to this immutable plan.")
	return nil
}

func approvedFetchAnswer(answer string, err error) bool {
	if err != nil {
		return false
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

func cmdReproduce(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: hbench reproduce CALLOUT_URL")
	}
	rawURL := args[0]
	u, err := neturl.Parse(rawURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid Callout URL")
	}
	slug := path.Base(strings.TrimSuffix(u.Path, "/"))
	endpoint := strings.TrimSuffix(rawURL, "/")
	if !strings.Contains(u.Path, "/api/") {
		endpoint = u.Scheme + "://" + u.Host + "/api/v1/callouts/" + slug
	}
	resp, err := http.Get(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Callout API returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		return fmt.Errorf("invalid Callout JSON: %w", err)
	}
	manifest, ok := document["contract"].(map[string]any)
	if !ok {
		return fmt.Errorf("Callout response has no frozen contract")
	}
	id, _ := manifest["id"].(string)
	if id == "" {
		return fmt.Errorf("Callout contract has no study id")
	}
	out := id + ".yaml"
	encoded, err := yaml.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, encoded, 0o600); err != nil {
		return err
	}
	fmt.Printf("Wrote %s. Validate it before running: hbench study validate %s\\n", out, out)
	return nil
}

func cmdAdapter(args []string) error {
	if len(args) != 2 || args[0] != "validate" {
		return fmt.Errorf("usage: hbench adapter validate PATH")
	}
	manifest, digest, err := adapter.Load(args[1])
	if err != nil {
		return err
	}
	fmt.Printf("Valid adapter %s@%s\\ndigest: %s\\ntelemetry: %s\\n", manifest.ID, manifest.Version, digest, manifest.Telemetry)
	return nil
}

func cmdSkill(args []string) error {
	if len(args) == 0 || args[0] != "install" {
		return fmt.Errorf("usage: hbench skill install [--target DIR]")
	}
	fs := flag.NewFlagSet("skill install", flag.ContinueOnError)
	target := fs.String("target", "", "directory that contains installed skills")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *target == "" {
		home, _ := os.UserHomeDir()
		var detected []string
		for _, candidate := range []string{filepath.Join(home, ".codex", "skills"), filepath.Join(home, ".agents", "skills"), filepath.Join(home, ".claude", "skills"), filepath.Join(home, ".pi", "agent", "skills")} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				detected = append(detected, candidate)
			}
		}
		if len(detected) == 0 {
			return fmt.Errorf("no skill root detected; rerun with --target DIR")
		}
		return fmt.Errorf("choose a detected skill root with --target:\n  %s", strings.Join(detected, "\n  "))
	}
	if err := skills.Install(*target); err != nil {
		return err
	}
	fmt.Printf("Installed run-agent-rodeo-study at %s\n", filepath.Join(*target, "run-agent-rodeo-study"))
	return nil
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("value cannot be empty")
	}
	*f = append(*f, value)
	return nil
}

func cmdSetup(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hbench setup save <id> | list | show <id>")
	}
	l := layout()
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: hbench setup list")
		}
		setups, err := loop.ListSetups(l)
		if err != nil {
			return err
		}
		for _, setup := range setups {
			fmt.Printf("%s\t%s\t%s\t%s/%s\t%s\n", setup.ID, setup.Profile.Mode, setup.Profile.Harness, setup.Profile.Provider, setup.Profile.Model, setup.Profile.Workflow)
		}
		return nil
	case "show":
		if len(args) != 2 {
			return fmt.Errorf("usage: hbench setup show <id>")
		}
		setup, err := loop.LoadSetup(l, args[1])
		if err != nil {
			return err
		}
		raw, err := json.MarshalIndent(setup, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	case "save":
		if len(args) < 2 {
			return fmt.Errorf("usage: hbench setup save <id> [flags]")
		}
		return cmdSetupSave(l, args[1], args[2:])
	default:
		return fmt.Errorf("unknown setup command %q; use save, list, or show", args[0])
	}
}

func cmdSetupSave(l paths.Layout, id string, args []string) error {
	fs := flag.NewFlagSet("setup save", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	harness := fs.String("harness", "", "harness: grok, pi, claude, codex, cursor, or manual")
	provider := fs.String("provider", "", "model provider")
	model := fs.String("model", "", "model id")
	reasoning := fs.String("reasoning", "", "reasoning level")
	workflow := fs.String("workflow", "baseline", "workflow label; personal mode records it without claiming adapter enforcement")
	mode := fs.String("mode", "personal", "personal or clean-baseline")
	var skills, extensions, plugins, skillDirs stringListFlag
	fs.Var(&skills, "skill", "exact installed skill package or path (repeatable)")
	fs.Var(&skillDirs, "skill-dir", "local skill directory to freeze on each run (repeatable; Pi only)")
	fs.Var(&extensions, "extension", "exact installed extension package or path (repeatable)")
	fs.Var(&plugins, "plugin", "exact installed plugin package or path (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *harness == "" {
		return fmt.Errorf("--harness is required when saving a setup")
	}
	profile := loop.Profile{ID: id, Mode: *mode, Harness: *harness, Provider: *provider, Model: *model, Reasoning: *reasoning, Workflow: *workflow, Skills: skills, LocalSkills: skillDirs, Extensions: extensions, Plugins: plugins}
	profile, err := loop.NormalizeDirectProfile(profile)
	if err != nil {
		return err
	}
	if err := loop.SaveSetup(l, loop.Setup{ID: id, Name: id, Profile: profile}); err != nil {
		return err
	}
	fmt.Printf("Saved setup %s (%s)\n", id, profile.Mode)
	return nil
}

func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func parseIDArgs(args []string) (id string, force bool, err error) {
	for _, a := range args {
		switch a {
		case "--force":
			force = true
		case "-h", "--help":
			continue
		default:
			if strings.HasPrefix(a, "-") {
				return "", false, fmt.Errorf("unknown flag %s", a)
			}
			if id != "" {
				return "", false, fmt.Errorf("unexpected extra argument %s", a)
			}
			id = a
		}
	}
	return id, force, nil
}

func printHelp() { fmt.Print(usage()) }

func layout() paths.Layout { return paths.Default() }

func ensureCorpus(l paths.Layout) error {
	return corpus.EnsureCache(l.ScenariosDir())
}

func probeSuggestion() (doctor.Suggestion, error) {
	l := layout()
	if err := ensureCorpus(l); err != nil {
		return doctor.Suggestion{}, err
	}
	cwd, _ := os.Getwd()
	snap := doctor.Probe(doctor.LookPATH)
	snap.SkillNames = doctor.ListSkills(doctor.DefaultSkillRoots(l.Home, cwd))
	snap.Scenarios = corpus.DoctorScenarios(l.ScenariosDir())
	return doctor.Suggest(snap), nil
}

func cmdSuggest() error {
	l := layout()
	if rec, err := loop.LatestRecord(l); err == nil {
		switch rec.Status {
		case "pending", "running":
			fmt.Printf("hbench finish %s\n", rec.ID)
			return nil
		case "completed", "failed", "timeout", "budget_exceeded", "setup_failed", "preparing":
			fmt.Println("hbench report")
			return nil
		}
	}
	fmt.Println("hbench ride -s rodeo:js-commander-negative-exp-E@3 --harness pi --model openrouter/z-ai/glm-5.3-flash --approve-spend")
	return nil
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	scenarioID := fs.String("s", "", "scenario id")
	fs.StringVar(scenarioID, "scenario", "", "scenario id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sug, err := probeSuggestion()
	if err != nil {
		return err
	}
	fmt.Print(doctor.Format(sug))
	if *scenarioID != "" {
		l := layout()
		sc, err := corpus.Resolve(l.ScenariosDir(), "", *scenarioID)
		if err != nil {
			return err
		}
		results, checkErr := loop.CheckRequirements(sc)
		fmt.Printf("\nPrerequisites for %s:\n", sc.ID)
		if len(results) == 0 {
			fmt.Println("  (none declared)")
		}
		for _, result := range results {
			state := "ready"
			if result.Problem != "" {
				state = result.Problem
			}
			version := result.Version
			if version == "" {
				version = "version not required"
			}
			fmt.Printf("  %s: %s (%s, %s)\n", result.Name, state, result.Path, version)
		}
		return checkErr
	}
	return nil
}

func cmdList(args []string) error {
	l := layout()
	if err := ensureCorpus(l); err != nil {
		return err
	}
	what := "scenarios"
	if len(args) > 0 {
		what = args[0]
	}
	switch what {
	case "scenarios":
		scs, err := corpus.Load(l.ScenariosDir())
		if err != nil {
			return err
		}
		for _, s := range scs {
			fmt.Printf("%s\t%s\t%s\n", s.ID, s.Language, s.Title)
		}
	case "runs":
		entries, err := os.ReadDir(l.OutDir)
		if err != nil {
			return nil
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			rec, loadErr := loop.Load(l, e.Name())
			if loadErr != nil {
				continue
			}
			fmt.Printf("%s\t%s\t%s\t%s/%s", rec.ID, rec.Status, rec.ScenarioID, rec.Harness, rec.Model)
			switch rec.Status {
			case "pending":
				if loop.HeadlessCommand(rec.Harness) != "" {
					fmt.Printf("\tnext: hbench execute %s", rec.ID)
				} else {
					fmt.Printf("\tnext: hbench finish %s", rec.ID)
				}
			case "preparing", "setup_failed":
				fmt.Print("\tkept for diagnosis; start a new run after fixing the prerequisite")
			case "completed", "failed", "timeout", "budget_exceeded":
				if len(rec.Judges) > 0 {
					fmt.Printf("\tnext: hbench publish --preview %s", rec.ID)
				}
			}
			fmt.Println()
		}
	default:
		return fmt.Errorf("list %s: use scenarios or runs", what)
	}
	return nil
}

func cmdRunMode(args []string, ride bool) error {
	command := "run"
	if ride {
		command = "ride"
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = func() {
		fmt.Fprintf(os.Stdout, `hbench %s -s <scenario> --harness <name>

  -s, --scenario   official scenario id, or a path to a .yaml
  --from           extra directory of scenario YAML (optional packs)
  --harness        grok, pi, claude, codex, cursor, or manual
  --provider       model provider, for example openai
  --model          model id (pi: grok-4.6 uses xAI; x-ai/grok-4.6 uses OpenRouter)
  --reasoning      default, off, minimal, low, medium, high, xhigh, max, or ultra
  --thinking       alias for --reasoning
  --setup          load a saved setup profile from hbench setup save
  --mode           personal or clean-baseline (default: clean-baseline)
  --workflow       workflow label; personal mode records it without claiming adapter enforcement
  --skill          exact installed skill package or path (repeatable)
  --skill-dir      local skill directory to freeze for this run (repeatable; Pi only)
  --runtime        ride runtime: auto, docker, podman, nerdctl, or native
  --relay-image    digest-pinned credential relay image
  --no-setup       skip setup commands in native mode
  --yes            approve the displayed immutable ride plan
  --approve-spend  allow ride to execute the model
  --max-usd        OCI relay spend cap (default 1.00 USD)
  --trust-scenario approve this exact external scenario digest for this run
  --adapter       hb.adapter.v1 manifest for an arbitrary local harness
`, command)
	}
	scenario := fs.String("s", "", "scenario id")
	from := fs.String("from", "", "extra scenario directory")
	harness := fs.String("harness", "", "harness name")
	provider := fs.String("provider", "", "model provider")
	model := fs.String("model", "", "model id")
	reasoning := fs.String("reasoning", "", "reasoning level")
	setupID := fs.String("setup", "", "saved setup profile id")
	mode := fs.String("mode", "", "personal or clean-baseline")
	workflow := fs.String("workflow", "", "workflow label")
	var skills, skillDirs stringListFlag
	fs.Var(&skills, "skill", "exact installed skill package or path (repeatable)")
	fs.Var(&skillDirs, "skill-dir", "local skill directory to freeze for this run (repeatable; Pi only)")
	fs.StringVar(reasoning, "thinking", "", "alias for --reasoning")
	runtimeDefault := "native"
	if ride {
		runtimeDefault = "auto"
	}
	runtimeName := fs.String("runtime", runtimeDefault, "ride runtime: auto, docker, podman, nerdctl, or native")
	relayImage := fs.String("relay-image", defaultRelayImage, "digest-pinned credential relay image")
	noSetup := fs.Bool("no-setup", false, "skip setup commands in native mode")
	yes := fs.Bool("yes", false, "approve the displayed immutable ride plan")
	approveSpend := fs.Bool("approve-spend", false, "allow ride to execute the model")
	maxUSD := fs.Float64("max-usd", 1, "OCI relay spend cap in USD")
	trustScenario := fs.String("trust-scenario", "", "approved external scenario sha256 digest")
	adapterPath := fs.String("adapter", "", "hb.adapter.v1 manifest for an arbitrary local harness")
	fs.StringVar(scenario, "scenario", "", "scenario id")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *scenario == "" {
		fs.Usage()
		return fmt.Errorf("usage: hbench %s -s <scenario> --harness <name>", command)
	}
	if ride && !*approveSpend {
		return fmt.Errorf("hbench ride requires --approve-spend because it executes the model")
	}
	if ride && *harness == "manual" {
		return fmt.Errorf("hbench ride requires a headless harness; use hbench run for manual work")
	}
	printCLIIdentity()
	l := layout()
	if err := ensureCorpus(l); err != nil {
		return err
	}
	sc, err := corpus.Resolve(l.ScenariosDir(), *from, *scenario)
	if err != nil {
		return err
	}
	if sc.External {
		if err := authorizeExternalScenario(l, sc, *trustScenario); err != nil {
			return err
		}
	}
	profile := loop.Profile{Harness: *harness, Provider: *provider, Model: *model, Reasoning: *reasoning, Mode: *mode, Workflow: *workflow, Skills: skills, LocalSkills: skillDirs, AdapterManifest: *adapterPath}
	if *adapterPath != "" {
		manifest, digest, loadErr := adapter.Load(*adapterPath)
		if loadErr != nil {
			return loadErr
		}
		profile.AdapterDigest = digest
		if profile.Harness == "" {
			profile.Harness = "adapter:" + manifest.ID
		}
	}
	if *setupID != "" {
		saved, loadErr := loop.LoadSetup(l, *setupID)
		if loadErr != nil {
			return loadErr
		}
		profile = saved.Profile
		// Direct flags are intentional overrides of the saved recipe.
		if *harness != "" {
			profile.Harness = *harness
		}
		if *provider != "" {
			profile.Provider = *provider
		}
		if *model != "" {
			profile.Model = *model
		}
		if *reasoning != "" {
			profile.Reasoning = *reasoning
		}
		if *mode != "" {
			profile.Mode = *mode
		}
		if *workflow != "" {
			profile.Workflow = *workflow
		}
		profile.Skills = append(profile.Skills, skills...)
		profile.LocalSkills = append(profile.LocalSkills, skillDirs...)
	}
	if profile.Harness == "" {
		fs.Usage()
		return fmt.Errorf("--harness is required unless --setup names a saved setup")
	}
	profile, err = loop.NormalizeDirectProfile(profile)
	if err != nil {
		return err
	}
	if ride && *runtimeName != "native" {
		if *noSetup {
			return fmt.Errorf("--no-setup is available only with --runtime native")
		}
		return cmdOCIRide(l, sc, profile, *runtimeName, *relayImage, *maxUSD, *yes)
	}
	if !ride && *runtimeName != "native" {
		return fmt.Errorf("hbench run is the native compatibility path; use hbench ride --runtime %s", *runtimeName)
	}
	if profile.AdapterManifest != "" {
		profile.HarnessVersion = "adapter/" + profile.AdapterDigest[:12]
	} else if profile.Harness != "manual" {
		identity, err := loop.DetectHarnessIdentity(profile.Harness)
		if err != nil {
			return err
		}
		profile.HarnessVersion = identity.Version
		fmt.Printf("Using harness %s at %s (%s)\n", profile.Harness, identity.Path, identity.Version)
	} else {
		profile.HarnessVersion = "human"
	}
	results, err := loop.CheckRequirements(sc)
	if err != nil {
		for _, result := range results {
			if result.Problem != "" {
				fmt.Printf("  %s: %s (resolved %s)\n", result.Name, result.Problem, result.Path)
			}
		}
		return err
	}
	authorize := func(plan fetchconsent.Plan) error { return authorizeFetch(l, plan) }
	if *yes {
		authorize = func(plan fetchconsent.Plan) error { return approveFetchPlan(l, plan) }
	}
	if err := loop.PrepareInputs(l, sc, !*noSetup, authorize); err != nil {
		return err
	}
	rec, err := loop.CreateRunWithProfile(l, sc, profile, !*noSetup)
	if err != nil {
		if rec.ID != "" {
			fmt.Printf("Run %s status=%s\n", rec.ID, rec.Status)
			fmt.Printf("  report: hbench report\n")
		}
		return err
	}
	fmt.Printf("Run created %s\n", rec.ID)
	fmt.Printf("  status:    %s\n", rec.Status)
	if rec.Model != "" {
		fmt.Printf("  model:     %s\n", rec.Model)
	}
	if profile.Provider != "" {
		fmt.Printf("  provider:  %s\n", profile.Provider)
	}
	if profile.Reasoning != "" {
		fmt.Printf("  reasoning: %s\n", profile.Reasoning)
	}
	fmt.Printf("  workspace: %s\n", rec.Worktree)
	fmt.Printf("  prompt:    %s\n", filepath.Join(rec.Worktree, "HB_PROMPT.txt"))
	if ride {
		return cmdExecute([]string{rec.ID})
	}
	if loop.HeadlessCommand(*harness) != "" {
		fmt.Printf("  next:      hbench execute %s\n", rec.ID)
		fmt.Println("             Run it here or anywhere inside the printed workspace.")
	} else {
		fmt.Printf("  next:      work in the workspace, then hbench finish %s\n", rec.ID)
	}
	return nil
}

func cmdOCIRide(l paths.Layout, sc corpus.Scenario, profile loop.Profile, runtimeName, relayImage string, maxUSD float64, yes bool) error {
	if profile.Mode != "clean-baseline" {
		return fmt.Errorf("OCI rides require --mode clean-baseline; use hbench run --runtime native for personal setup profiles")
	}
	if profile.Workflow != "baseline" || len(profile.Skills) > 0 || len(profile.Extensions) > 0 || len(profile.Plugins) > 0 || len(profile.LocalSkills) > 0 {
		return fmt.Errorf("OCI rides support the clean baseline only; use hbench run --runtime native for workflow or skill experiments")
	}
	if profile.Harness != "pi" || profile.Provider != "openrouter" || profile.Model != "openrouter/z-ai/glm-5.3-flash" {
		return fmt.Errorf("OCI rides currently require --harness pi --model openrouter/z-ai/glm-5.3-flash")
	}
	if maxUSD < 0.02 {
		return fmt.Errorf("--max-usd must be at least 0.02 for the bounded model request")
	}
	if !controlled.PinnedImage(sc.EnvironmentImageDigest) {
		return fmt.Errorf("scenario %s has no digest-pinned OCI environment; use --runtime native only as an advanced compatibility path", sc.ID)
	}
	if !controlled.PinnedImage(sc.RelayImageDigest) {
		return fmt.Errorf("scenario %s has no digest-pinned credential relay", sc.ID)
	}
	if relayImage != sc.RelayImageDigest {
		return fmt.Errorf("--relay-image must match the immutable scenario version")
	}
	rt, err := controlled.SelectRuntime(runtimeName)
	if err != nil {
		return err
	}
	os.Setenv("HB_RUNTIME", rt.Name)
	plan, err := loop.InputPlan(l, sc, false)
	if err != nil {
		return err
	}
	items := append([]fetchconsent.Item(nil), plan.Items...)
	items = append(items,
		fetchconsent.Item{Kind: "Scenario contract", Source: sc.ID, Ref: sc.ManifestDigest, Reason: "immutable ride definition", Destination: l.ScenariosDir(), Size: "metadata only"},
		imagePlanItem("OCI environment", sc.EnvironmentImageDigest, rt.Name+" image store", "sealed scenario tools and dependencies"),
		imagePlanItem("Credential relay", relayImage, rt.Name+" image store", "isolated model API access"),
		fetchconsent.Item{Kind: "Model execution", Source: "https://openrouter.ai", Ref: profile.Model, Reason: "model inference; provider charges apply", Destination: "credential relay", Size: "at most 45 minutes"},
		fetchconsent.Item{Kind: "Reasoning level", Source: profile.Reasoning, Reason: "immutable model execution setting", Destination: "agent container", Size: "metadata only"},
		fetchconsent.Item{Kind: "Spend cap", Source: fmt.Sprintf("%.2f USD", maxUSD), Ref: "100000 request bytes; 32768 output tokens; 0.075/0.25 USD per million input/output tokens", Reason: "relay rejects requests that exceed the approved bound", Destination: "credential relay", Size: "0.02 USD reserved per request"},
		fetchconsent.Item{Kind: "Environment variable", Source: "OPENROUTER_API_KEY", Reason: "provider authentication", Destination: "credential relay secret file", Size: "value is not recorded"},
	)
	fullPlan := fetchconsent.New(items...)
	if yes {
		err = approveFetchPlan(l, fullPlan)
	} else {
		err = authorizeFetch(l, fullPlan)
	}
	if err != nil {
		return err
	}
	if err := loop.PrepareApprovedInputs(l, sc, false); err != nil {
		return err
	}

	if err := os.MkdirAll(l.OutDir, 0o755); err != nil {
		return err
	}
	evaluator, err := os.MkdirTemp(l.OutDir, ".hbench-public-evaluator-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(evaluator)
	goldDir := filepath.Join(evaluator, "gold")
	goldFiles, err := loop.ExportGoldTests(l, sc, goldDir)
	if err != nil {
		return err
	}
	commands := make([]string, 0, len(goldFiles)+len(sc.Acceptance.BuildCommands)+len(sc.Acceptance.TestCommands))
	for _, file := range goldFiles {
		source := path.Join("/evaluator/gold", filepath.ToSlash(file))
		destination := path.Join("/workspace", filepath.ToSlash(file))
		commands = append(commands, "mkdir -p "+shellQuote(path.Dir(destination))+" && cat "+shellQuote(source)+" > "+shellQuote(destination))
	}
	commands = append(commands, sc.Acceptance.BuildCommands...)
	commands = append(commands, sc.Acceptance.TestCommands...)
	if len(commands) == 0 {
		return fmt.Errorf("scenario %s has no public evaluator commands", sc.ID)
	}
	modelID := strings.TrimPrefix(profile.Model, profile.Provider+"/")
	pack := controlled.Pack{
		ScenarioSlug: strings.SplitN(sc.ID, "@", 2)[0], ScenarioVersion: sc.Version,
		EnvironmentImageDigest: sc.EnvironmentImageDigest, RelayImageDigest: relayImage, ProtocolID: "controlled-v3",
		EvaluatorCommands: commands,
		Execution:         controlled.Execution{Harness: "pi", HarnessVersion: "0.84.4", Provider: profile.Provider, Model: profile.Model, Reasoning: profile.Reasoning, Command: "hbench-pi-openrouter", Environment: map[string]string{"HOME": "/tmp/hbench-home", "HB_MODEL": modelID, "HB_REASONING": profile.Reasoning}},
		Relay: controlled.Relay{
			Upstream: "https://openrouter.ai", BaseURLEnv: "HB_MODEL_BASE_URL", SecretEnv: "OPENROUTER_API_KEY", AuthHeader: "Authorization", AuthScheme: "Bearer", DummyKeyEnv: "HB_MODEL_API_KEY",
			AllowedModel: modelID, MaxRequestUSD: 0.02, MaxRequestBytes: 100000, MaxOutputTokens: 32768,
			MaxPromptUSDPerMillion: 0.075, MaxCompletionUSDPerMillion: 0.25,
		},
		Budget: map[string]any{"max_minutes": 45, "max_usd": maxUSD},
	}
	runID := loop.NewID()
	artifactDir := l.RunDir(runID)
	workspace, err := loop.PrepareWorktree(l, sc, runID, false)
	if err != nil {
		return err
	}
	ctx, cancel := controlled.RunTimeout(45)
	defer cancel()
	result, runErr := controlled.Run(ctx, sc, pack, evaluator, relayImage, artifactDir, workspace)
	if runErr != nil {
		return runErr
	}
	return saveOCIRide(l, sc, profile, runID, rt, relayImage, maxUSD, result)
}

func imagePlanItem(kind, image, destination, reason string) fetchconsent.Item {
	parts := strings.SplitN(image, "@", 2)
	return fetchconsent.Item{Kind: kind, Source: parts[0], Ref: parts[1], Checksum: parts[1], Reason: reason, Destination: destination, Size: "unknown"}
}

func saveOCIRide(l paths.Layout, sc corpus.Scenario, profile loop.Profile, runID string, rt controlled.Runtime, relayImage string, maxUSD float64, result controlled.RunResult) error {
	run, ok := result.Payload["run"].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid OCI run result")
	}
	status, _ := run["status"].(string)
	telemetry, _ := run["telemetry"].(loop.Telemetry)
	passed, _ := result.Report["passed"].(bool)
	judge := loop.JudgeScore{Name: "acceptance_tests", Score: 0, Passed: &passed, Notes: "Containerized public evaluator failed."}
	if passed {
		judge.Score = 1
		judge.Notes = "Containerized public evaluator passed."
	}
	record := loop.RunRecord{
		ID: runID, ScenarioID: sc.ID, ConfigID: "oci-" + rt.Name, Status: status, Worktree: l.Worktree(runID),
		Harness: profile.Harness, HarnessVersion: "0.84.4", Model: profile.Model,
		Judges: []loop.JudgeScore{judge}, Telemetry: telemetry, CreatedAt: loop.Now(), FinishedAt: loop.Now(),
	}
	if err := loop.WriteFileAtomic(filepath.Join(l.RunDir(runID), "patch.diff"), []byte(result.Patch), 0o600); err != nil {
		return err
	}
	promptDigest := sha256.Sum256([]byte(strings.TrimSpace(sc.Prompt)))
	snapshot := map[string]any{
		"prompt_sha256_16": fmt.Sprintf("%x", promptDigest)[:16],
		"repo":             map[string]any{"base_ref": sc.Repo.BaseRef, "gold_ref": sc.Repo.GoldRef},
		"config": map[string]any{
			"id": record.ConfigID, "mode": "clean-baseline", "harness": profile.Harness, "harness_version": record.HarnessVersion,
			"provider": profile.Provider, "model": profile.Model, "reasoning": profile.Reasoning,
			"workflow": "baseline", "interaction": "unattended", "budget": map[string]any{"max_minutes": 45, "max_usd": maxUSD},
			"environment": map[string]any{"image_digest": sc.EnvironmentImageDigest}, "relay_image_digest": relayImage, "network": "relay-only", "runtime": rt,
		},
	}
	if raw, err := json.MarshalIndent(snapshot, "", "  "); err != nil {
		return err
	} else if err := loop.WriteFileAtomic(filepath.Join(l.RunDir(runID), "snapshot.json"), append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if raw, err := os.ReadFile(result.LogPath); err == nil {
		_ = loop.WriteFileAtomic(filepath.Join(l.RunDir(runID), "agent.log"), raw, 0o600)
	}
	if err := loop.Save(l, record); err != nil {
		return err
	}
	if err := loop.SetLatest(l, runID); err != nil {
		return err
	}
	printScored(l, record, false)
	return nil
}

func printCLIIdentity() {
	path, err := os.Executable()
	if err != nil {
		path = "unknown"
	}
	fmt.Printf("Using hbench %s at %s\n", version, path)
}

func authorizeExternalScenario(l paths.Layout, sc corpus.Scenario, supplied string) error {
	digest, err := corpus.TrustDigest(sc)
	if err != nil {
		return err
	}
	if supplied != "" {
		if supplied != digest {
			return fmt.Errorf("scenario trust digest mismatch: got %s, want %s", supplied, digest)
		}
		return nil
	}
	if trust.IsTrusted(l.DataDir, digest) {
		return nil
	}
	fmt.Printf("External scenario %s requires trust before any setup, agent, or test command runs.\n", sc.ID)
	printScenarioInspection(sc, digest)
	info, _ := os.Stdin.Stat()
	if info == nil || info.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("external scenario is not trusted; inspect it, then pass --trust-scenario %s", digest)
	}
	fmt.Print("Type once to run now, or remember to trust this digest: ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(answer)) {
	case "once":
		return nil
	case "remember":
		return trust.Remember(l.DataDir, digest, sc.ID)
	default:
		return fmt.Errorf("scenario trust declined")
	}
}

func resolveScenarioFlag(args []string, command string) (paths.Layout, corpus.Scenario, error) {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	scenario := fs.String("s", "", "scenario id, rodeo:slug@version, or YAML path")
	from := fs.String("from", "", "extra scenario directory")
	fs.StringVar(scenario, "scenario", "", "scenario id, rodeo:slug@version, or YAML path")
	if err := fs.Parse(args); err != nil {
		return paths.Layout{}, corpus.Scenario{}, err
	}
	if *scenario == "" {
		return paths.Layout{}, corpus.Scenario{}, fmt.Errorf("usage: hbench %s -s <scenario>", command)
	}
	l := layout()
	if err := ensureCorpus(l); err != nil {
		return l, corpus.Scenario{}, err
	}
	sc, err := corpus.Resolve(l.ScenariosDir(), *from, *scenario)
	return l, sc, err
}

func printScenarioInspection(sc corpus.Scenario, digest string) {
	fmt.Printf("  trust digest: %s\n", digest)
	fmt.Printf("  repository:   %s\n  base ref:     %s\n  gold ref:     %s\n", sc.Repo.URL, sc.Repo.BaseRef, sc.Repo.GoldRef)
	fmt.Printf("  image:        %s\n  network:      %s\n", sc.EnvironmentImageDigest, sc.NetworkPolicy)
	fmt.Println("  prerequisites:")
	if len(sc.Requirements.Commands) == 0 {
		fmt.Println("    (none declared)")
	}
	for _, requirement := range sc.Requirements.Commands {
		version := ""
		if requirement.MinimumVersion != "" {
			version = " >= " + requirement.MinimumVersion
		}
		fmt.Printf("    %s%s — %s\n", requirement.Name, version, requirement.Purpose)
	}
	fmt.Println("  consented fetches:")
	if sc.Repo.URL != "" {
		fmt.Printf("    git %s @ %s\n", sc.Repo.URL, sc.Repo.BaseRef)
	}
	if len(sc.Fetches) == 0 && sc.Repo.URL == "" {
		fmt.Println("    (none declared)")
	}
	for _, fetch := range sc.Fetches {
		fmt.Printf("    %s %s — %s\n", fetch.Kind, fetch.Lockfile, fetch.Reason)
	}
	for _, item := range []struct {
		name     string
		commands []string
	}{
		{"setup", sc.Acceptance.SetupCommands}, {"build", sc.Acceptance.BuildCommands}, {"tests", sc.Acceptance.TestCommands},
	} {
		fmt.Printf("  %s commands:\n", item.name)
		if len(item.commands) == 0 {
			fmt.Println("    (none)")
		}
		for _, command := range item.commands {
			fmt.Printf("    %s\n", command)
		}
	}
}

func cmdInspect(args []string) error {
	_, sc, err := resolveScenarioFlag(args, "inspect")
	if err != nil {
		return err
	}
	digest, err := corpus.TrustDigest(sc)
	if err != nil {
		return err
	}
	printScenarioInspection(sc, digest)
	return nil
}

func cmdTrust(args []string) error {
	l, sc, err := resolveScenarioFlag(args, "trust")
	if err != nil {
		return err
	}
	if !sc.External {
		return fmt.Errorf("embedded scenarios are already trusted")
	}
	digest, err := corpus.TrustDigest(sc)
	if err != nil {
		return err
	}
	printScenarioInspection(sc, digest)
	if err := trust.Remember(l.DataDir, digest, sc.ID); err != nil {
		return err
	}
	fmt.Printf("Remembered trust for %s at digest %s\n", sc.ID, digest)
	return nil
}

func cmdSandboxCommand(args []string) error {
	fs := flag.NewFlagSet("sandbox-command", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	scenario := fs.String("s", "", "scenario identifier available inside the image")
	harness := fs.String("harness", "", "harness name")
	image := fs.String("image", "", "dependency-complete hbench image pinned by sha256 digest")
	engine := fs.String("engine", "docker", "docker or podman")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scenario == "" || *harness == "" || (*engine != "docker" && *engine != "podman") {
		return fmt.Errorf("usage: hbench sandbox-command -s <scenario> --harness <name> --image <name@sha256:digest>")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._/:@-]+@sha256:[0-9a-f]{64}$`).MatchString(*image) {
		return fmt.Errorf("--image must be pinned by sha256 digest")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	resultDir := filepath.Join(cwd, "hb-out")
	fmt.Printf("%s run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges --pids-limit 256 --memory 4g --cpus 2 --tmpfs /tmp:rw,noexec,nosuid,size=512m -e HB_OUT_DIR=/results -v %s:/results:rw %s hbench run -s %s --harness %s\n",
		*engine, shellQuote(resultDir), shellQuote(*image), shellQuote(*scenario), shellQuote(*harness))
	fmt.Println("# Printed only; hbench did not start the container. The pinned image must contain the scenario repository and dependencies.")
	return nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func cmdExecute(args []string) error {
	if wantsHelp(args) {
		fmt.Print(`hbench execute [run_id]

  Launch a headless harness for this run, then judge.
  Manual runs have no headless launch — use hbench finish instead.
`)
		return nil
	}
	l := layout()
	id, _, err := parseIDArgs(args)
	if err != nil {
		return err
	}
	id, err = loop.ResolveRunID(l, id)
	if err != nil {
		return err
	}
	rec, err := loop.Load(l, id)
	if err != nil {
		return err
	}
	adapterLaunch := false
	if profile, ok := rec.Metadata["profile"].(map[string]any); ok {
		_, adapterLaunch = profile["adapter_manifest"].(string)
	}
	if loop.HeadlessCommand(rec.Harness) == "" && !adapterLaunch {
		return fmt.Errorf("%s has no headless launch; stay in this directory and run: hbench finish %s", rec.Harness, id)
	}
	fmt.Printf("Executing run %s (this may spend tokens)\n", id)
	res, err := loop.Execute(l, id, 45*time.Minute)
	if err != nil && res.LogPath == "" {
		return err
	}
	fmt.Printf("  exit=%d wall_ms=%d log=%s\n", res.ReturnCode, res.WallMS, res.LogPath)
	if res.TimedOut {
		return err
	}
	if err := ensureCorpus(l); err != nil {
		return err
	}
	sc, ferr := loop.ScenarioForRun(l, l.ScenariosDir(), id)
	if ferr != nil {
		return ferr
	}
	finished, ferr := loop.Finish(l, id, sc, res.WallMS, "hb execute", true)
	if ferr != nil {
		return ferr
	}
	printScored(l, finished, false)
	return err
}

func cmdFinish(args []string) error {
	if wantsHelp(args) {
		fmt.Print(`hbench finish [run_id] [--force]

  Capture the workspace patch and judge it.
  Stay in the directory where you ran hbench run.
  A completed run is not re-judged unless you pass --force.
`)
		return nil
	}
	l := layout()
	id, force, err := parseIDArgs(args)
	if err != nil {
		return err
	}
	id, err = loop.ResolveRunID(l, id)
	if err != nil {
		return err
	}
	if err := ensureCorpus(l); err != nil {
		return err
	}
	rec, err := loop.Load(l, id)
	if err != nil {
		return err
	}
	if loop.AlreadyScored(rec) && !force {
		printScored(l, rec, true)
		return nil
	}
	sc, err := loop.ScenarioForRun(l, l.ScenariosDir(), rec.ID)
	if err != nil {
		return err
	}
	finished, err := loop.Finish(l, id, sc, rec.Telemetry.WallMS, "hb finish", force)
	if err != nil {
		return err
	}
	printScored(l, finished, false)
	return nil
}

func printScored(l paths.Layout, rec loop.RunRecord, reused bool) {
	q := loop.Quality(rec)
	if reused {
		fmt.Printf("Already scored %s status=%s quality=%.2f (not re-judged)\n", rec.ID, rec.Status, q)
	} else {
		fmt.Printf("Finished %s status=%s quality=%.2f\n", rec.ID, rec.Status, q)
	}
	if len(rec.Judges) > 0 {
		fmt.Println("  judges:")
		for _, j := range rec.Judges {
			mark := "—"
			if j.Passed != nil {
				if *j.Passed {
					mark = "pass"
				} else {
					mark = "fail"
				}
			}
			line := fmt.Sprintf("    %-20s %s  %.2f", j.Name, mark, j.Score)
			if j.Notes != "" {
				line += "  " + j.Notes
			}
			fmt.Println(line)
		}
	}
	if reused {
		fmt.Printf("  re-judge:  hbench finish --force %s\n", rec.ID)
	}
	if path, _, err := report.Write(l); err == nil {
		fmt.Printf("  report:    %s\n", path)
		fmt.Printf("             open %s\n", path)
	}
	fmt.Printf("  optional:  hbench publish %s\n", rec.ID)
}

func cmdReport(args []string) error {
	if wantsHelp(args) {
		fmt.Println("hbench report  — write local HTML in hb-out (does not upload)")
		return nil
	}
	l := layout()
	if !paths.IsStore(l.OutDir) {
		cwd, _ := os.Getwd()
		if cwd == "" {
			cwd = l.Start
		}
		return fmt.Errorf("no hb-out here or in parent directories (looked from %s)\nnothing was uploaded — state lives in ./hb-out next to where you ran hbench run", cwd)
	}
	path, n, err := report.Write(l)
	if err != nil {
		return err
	}
	fmt.Printf("Wrote %s (%d runs). Nothing was uploaded.\n", path, n)
	fmt.Printf("  open %s\n", path)
	return nil
}

func cmdPublish(args []string) error {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	preview := fs.Bool("preview", false, "print the exact public payload without uploading")
	fs.Usage = func() {
		fmt.Println("hbench publish [--preview] [run_id]  — explicitly upload a privacy-filtered run")
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return fmt.Errorf("too many run ids")
	}
	if wantsHelp(args) {
		fs.Usage()
		return nil
	}
	l := layout()
	id := ""
	if fs.NArg() > 0 {
		id = fs.Arg(0)
	}
	id, err := loop.ResolveRunID(l, id)
	if err != nil {
		return err
	}
	if *preview {
		payload, err := publish.SignedPayload(l, id)
		if err != nil {
			return err
		}
		raw, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		fmt.Println("Nothing was uploaded.")
		return nil
	}
	out, err := publish.Publish(l, id, nil)
	if err != nil {
		return err
	}
	rodeoURL, _ := publish.ValidatedRodeoURL()
	fmt.Printf("Published %s to %s\n", id, rodeoURL)
	if publicURL, ok := out["url"].(string); ok && publicURL != "" {
		fmt.Printf("  run: %s\n", publicURL)
	}
	if v, ok := out["id"]; ok {
		fmt.Printf("  remote id: %v\n", v)
	}
	return nil
}
