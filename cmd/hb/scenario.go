package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/clayton/harness-benchmark/internal/corpus"
)

func cmdScenario(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hbench scenario new|validate")
	}
	if args[0] == "-h" || args[0] == "--help" {
		fmt.Println("usage: hbench scenario new|validate")
		return nil
	}
	switch args[0] {
	case "new":
		return cmdScenarioNew(args[1:])
	case "validate":
		return cmdScenarioValidate(args[1:])
	default:
		return fmt.Errorf("unknown scenario command %q; use new or validate", args[0])
	}
}

func cmdScenarioNew(args []string) error {
	fs := flag.NewFlagSet("scenario new", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	out := fs.String("out", "", "output YAML path")
	id := fs.String("id", "", "scenario slug or slug@version")
	title := fs.String("title", "", "scenario title")
	prompt := fs.String("prompt", "", "agent intent prompt")
	typ := fs.String("type", "rewrite", "bugfix, feature, refactor, rewrite, or endurance")
	language := fs.String("language", "", "implementation language")
	difficulty := fs.String("difficulty", "", "difficulty label")
	filesJSON := fs.String("files-json", "", "starter scaffold files as a JSON object")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *out == "" || *id == "" || *title == "" || *prompt == "" || *language == "" || *filesJSON == "" {
		return fmt.Errorf("usage: hbench scenario new --out PATH --id ID --title TITLE --prompt TEXT --language LANG --files-json JSON")
	}
	files, err := corpus.ParseWorkspaceFiles(*filesJSON)
	if err != nil {
		return err
	}
	scenario, err := corpus.NewScaffoldScenario(corpus.NewScenarioOptions{ID: *id, Title: *title, Prompt: *prompt, Type: *typ, Language: *language, Difficulty: *difficulty}, files)
	if err != nil {
		return err
	}
	if err := corpus.WriteScenario(*out, scenario); err != nil {
		return err
	}
	fmt.Printf("Wrote scaffold scenario %s\n", *out)
	return nil
}

func cmdScenarioValidate(args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: hbench scenario validate PATH")
	}
	if _, err := os.Stat(args[0]); err != nil {
		return err
	}
	scenario, err := corpus.ValidateScenarioFile(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Valid scenario %s (%s)\n", scenario.ID, scenario.Type)
	return nil
}
