package loop

import (
	"bytes"
	"encoding/json"
	"os"
)

// ExtractTelemetry parses usage emitted by supported headless harnesses.
// It is deliberately best-effort: telemetry must never decide whether a ride
// itself succeeds or fails.
func ExtractTelemetry(harness, logPath string) Telemetry {
	raw, err := os.ReadFile(logPath)
	if err != nil {
		return Telemetry{}
	}
	switch harness {
	case "grok":
		return withTotalTokens(grokTelemetry(raw))
	case "pi":
		return withTotalTokens(piTelemetry(raw))
	default:
		return Telemetry{}
	}
}

type grokLog struct {
	Usage struct {
		TotalTokens              int `json:"total_tokens"`
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		ReasoningTokens          int `json:"reasoning_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	CostUSD    *float64 `json:"costUSD"`
	ModelUsage map[string]struct {
		ModelCalls int      `json:"modelCalls"`
		CostUSD    *float64 `json:"costUSD"`
	} `json:"modelUsage"`
}

func grokTelemetry(raw []byte) Telemetry {
	var log grokLog
	if json.Unmarshal(raw, &log) != nil {
		return Telemetry{}
	}
	t := Telemetry{}
	seen := false
	if log.Usage.InputTokens != 0 || log.Usage.OutputTokens != 0 ||
		log.Usage.ReasoningTokens != 0 || log.Usage.CacheReadInputTokens != 0 ||
		log.Usage.CacheCreationInputTokens != 0 {
		t.TokensIn = intPointer(log.Usage.InputTokens)
		t.TokensOut = intPointer(log.Usage.OutputTokens)
		t.ReasoningTokens = intPointer(log.Usage.ReasoningTokens)
		t.CacheReadTokens = intPointer(log.Usage.CacheReadInputTokens)
		t.CacheWriteTokens = intPointer(log.Usage.CacheCreationInputTokens)
		seen = true
	}
	if log.Usage.TotalTokens > 0 {
		t.TotalTokens = intPointer(log.Usage.TotalTokens)
		seen = true
	}

	cost := 0.0
	turns := 0
	costSeen := false
	for _, usage := range log.ModelUsage {
		turns += usage.ModelCalls
		if usage.CostUSD != nil {
			cost += *usage.CostUSD
			costSeen = true
		}
	}
	if turns > 0 {
		t.Turns = intPointer(turns)
		seen = true
	}
	if costSeen {
		t.EstimatedUSD = floatPointer(cost)
		seen = true
	} else if log.CostUSD != nil {
		t.EstimatedUSD = floatPointer(*log.CostUSD)
		seen = true
	}
	if !seen {
		return Telemetry{}
	}
	return t
}

type piLogLine struct {
	Type    string `json:"type"`
	Message struct {
		Usage *struct {
			Total      int `json:"totalTokens"`
			Input      int `json:"input"`
			Output     int `json:"output"`
			CacheRead  int `json:"cacheRead"`
			CacheWrite int `json:"cacheWrite"`
			Reasoning  int `json:"reasoning"`
			Cost       *struct {
				Total float64 `json:"total"`
			} `json:"cost"`
		} `json:"usage"`
	} `json:"message"`
}

func piTelemetry(raw []byte) Telemetry {
	input, output, reasoning := 0, 0, 0
	cacheRead, cacheWrite, turns := 0, 0, 0
	nativeTotal := 0
	cost := 0.0
	costSeen := false

	for _, rawLine := range bytes.Split(raw, []byte{'\n'}) {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 {
			continue
		}
		var event piLogLine
		if json.Unmarshal(line, &event) != nil || event.Type != "message_end" || event.Message.Usage == nil {
			continue
		}
		usage := event.Message.Usage
		input += usage.Input
		output += usage.Output
		reasoning += usage.Reasoning
		cacheRead += usage.CacheRead
		cacheWrite += usage.CacheWrite
		if usage.Total > 0 {
			nativeTotal += usage.Total
		}
		turns++
		if usage.Cost != nil {
			cost += usage.Cost.Total
			costSeen = true
		}
	}
	if turns == 0 {
		return Telemetry{}
	}
	t := Telemetry{
		TokensIn:         intPointer(input),
		TokensOut:        intPointer(output),
		Turns:            intPointer(turns),
		ReasoningTokens:  intPointer(reasoning),
		CacheReadTokens:  intPointer(cacheRead),
		CacheWriteTokens: intPointer(cacheWrite),
	}
	if nativeTotal > 0 {
		t.TotalTokens = intPointer(nativeTotal)
	}
	if costSeen {
		t.EstimatedUSD = floatPointer(cost)
	}
	return t
}

func intPointer(value int) *int { return &value }

func floatPointer(value float64) *float64 { return &value }

func withTotalTokens(t Telemetry) Telemetry {
	if t.TotalTokens != nil {
		return t
	}
	parts := []*int{t.TokensIn, t.TokensOut, t.ReasoningTokens, t.CacheReadTokens, t.CacheWriteTokens}
	total, seen := 0, false
	for _, part := range parts {
		if part != nil {
			total += *part
			seen = true
		}
	}
	if seen {
		t.TotalTokens = intPointer(total)
	}
	return t
}
