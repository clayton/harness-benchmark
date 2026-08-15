package loop

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clayton/harness-benchmark/internal/paths"
)

func TestExtractGrokTelemetry(t *testing.T) {
	path := writeTelemetryFixture(t, `{
  "usage": {
    "input_tokens": 80,
    "output_tokens": 16,
    "reasoning_tokens": 14,
    "cache_read_input_tokens": 1200,
    "cache_creation_input_tokens": 3
  },
  "modelUsage": {
    "grok-a": {"modelCalls": 20, "costUSD": 0.9},
    "grok-b": {"modelCalls": 1, "costUSD": 0.012408}
  }
}`)

	got := ExtractTelemetry("grok", path)
	assertIntPointer(t, "tokens in", got.TokensIn, 80)
	assertIntPointer(t, "tokens out", got.TokensOut, 16)
	assertIntPointer(t, "reasoning", got.ReasoningTokens, 14)
	assertIntPointer(t, "cache read", got.CacheReadTokens, 1200)
	assertIntPointer(t, "cache write", got.CacheWriteTokens, 3)
	assertIntPointer(t, "turns", got.Turns, 21)
	assertFloatPointer(t, "cost", got.EstimatedUSD, 0.912408)
}

func TestExtractPiTelemetry(t *testing.T) {
	path := writeTelemetryFixture(t, `{"type":"message_end","message":{"usage":{"input":12,"output":3,"cacheRead":20,"cacheWrite":2,"reasoning":4,"cost":{"total":0.05}}}}
{"type":"message_end","message":{}}
not json
{"type":"message_end","message":{"usage":{"input":8,"output":2,"cacheRead":10,"cacheWrite":1,"reasoning":6,"cost":{"total":0.03}}}}
`)

	got := ExtractTelemetry("pi", path)
	assertIntPointer(t, "tokens in", got.TokensIn, 20)
	assertIntPointer(t, "tokens out", got.TokensOut, 5)
	assertIntPointer(t, "reasoning", got.ReasoningTokens, 10)
	assertIntPointer(t, "cache read", got.CacheReadTokens, 30)
	assertIntPointer(t, "cache write", got.CacheWriteTokens, 3)
	assertIntPointer(t, "turns", got.Turns, 2)
	assertFloatPointer(t, "cost", got.EstimatedUSD, 0.08)
}

func TestExtractTelemetryIsBestEffort(t *testing.T) {
	path := writeTelemetryFixture(t, "plain text without usage")
	if got := ExtractTelemetry("cursor", path); got != (Telemetry{}) {
		t.Fatalf("cursor telemetry = %+v", got)
	}
	if got := ExtractTelemetry("grok", path); got != (Telemetry{}) {
		t.Fatalf("malformed grok telemetry = %+v", got)
	}
}

func TestExecutePersistsExtractedTelemetry(t *testing.T) {
	root := t.TempDir()
	worktree := filepath.Join(root, "workspace")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeGrok := filepath.Join(binDir, "grok")
	if err := os.WriteFile(fakeGrok, []byte(`#!/bin/sh
printf '%s\n' '{"usage":{"input_tokens":10,"output_tokens":2},"modelUsage":{"grok-test":{"modelCalls":1,"costUSD":0.04}}}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	l := paths.New(root, root)
	rec := RunRecord{
		ID:        "telemetry-run",
		Status:    "pending",
		Worktree:  worktree,
		Harness:   "grok",
		Model:     "grok-test",
		CreatedAt: Now(),
	}
	if err := Save(l, rec); err != nil {
		t.Fatal(err)
	}
	result, err := Execute(l, rec.ID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReturnCode != 0 {
		t.Fatalf("return code = %d", result.ReturnCode)
	}
	saved, err := Load(l, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertIntPointer(t, "persisted tokens in", saved.Telemetry.TokensIn, 10)
	assertIntPointer(t, "persisted tokens out", saved.Telemetry.TokensOut, 2)
	assertIntPointer(t, "persisted turns", saved.Telemetry.Turns, 1)
	assertFloatPointer(t, "persisted cost", saved.Telemetry.EstimatedUSD, 0.04)
	if saved.Telemetry.WallMS < 0 {
		t.Fatalf("persisted wall time = %d", saved.Telemetry.WallMS)
	}
}

func writeTelemetryFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertIntPointer(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %d", name, got, want)
	}
}

func assertFloatPointer(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-12 {
		t.Fatalf("%s = %v, want %f", name, got, want)
	}
}
