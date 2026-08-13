package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/doctor"
	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
	"github.com/clayton/harness-benchmark/internal/publish"
	"github.com/clayton/harness-benchmark/internal/report"
)

const version = "0.2.1"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return cmdDoctor()
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("hb %s (go)\n", version)
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
		return cmdReport()
	case "publish":
		return cmdPublish(args[1:])
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

func usage() string {
	return `hb — compare coding-agent systems on fixed official tasks.

Commands:
  hb                 doctor: what you have + one suggested command
  hb version
  hb list scenarios
  hb run -s <id> --harness <name>
  hb execute [run_id]
  hb finish [run_id]
  hb report
  hb publish [run_id]
`
}

func printHelp() { fmt.Print(usage()) }

func layout() paths.Layout { return paths.Default() }

func ensureCorpus(l paths.Layout) error {
	return corpus.EnsureCache(l.ScenariosDir())
}

func cmdDoctor() error {
	l := layout()
	if err := ensureCorpus(l); err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	snap := doctor.Probe(doctor.LookPATH)
	snap.SkillNames = doctor.ListSkills(doctor.DefaultSkillRoots(l.Home, cwd))
	snap.Scenarios = corpus.DoctorScenarios(l.ScenariosDir())
	fmt.Print(doctor.Format(doctor.Suggest(snap)))
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
	scenario := fs.String("s", "", "scenario id")
	harness := fs.String("harness", "", "harness name")
	noSetup := fs.Bool("no-setup", false, "skip setup commands")
	fs.StringVar(scenario, "scenario", "", "scenario id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scenario == "" || *harness == "" {
		return fmt.Errorf("usage: hb run -s <scenario> --harness <name>")
	}
	l := layout()
	if err := ensureCorpus(l); err != nil {
		return err
	}
	sc, err := corpus.Find(l.ScenariosDir(), *scenario)
	if err != nil {
		return err
	}
	rec, err := loop.CreateRun(l, sc, *harness, !*noSetup)
	if err != nil {
		return err
	}
	fmt.Printf("Run created %s\n", rec.ID)
	fmt.Printf("  status:    %s\n", rec.Status)
	fmt.Printf("  workspace: %s\n", rec.Worktree)
	fmt.Printf("  prompt:    %s\n", filepath.Join(rec.Worktree, "HB_PROMPT.txt"))
	if loop.HeadlessCommand(*harness) != "" {
		fmt.Printf("  next:      hb execute %s\n", rec.ID)
	} else {
		fmt.Printf("  next:      work in the workspace, then hb finish %s\n", rec.ID)
	}
	return nil
}

func cmdExecute(args []string) error {
	l := layout()
	id := ""
	if len(args) > 0 {
		id = args[0]
	}
	id, err := loop.ResolveRunID(l, id)
	if err != nil {
		return err
	}
	fmt.Printf("Executing run %s (this may spend tokens)\n", id)
	res, err := loop.Execute(l, id, 45*time.Minute)
	if err != nil && res.LogPath == "" {
		return err
	}
	fmt.Printf("  agent exit=%d wall_ms=%d log=%s\n", res.ReturnCode, res.WallMS, res.LogPath)
	scID := ""
	if rec, e := loop.Load(l, id); e == nil {
		scID = rec.ScenarioID
	}
	if err := ensureCorpus(l); err != nil {
		return err
	}
	sc, ferr := corpus.Find(l.ScenariosDir(), scID)
	if ferr != nil {
		return ferr
	}
	finished, ferr := loop.Finish(l, id, sc, res.WallMS, "hb execute")
	if ferr != nil {
		return ferr
	}
	fmt.Printf("Finished %s status=%s quality=%.2f\n", finished.ID, finished.Status, loop.Quality(finished))
	fmt.Printf("  report: hb report\n")
	fmt.Printf("  optional: hb publish %s\n", finished.ID)
	return err
}

func cmdFinish(args []string) error {
	l := layout()
	id := ""
	if len(args) > 0 {
		id = args[0]
	}
	id, err := loop.ResolveRunID(l, id)
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
	sc, err := corpus.Find(l.ScenariosDir(), rec.ScenarioID)
	if err != nil {
		return err
	}
	finished, err := loop.Finish(l, id, sc, rec.Telemetry.WallMS, "hb finish")
	if err != nil {
		return err
	}
	fmt.Printf("Finished %s status=%s quality=%.2f\n", finished.ID, finished.Status, loop.Quality(finished))
	return nil
}

func cmdReport() error {
	l := layout()
	if _, err := os.Stat(l.OutDir); err != nil {
		fmt.Println("No ./hb-out yet. Nothing was uploaded.")
		return nil
	}
	path, n, err := report.Write(l)
	if err != nil {
		return err
	}
	fmt.Printf("Wrote %s (%d runs). Nothing was uploaded.\n", path, n)
	return nil
}

func cmdPublish(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("hb publish [run_id]  — upload a finished run to agentrodeo.dev (not automatic)")
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
