package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/clayton/harness-benchmark/internal/controlled"
	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/doctor"
	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
	"github.com/clayton/harness-benchmark/internal/publish"
	"github.com/clayton/harness-benchmark/internal/report"
	"github.com/clayton/harness-benchmark/internal/trust"
)

const version = "0.4.2"

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
		return cmdDoctor()
	case "help", "-h", "--help":
		printHelp()
		return nil
	case "list":
		return cmdList(args[1:])
	case "run":
		return cmdRun(args[1:])
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
	relayImage := fs.String("relay-image", os.Getenv("HB_RELAY_IMAGE"), "pinned credential relay image")
	artifactDir := fs.String("artifacts", filepath.Join(layout().OutDir, "controlled"), "private artifact directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scenarioID == "" || *packPath == "" || *keyID == "" {
		return fmt.Errorf("--scenario, --pack, and --key-id are required")
	}
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
	if value, ok := pack.Budget["max_minutes"].(int); ok {
		minutes = value
	}
	ctx, cancel := controlled.RunTimeout(minutes)
	defer cancel()
	if action == "validate" {
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
	if *relayImage == "" || !controlled.PinnedImage(*relayImage) {
		return fmt.Errorf("--relay-image must be pinned by sha256 digest")
	}
	result, err := controlled.Run(ctx, scenario, pack, *packPath, *relayImage, *artifactDir)
	if err != nil {
		return err
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

func defaultRunnerKey() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "hb", "runner-ed25519.pem")
}

func usage() string {
	return `hbench — compare coding-agent systems on fixed, repeatable tasks.

Commands:
  hbench                      print one suggested command
  hbench doctor               what this machine has
  hbench version
  hbench list scenarios
  hbench list runs
  hbench run -s <id> --harness <name>
  hbench execute [run_id]
  hbench finish [run_id] [--force]
  hbench report
  hbench publish [run_id]
  hbench inspect -s <scenario>
  hbench trust -s <scenario>
  hbench sandbox-command -s <scenario> --harness <name> --image <name@sha256:digest>
  hbench controlled keygen|validate|run
`
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
		case "completed", "failed":
			fmt.Println("hbench report")
			return nil
		}
	}
	sug, err := probeSuggestion()
	if err != nil {
		return err
	}
	if sug.Command == "" {
		fmt.Println("hbench doctor")
		return nil
	}
	fmt.Println(sug.Command)
	return nil
}

func cmdDoctor() error {
	sug, err := probeSuggestion()
	if err != nil {
		return err
	}
	fmt.Print(doctor.Format(sug))
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
			if e.IsDir() {
				fmt.Println(e.Name())
			}
		}
	default:
		return fmt.Errorf("list %s: use scenarios or runs", what)
	}
	return nil
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = func() {
		fmt.Fprint(os.Stdout, `hbench run -s <scenario> --harness <name>

  -s, --scenario   official scenario id, or a path to a .yaml
  --from           extra directory of scenario YAML (optional packs)
  --harness        grok, pi, claude, codex, cursor, or manual
  --model          model id (pi: grok-4.6 uses xAI; x-ai/grok-4.6 uses OpenRouter)
  --no-setup       skip setup commands
	  --trust-scenario approve this exact external scenario digest for this run
`)
	}
	scenario := fs.String("s", "", "scenario id")
	from := fs.String("from", "", "extra scenario directory")
	harness := fs.String("harness", "", "harness name")
	model := fs.String("model", "", "model id")
	noSetup := fs.Bool("no-setup", false, "skip setup commands")
	trustScenario := fs.String("trust-scenario", "", "approved external scenario sha256 digest")
	fs.StringVar(scenario, "scenario", "", "scenario id")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *scenario == "" || *harness == "" {
		fs.Usage()
		return fmt.Errorf("usage: hbench run -s <scenario> --harness <name>")
	}
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
	rec, err := loop.CreateRunWithModel(l, sc, *harness, *model, !*noSetup)
	if err != nil {
		return err
	}
	fmt.Printf("Run created %s\n", rec.ID)
	fmt.Printf("  status:    %s\n", rec.Status)
	if rec.Model != "" {
		fmt.Printf("  model:     %s\n", rec.Model)
	}
	fmt.Printf("  workspace: %s\n", rec.Worktree)
	fmt.Printf("  prompt:    %s\n", filepath.Join(rec.Worktree, "HB_PROMPT.txt"))
	if loop.HeadlessCommand(*harness) != "" {
		fmt.Printf("  next:      stay in this directory; hbench execute %s\n", rec.ID)
	} else {
		fmt.Printf("  next:      stay in this directory; work in the workspace, then hbench finish %s\n", rec.ID)
	}
	return nil
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
	if loop.HeadlessCommand(rec.Harness) == "" {
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
		payload, err := publish.BuildPayload(l, id)
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
	if v, ok := out["id"]; ok {
		fmt.Printf("  remote id: %v\n", v)
	}
	return nil
}
