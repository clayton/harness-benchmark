"""Parse token/cost telemetry from harness agent logs (pi, grok, …)."""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

from hb.models import Telemetry


def _as_int(value: Any) -> int | None:
    if value is None:
        return None
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def _as_float(value: Any) -> float | None:
    if value is None:
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def parse_pi_jsonl(text: str) -> Telemetry:
    """Sum usage from pi `--mode json` NDJSON stream."""
    tokens_in = 0
    tokens_out = 0
    tokens_cache = 0
    cost = 0.0
    turns = 0
    tool_calls = 0
    saw_usage = False

    for line in text.splitlines():
        line = line.strip()
        if not line or not line.startswith("{"):
            continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not isinstance(obj, dict):
            continue
        t = obj.get("type")
        if t == "turn_end":
            turns += 1
        if t == "tool_execution_start" or t == "tool_start":
            tool_calls += 1
        # usage lives on message_end / turn_end assistant messages
        msg = obj.get("message") if isinstance(obj.get("message"), dict) else None
        if msg is None and obj.get("role") == "assistant":
            msg = obj
        if not isinstance(msg, dict):
            continue
        usage = msg.get("usage")
        if not isinstance(usage, dict):
            continue
        # Prefer final non-zero usage; sum per-turn if multiple
        tin = _as_int(usage.get("input")) or 0
        tout = _as_int(usage.get("output")) or 0
        cache = (_as_int(usage.get("cacheRead")) or 0) + (_as_int(usage.get("cacheWrite")) or 0)
        c = usage.get("cost") if isinstance(usage.get("cost"), dict) else {}
        ctotal = _as_float(c.get("total")) or 0.0
        if tin or tout or ctotal:
            saw_usage = True
            tokens_in += tin
            tokens_out += tout
            tokens_cache += cache
            cost += ctotal

    if not saw_usage:
        return Telemetry()
    return Telemetry(
        tokens_in=tokens_in or None,
        tokens_out=tokens_out or None,
        tokens_cache=tokens_cache or None,
        estimated_usd=cost or None,
        turns=turns or None,
        tool_calls=tool_calls or None,
    )


def parse_grok_json(text: str) -> Telemetry:
    """Parse grok `--output-format json` final object (or last JSON object in stream)."""
    # Prefer a full-file JSON object
    text = text.strip()
    candidates: list[dict] = []
    try:
        obj = json.loads(text)
        if isinstance(obj, dict):
            candidates.append(obj)
    except json.JSONDecodeError:
        # Try last JSON object in the log (may have preamble)
        for match in re.finditer(r"\{[\s\S]*\}", text):
            try:
                obj = json.loads(match.group(0))
                if isinstance(obj, dict):
                    candidates.append(obj)
            except json.JSONDecodeError:
                continue
        # Also NDJSON lines
        for line in text.splitlines():
            line = line.strip()
            if not line.startswith("{"):
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError:
                continue
            if isinstance(obj, dict) and ("usage" in obj or "total_cost_usd" in obj):
                candidates.append(obj)

    if not candidates:
        return Telemetry()

    # Use the last candidate with usage
    chosen = None
    for obj in reversed(candidates):
        if "usage" in obj or "total_cost_usd" in obj or "modelUsage" in obj:
            chosen = obj
            break
    if chosen is None:
        chosen = candidates[-1]

    usage = chosen.get("usage") if isinstance(chosen.get("usage"), dict) else {}
    tokens_in = _as_int(usage.get("input_tokens"))
    tokens_out = _as_int(usage.get("output_tokens"))
    tokens_cache = _as_int(usage.get("cache_read_input_tokens")) or 0
    tokens_cache += _as_int(usage.get("cache_creation_input_tokens")) or 0
    cost = _as_float(chosen.get("total_cost_usd"))
    turns = _as_int(chosen.get("num_turns"))

    # Fallback modelUsage aggregation
    if tokens_in is None and isinstance(chosen.get("modelUsage"), dict):
        tin = tout = cache = 0
        csum = 0.0
        for mu in chosen["modelUsage"].values():
            if not isinstance(mu, dict):
                continue
            tin += _as_int(mu.get("inputTokens")) or 0
            tout += _as_int(mu.get("outputTokens")) or 0
            cache += _as_int(mu.get("cacheReadInputTokens")) or 0
            cache += _as_int(mu.get("cacheCreationInputTokens")) or 0
            csum += _as_float(mu.get("costUSD")) or 0.0
        tokens_in = tin or None
        tokens_out = tout or None
        tokens_cache = cache or None
        if cost is None:
            cost = csum or None

    return Telemetry(
        tokens_in=tokens_in,
        tokens_out=tokens_out,
        tokens_cache=tokens_cache or None,
        estimated_usd=cost,
        turns=turns,
    )


def parse_agent_log(path: Path, harness: str | None = None) -> Telemetry:
    """Auto-detect harness format from log contents / harness name."""
    if not path.exists():
        return Telemetry()
    text = path.read_text(encoding="utf-8", errors="replace")
    harness = (harness or "").lower()

    # Explicit formats
    if harness == "pi" or '"type":"agent_start"' in text or '"type": "agent_start"' in text:
        tel = parse_pi_jsonl(text)
        if tel.tokens_in or tel.estimated_usd:
            return tel
    if harness == "grok" or "total_cost_usd" in text or "input_tokens" in text:
        tel = parse_grok_json(text)
        if tel.tokens_in or tel.estimated_usd:
            return tel

    # Try both
    for parser in (parse_pi_jsonl, parse_grok_json):
        tel = parser(text)
        if tel.tokens_in or tel.tokens_out or tel.estimated_usd:
            return tel
    return Telemetry()


def merge_telemetry(base: Telemetry, extra: Telemetry) -> Telemetry:
    """Fill null fields on base from extra."""
    data = base.model_dump()
    for key, value in extra.model_dump().items():
        if data.get(key) is None and value is not None:
            data[key] = value
    return Telemetry.model_validate(data)
