from hb.telemetry import parse_grok_json, parse_pi_jsonl


def test_parse_pi_jsonl_usage():
    sample = """
{"type":"session","version":3}
{"type":"turn_end","message":{"role":"assistant","usage":{"input":100,"output":20,"cacheRead":5,"cacheWrite":0,"totalTokens":125,"cost":{"input":0.01,"output":0.002,"cacheRead":0.0,"cacheWrite":0,"total":0.012}}}}
{"type":"turn_end","message":{"role":"assistant","usage":{"input":50,"output":10,"cacheRead":0,"cacheWrite":0,"totalTokens":60,"cost":{"total":0.005}}}}
"""
    tel = parse_pi_jsonl(sample)
    assert tel.tokens_in == 150
    assert tel.tokens_out == 30
    assert tel.tokens_cache == 5
    assert tel.estimated_usd == 0.017
    assert tel.turns == 2


def test_parse_grok_json_usage():
    sample = """
{
  "text": "ok",
  "usage": {
    "input_tokens": 1000,
    "cache_read_input_tokens": 100,
    "cache_creation_input_tokens": 0,
    "output_tokens": 50,
    "total_tokens": 1150
  },
  "num_turns": 3,
  "total_cost_usd": 0.02
}
"""
    tel = parse_grok_json(sample)
    assert tel.tokens_in == 1000
    assert tel.tokens_out == 50
    assert tel.tokens_cache == 100
    assert tel.estimated_usd == 0.02
    assert tel.turns == 3
