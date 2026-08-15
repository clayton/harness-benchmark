package loop

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
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
		return withTokenCompleteness(withTotalTokens(grokTelemetry(raw)))
	case "pi":
		return withTokenCompleteness(withTotalTokens(piTelemetry(raw)))
	case "codex":
		return withTokenCompleteness(withTotalTokens(codexTelemetry(raw)))
	case "cursor":
		return withTokenCompleteness(withTotalTokens(cursorTelemetry(raw)))
	default:
		return Telemetry{}
	}
}

func withTokenCompleteness(t Telemetry) Telemetry {
	if t.TokensIn == nil && t.TokensOut == nil && t.TotalTokens == nil {
		return t
	}
	complete := t.TokensIn != nil && t.TokensOut != nil && t.TotalTokens != nil
	t.TokenComplete = &complete
	return t
}

func cursorTelemetry(raw []byte) Telemetry {
	input, output, cached, cacheWrite, reasoning := 0, 0, 0, 0, 0
	cost := 0.0
	seen, inputSeen, outputSeen, costSeen := false, false, false, false
	for _, rawLine := range bytes.Split(raw, []byte{'\n'}) {
		var event map[string]any
		if json.Unmarshal(bytes.TrimSpace(rawLine), &event) != nil {
			continue
		}
		usage := findUsageMap(event)
		if usage == nil {
			continue
		}
		input += firstJSONInt(usage, "input_tokens", "inputTokens", "input")
		output += firstJSONInt(usage, "output_tokens", "outputTokens", "output")
		inputSeen = inputSeen || hasJSONKey(usage, "input_tokens", "inputTokens", "input")
		outputSeen = outputSeen || hasJSONKey(usage, "output_tokens", "outputTokens", "output")
		cached += firstJSONInt(usage, "cache_read_tokens", "cached_input_tokens", "cacheReadTokens", "cacheRead")
		cacheWrite += firstJSONInt(usage, "cache_write_tokens", "cacheWriteTokens", "cacheWrite")
		reasoning += firstJSONInt(usage, "reasoning_tokens", "reasoningTokens", "reasoning")
		if value, ok := firstJSONFloat(usage, "cost_usd", "costUSD", "totalCost"); ok {
			cost += value
			costSeen = true
		}
		seen = true
	}
	if !seen {
		return Telemetry{}
	}
	complete := inputSeen && outputSeen && costSeen
	usageByAgent := []AgentUsage{{AgentID: "parent", TokensIn: input, TokensOut: output, CacheReadTokens: cached, ReasoningTokens: reasoning}}
	t := Telemetry{TokensIn: intPointer(input), TokensOut: intPointer(output), CacheReadTokens: intPointer(cached), CacheWriteTokens: intPointer(cacheWrite), ReasoningTokens: intPointer(reasoning), Complete: &complete, UsageByAgent: &usageByAgent}
	if costSeen {
		t.EstimatedUSD = floatPointer(cost)
		t.CostKind = "actual"
		(*t.UsageByAgent)[0].EstimatedUSD = floatPointer(cost)
	}
	return t
}

func hasJSONKey(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func findUsageMap(value map[string]any) map[string]any {
	for _, key := range []string{"usage", "token_usage", "tokenUsage"} {
		if item, ok := value[key].(map[string]any); ok {
			return item
		}
	}
	for _, key := range []string{"message", "result", "data"} {
		if nested, ok := value[key].(map[string]any); ok {
			if found := findUsageMap(nested); found != nil {
				return found
			}
		}
	}
	return nil
}
func firstJSONInt(m map[string]any, keys ...string) int {
	for _, key := range keys {
		if _, ok := m[key]; ok {
			return jsonInt(m[key])
		}
	}
	return 0
}
func firstJSONFloat(m map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		switch value := m[key].(type) {
		case float64:
			return value, true
		case json.Number:
			n, err := value.Float64()
			return n, err == nil
		}
	}
	return 0, false
}

func codexTelemetry(raw []byte) Telemetry {
	input, output, cached, reasoning := 0, 0, 0, 0
	seen := false
	for _, rawLine := range bytes.Split(raw, []byte{'\n'}) {
		var event map[string]any
		if json.Unmarshal(bytes.TrimSpace(rawLine), &event) != nil {
			continue
		}
		usage, ok := event["usage"].(map[string]any)
		if !ok {
			continue
		}
		input += jsonInt(usage["input_tokens"])
		output += jsonInt(usage["output_tokens"])
		cached += jsonInt(usage["cached_input_tokens"])
		reasoning += jsonInt(usage["reasoning_output_tokens"])
		seen = true
	}
	if !seen {
		return Telemetry{}
	}
	complete := false
	usageByAgent := []AgentUsage{{AgentID: "parent", TokensIn: input, TokensOut: output, CacheReadTokens: cached, ReasoningTokens: reasoning}}
	t := Telemetry{TokensIn: intPointer(input), TokensOut: intPointer(output), CacheReadTokens: intPointer(cached), ReasoningTokens: intPointer(reasoning), Complete: &complete, UsageByAgent: &usageByAgent}
	return t
}

func jsonInt(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
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
	complete := t.TokensIn != nil && t.TokensOut != nil && t.EstimatedUSD != nil
	t.Complete = &complete
	t.CostKind = "actual"
	t.UsageByAgent = agentUsagePointer([]AgentUsage{{AgentID: "parent", TokensIn: valueOrZero(t.TokensIn), TokensOut: valueOrZero(t.TokensOut), ReasoningTokens: valueOrZero(t.ReasoningTokens), CacheReadTokens: valueOrZero(t.CacheReadTokens), EstimatedUSD: t.EstimatedUSD}})
	return t
}

type piLogLine struct {
	Type    string `json:"type"`
	Message struct {
		Usage *struct {
			Total      *int `json:"totalTokens"`
			Input      *int `json:"input"`
			Output     *int `json:"output"`
			CacheRead  int  `json:"cacheRead"`
			CacheWrite int  `json:"cacheWrite"`
			Reasoning  int  `json:"reasoning"`
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
	children := map[string]AgentUsage{}

	for _, rawLine := range bytes.Split(raw, []byte{'\n'}) {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 {
			continue
		}
		var generic any
		if json.Unmarshal(line, &generic) == nil {
			collectPiChildUsage(generic, children)
		}
		var event piLogLine
		if json.Unmarshal(line, &event) != nil || event.Type != "message_end" || event.Message.Usage == nil {
			continue
		}
		usage := event.Message.Usage
		if usage.Input != nil {
			input += *usage.Input
		}
		if usage.Output != nil {
			output += *usage.Output
		}
		reasoning += usage.Reasoning
		cacheRead += usage.CacheRead
		cacheWrite += usage.CacheWrite
		if usage.Total != nil && *usage.Total > 0 {
			nativeTotal += *usage.Total
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
	parentCost, parentCostSeen := cost, costSeen
	childIDs := make([]string, 0, len(children))
	for id := range children {
		childIDs = append(childIDs, id)
	}
	sort.Strings(childIDs)
	usageByAgent := []AgentUsage{{AgentID: "parent", TokensIn: input, TokensOut: output, ReasoningTokens: reasoning, CacheReadTokens: cacheRead}}
	if parentCostSeen {
		usageByAgent[0].EstimatedUSD = floatPointer(parentCost)
	}
	for _, id := range childIDs {
		child := children[id]
		usageByAgent = append(usageByAgent, child)
		nativeTotal += child.TotalTokens
		if child.EstimatedUSD != nil {
			cost += *child.EstimatedUSD
		} else {
			costSeen = false
		}
	}
	// Pi calculates this value from its model catalog. It is useful evidence,
	// but it is not a billed amount and this log does not freeze the rates.
	complete := false
	t := Telemetry{
		TokensIn:         intPointer(input),
		TokensOut:        intPointer(output),
		Turns:            intPointer(turns),
		ReasoningTokens:  intPointer(reasoning),
		CacheReadTokens:  intPointer(cacheRead),
		CacheWriteTokens: intPointer(cacheWrite),
		Complete:         &complete,
		UsageByAgent:     agentUsagePointer(usageByAgent),
	}
	if nativeTotal > 0 {
		t.TotalTokens = intPointer(nativeTotal)
	}
	if costSeen {
		t.EstimatedUSD = floatPointer(cost)
		t.CostKind = "estimated"
	}
	return t
}

func collectPiChildUsage(value any, found map[string]AgentUsage) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectPiChildUsage(item, found)
		}
	case map[string]any:
		id, _ := typed["runId"].(string)
		if id == "" {
			id, _ = typed["run_id"].(string)
		}
		if id != "" {
			total := firstJSONInt(typed, "totalTokens", "total_tokens")
			cost, hasCost := firstJSONFloat(typed, "totalCost", "total_cost")
			if total > 0 || hasCost {
				usage := AgentUsage{AgentID: "child-" + sanitizeAgentID(id), TotalTokens: total}
				usage.Model, _ = typed["model"].(string)
				if hasCost {
					usage.EstimatedUSD = floatPointer(cost)
				}
				found[id] = usage
			}
		}
		for _, child := range typed {
			collectPiChildUsage(child, found)
		}
	}
}

func sanitizeAgentID(value string) string {
	var out []byte
	for i := 0; i < len(value) && len(out) < 48; i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}

func intPointer(value int) *int { return &value }
func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func floatPointer(value float64) *float64 { return &value }

func agentUsagePointer(value []AgentUsage) *[]AgentUsage { return &value }

func withTotalTokens(t Telemetry) Telemetry {
	if t.TotalTokens != nil {
		return t
	}
	parts := []*int{t.TokensIn, t.TokensOut}
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
