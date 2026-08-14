package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/doctor"
	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
	"github.com/clayton/harness-benchmark/internal/publish"
	"github.com/clayton/harness-benchmark/internal/report"
)

const version = "0.3.2"

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
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

func usage() string {
	return `hbench — compare coding-agent systems on fixed official tasks.

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
`)
	}
	scenario := fs.String("s", "", "scenario id")
	from := fs.String("from", "", "extra scenario directory")
	harness := fs.String("harness", "", "harness name")
	model := fs.String("model", "", "model id")
	noSetup := fs.Bool("no-setup", false, "skip setup commands")
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
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("hbench publish [run_id]  — upload a finished run to agentrodeo.dev (not automatic)")
		return nil
	}
	l := layout()
	id := ""
	if len(args) > 0 {
		id = args[0]
	}
	id, err := loop.ResolveRunID(l, id)
	if err != nil {
		return err
	}
	out, err := publish.Publish(l, id, nil)
	if err != nil {
		return err
	}
	fmt.Printf("Published %s to %s\n", id, publish.RodeoURL())
	if v, ok := out["id"]; ok {
		fmt.Printf("  remote id: %v\n", v)
	}
	return nil
}
