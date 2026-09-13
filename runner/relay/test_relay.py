import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path


class RelayTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        root = Path(cls.temp.name)
        secret = root / "secret"
        client = root / "client"
        secret.write_text("upstream-secret", encoding="utf-8")
        client.write_text("client-secret", encoding="utf-8")
        os.environ.update({
            "UPSTREAM": "https://openrouter.ai/api",
            "AUTH_FILE": str(secret),
            "CLIENT_TOKEN_FILE": str(client),
            "ALLOWED_MODEL": "provider/model",
            "MAX_REQUEST_BYTES": "10000",
            "MAX_OUTPUT_TOKENS": "100",
            "MAX_PROMPT_USD_PER_MILLION": "1",
            "MAX_COMPLETION_USD_PER_MILLION": "2",
            "MAX_USD": "0",
        })
        path = Path(__file__).with_name("relay.py")
        spec = importlib.util.spec_from_file_location("hbench_relay", path)
        cls.relay = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.relay)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_bounds_chat_and_responses_without_shell_or_model_fallback(self):
        for endpoint in ("/api/v1/chat/completions", "/v1/chat/completions", "/api/v1/responses", "/v1/responses"):
            raw = json.dumps({"model": "provider/model", "input": "hello"}).encode()
            bounded = json.loads(self.relay.bounded_request(endpoint, raw))
            token_key = "max_output_tokens" if endpoint.endswith("/responses") else "max_tokens"
            self.assertEqual(100, bounded[token_key])
            self.assertEqual(endpoint.endswith("/chat/completions"), "provider" in bounded)
        with self.assertRaisesRegex(ValueError, "approved model"):
            self.relay.bounded_request("/v1/responses", b'{"model":"other"}')
        with self.assertRaisesRegex(ValueError, "output-token"):
            self.relay.bounded_request("/v1/responses", b'{"model":"provider/model","max_output_tokens":101}')
        with self.assertRaisesRegex(ValueError, "endpoint-compatible"):
            self.relay.bounded_request("/v1/responses", b'{"model":"provider/model","max_tokens":10}')
        with self.assertRaisesRegex(ValueError, "endpoint-compatible"):
            self.relay.bounded_request("/v1/chat/completions", b'{"model":"provider/model","max_output_tokens":10}')
        with self.assertRaisesRegex(ValueError, "not supported"):
            self.relay.bounded_request("/v1/models", b"{}")

    def test_controlled_allowlist_excludes_unbounded_cursor_endpoints(self):
        for endpoint in ("/auth/exchange_user_api_key", "/agent.v1.AgentService/RunSSE", "/aiserver.v1.BidiService/BidiAppend"):
            self.assertIsNone(self.relay.ALLOWED_ENDPOINT.match(endpoint))

    def test_prices_json_and_streaming_usage(self):
        self.assertEqual(0.25, self.relay.response_cost(b'{"usage":{"cost":0.25}}'))
        stream = b'data: {"usage":{"cost":0.5}}\n\ndata: [DONE]\n'
        self.assertEqual(0.5, self.relay.response_cost(stream))
        self.assertIsNone(self.relay.response_cost(b"not-json"))


if __name__ == "__main__":
    unittest.main()
