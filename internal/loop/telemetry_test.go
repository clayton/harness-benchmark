package loop

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/clayton/harness-benchmark/internal/paths"
)

func TestExtractGrokTelemetry(t *testing.T) {
	path := writeTelemetryFixture(t, `{
  "usage": {
    "total_tokens": 999,
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
	assertIntPointer(t, "total", got.TotalTokens, 999)
	assertFloatPointer(t, "cost", got.EstimatedUSD, 0.912408)
}

func TestExtractPiTelemetry(t *testing.T) {
	path := writeTelemetryFixture(t, `{"type":"message_end","message":{"usage":{"totalTokens":40,"input":12,"output":3,"cacheRead":20,"cacheWrite":2,"reasoning":4,"cost":{"total":0.05}}}}
{"type":"message_end","message":{}}
not json
{"type":"message_end","message":{"usage":{"totalTokens":50,"input":8,"output":2,"cacheRead":10,"cacheWrite":1,"reasoning":6,"cost":{"total":0.03}}}}
`)

	got := ExtractTelemetry("pi", path)
	assertIntPointer(t, "tokens in", got.TokensIn, 20)
	assertIntPointer(t, "tokens out", got.TokensOut, 5)
	assertIntPointer(t, "reasoning", got.ReasoningTokens, 10)
	assertIntPointer(t, "cache read", got.CacheReadTokens, 30)
	assertIntPointer(t, "cache write", got.CacheWriteTokens, 3)
	assertIntPointer(t, "turns", got.Turns, 2)
	assertIntPointer(t, "total", got.TotalTokens, 90)
	assertFloatPointer(t, "cost", got.EstimatedUSD, 0.08)
	if got.CostKind != "estimated" || got.Complete == nil || *got.Complete {
		t.Fatalf("Pi catalog cost must stay incomplete estimated telemetry: %+v", got)
	}
}

func TestFreezePiPriceSnapshotCompletesCatalogCost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models-store.json")
	if err := os.WriteFile(path, []byte(`{"openrouter":{"checkedAt":123,"models":[{"id":"stealth/ox-alpha","cost":{"input":0,"output":0}}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	input, output, cost := 10, 2, 0.0
	complete := false
	telemetry := Telemetry{TokensIn: &input, TokensOut: &output, EstimatedUSD: &cost, CostKind: "estimated", Complete: &complete}
	freezePiPriceSnapshot(&telemetry, path, "openrouter", "stealth/ox-alpha")
	if telemetry.Complete == nil || !*telemetry.Complete || !strings.Contains(telemetry.PriceSnapshot, `"checked_at":123`) {
		t.Fatalf("telemetry=%+v", telemetry)
	}
}

func TestExtractPiTelemetryAggregatesSubagentResults(t *testing.T) {
	path := writeTelemetryFixture(t, `{"type":"message_end","message":{"usage":{"totalTokens":40,"input":12,"output":3,"cost":{"total":0.05}}}}
{"type":"tool_result","details":{"results":[{"runId":"child-123","model":"gpt-5.6-luna","totalTokens":50,"totalCost":0.2}]}}`)
	got := ExtractTelemetry("pi", path)
	assertIntPointer(t, "total", got.TotalTokens, 90)
	assertFloatPointer(t, "cost", got.EstimatedUSD, 0.25)
	if got.Complete == nil || *got.Complete || got.UsageByAgent == nil || len(*got.UsageByAgent) != 2 {
		t.Fatalf("subagent telemetry=%+v", got)
	}
	if (*got.UsageByAgent)[1].TotalTokens != 50 || (*got.UsageByAgent)[1].Model != "gpt-5.6-luna" {
		t.Fatalf("child usage=%+v", (*got.UsageByAgent)[1])
	}
}

func TestExtractCursor202608ResultTelemetry(t *testing.T) {
	path := writeTelemetryFixture(t, `{"type":"result","usage":{"inputTokens":12,"outputTokens":3,"cacheReadTokens":7,"cacheWriteTokens":2}}`)
	got := ExtractTelemetry("cursor", path)
	assertIntPointer(t, "tokens in", got.TokensIn, 12)
	assertIntPointer(t, "tokens out", got.TokensOut, 3)
	assertIntPointer(t, "cache read", got.CacheReadTokens, 7)
	assertIntPointer(t, "cache write", got.CacheWriteTokens, 2)
	if got.Complete == nil || *got.Complete || got.EstimatedUSD != nil {
		t.Fatalf("Cursor result has tokens but no dollar cost: %+v", got)
	}
}

func TestTelemetryWithoutTokenFieldsIsIncomplete(t *testing.T) {
	for _, fixture := range []struct {
		harness string
		log     string
	}{
		{"pi", `{"type":"message_end","message":{"usage":{"cost":{"total":0.01}}}}`},
		{"cursor", `{"usage":{"cost_usd":0.01}}`},
		{"grok", `{"costUSD":0.01}`},
	} {
		got := ExtractTelemetry(fixture.harness, writeTelemetryFixture(t, fixture.log))
		if got.Complete == nil || *got.Complete {
			t.Fatalf("%s telemetry should be explicitly incomplete: %+v", fixture.harness, got)
		}
	}
}

func TestFallbackTotalDoesNotDoubleCountCacheOrReasoning(t *testing.T) {
	tokensIn, tokensOut, cache, reasoning := 100, 20, 80, 10
	got := withTotalTokens(Telemetry{TokensIn: &tokensIn, TokensOut: &tokensOut, CacheReadTokens: &cache, ReasoningTokens: &reasoning})
	assertIntPointer(t, "total", got.TotalTokens, 120)
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
	l := paths.New(root, root)
	worktree := l.Worktree("feedfacecafe")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "HB_PROMPT.txt"), []byte("test prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeGrok := filepath.Join(binDir, "grok")
	if err := os.WriteFile(fakeGrok, []byte(`#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' 'grok 1.2.3'
  exit 0
fi
printf '%s\n' '{"usage":{"input_tokens":10,"output_tokens":2},"modelUsage":{"grok-test":{"modelCalls":1,"costUSD":0.04}}}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	rec := RunRecord{
		ID:        "feedfacecafe",
		Status:    "pending",
		Worktree:  worktree,
		Harness:   "grok",
		Model:     "grok-test",
		CreatedAt: Now(),
	}
	if err := Save(l, rec); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.RunDir(rec.ID), "snapshot.json"), []byte(`{"config":{"harness":"grok"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Execute(l, rec.ID, 45*time.Minute)
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
	if saved.HarnessVersion != "grok 1.2.3" {
		t.Fatalf("harness version = %q", saved.HarnessVersion)
	}
	snapshotRaw, err := os.ReadFile(filepath.Join(l.RunDir(rec.ID), "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(snapshotRaw), `"max_minutes_per_run": 45`) {
		t.Fatalf("snapshot missing execution budget: %s", snapshotRaw)
	}
	if saved.Telemetry.WallMS < 0 {
		t.Fatalf("persisted wall time = %d", saved.Telemetry.WallMS)
	}
}

func TestExecuteTimeoutKillsTheHarnessProcessGroup(t *testing.T) {
	root := t.TempDir()
	l := paths.New(root, root)
	id := "badc0ffeeeee"
	worktree := l.Worktree(id)
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "HB_PROMPT.txt"), []byte("wait"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(binDir, "grok")
	script := "#!/bin/sh\nif [ \"${1:-}\" = --version ]; then echo test; exit 0; fi\nsleep 30 &\necho $! > child.pid\nwait\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := Save(l, RunRecord{ID: id, Status: "pending", Worktree: worktree, Harness: "grok", CreatedAt: Now()}); err != nil {
		t.Fatal(err)
	}
	result, err := Execute(l, id, 300*time.Millisecond)
	if err == nil || !result.TimedOut {
		t.Fatalf("timeout result=%+v err=%v", result, err)
	}
	saved, loadErr := Load(l, id)
	if loadErr != nil || saved.Status != "timeout" {
		t.Fatalf("saved=%+v err=%v", saved, loadErr)
	}
	raw, readErr := os.ReadFile(filepath.Join(worktree, "child.pid"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	time.Sleep(100 * time.Millisecond)
	if killErr := syscall.Kill(pid, 0); killErr == nil {
		t.Fatalf("child process %d survived timeout", pid)
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
