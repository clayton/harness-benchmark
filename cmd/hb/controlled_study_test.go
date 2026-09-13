package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clayton/harness-benchmark/internal/controlled"
	"github.com/clayton/harness-benchmark/internal/corpus"
	studycontract "github.com/clayton/harness-benchmark/internal/study"
	"gopkg.in/yaml.v3"
)

func TestControlledStudyCellBindsExactFrozenSetupBeforeExecution(t *testing.T) {
	digest := strings.Repeat("a", 64)
	manifest := studycontract.Manifest{
		Schema: studycontract.SchemaV2, ID: "controlled-study", Question: "Which model wins?", ComparisonMode: "controlled",
		Scenarios: []studycontract.Scenario{{ID: "rodeo:task@1", Digest: digest}},
		Arms: []studycontract.Arm{
			{ID: "a", Mode: "clean-baseline", Harness: "pi", Version: "0.85.1", Provider: "openai", Model: "model-a", ModelVersion: "2026-09", Reasoning: "high", Workflow: "baseline", Network: "none"},
			{ID: "b", Mode: "clean-baseline", Harness: "pi", Version: "0.85.1", Provider: "openai", Model: "model-b", ModelVersion: "2026-09", Reasoning: "high", Workflow: "baseline", Network: "none"},
		},
		VariedAxes: []string{"model"}, Repeats: 2, Seed: 7, JudgeProtocol: "scenario-default", WinRule: studycontract.WinRule,
		Budget: studycontract.Budget{MaxMinutes: 30, MaxTokens: intPointer(10_000), MaxUSDPerRun: floatPointer(2)},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "study.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	pack := controlled.Pack{
		EnvironmentImageDigest: "example.test/environment@sha256:" + strings.Repeat("b", 64),
		Execution:              controlled.Execution{Harness: "pi", HarnessVersion: "0.85.1", Provider: "openai", Model: "model-a", ModelVersion: "2026-09", Reasoning: "high"},
		Budget:                 map[string]any{"max_minutes": 30, "max_usd": 2.0},
	}
	scenario := corpus.Scenario{ID: "task@1", ManifestDigest: digest}
	binding, config, err := controlledStudyCell(path, "a", "rodeo:task@1", 2, scenario, pack)
	if err != nil {
		t.Fatal(err)
	}
	if binding["contract_digest"] != manifest.Digest() || binding["repeat"] != 2 {
		t.Fatalf("binding=%v", binding)
	}
	budget := config["budget"].(map[string]any)
	if config["judge_protocol"] != "scenario-default" || budget["max_minutes_per_run"] != 30 || budget["max_tokens_per_run"] != 10_000 || budget["max_usd_per_run"] != float64(2) {
		t.Fatalf("config=%v", config)
	}

	pack.Execution.Model = "model-b"
	if _, _, err := controlledStudyCell(path, "a", "rodeo:task@1", 1, scenario, pack); err == nil {
		t.Fatal("mismatched controlled model was accepted")
	}
	pack.Execution.Model = "model-a"
	pack.Budget["max_minutes"] = 31
	if _, _, err := controlledStudyCell(path, "a", "rodeo:task@1", 1, scenario, pack); err == nil {
		t.Fatal("looser controlled timeout was accepted")
	}
}

func intPointer(value int) *int           { return &value }
func floatPointer(value float64) *float64 { return &value }
