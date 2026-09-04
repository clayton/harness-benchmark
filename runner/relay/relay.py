import http.client
import json
import os
import re
import ssl
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

UPSTREAM = urlsplit(os.environ["UPSTREAM"])
AUTH_HEADER = os.environ.get("AUTH_HEADER", "Authorization")
AUTH_SCHEME = os.environ.get("AUTH_SCHEME", "Bearer").strip()
with open(os.environ["AUTH_FILE"], encoding="utf-8") as secret_file:
    AUTH_VALUE = secret_file.read().strip()
with open(os.environ["CLIENT_TOKEN_FILE"], encoding="utf-8") as token_file:
    CLIENT_TOKEN = token_file.read().strip()
ALLOWED_HEADERS = {
    "accept", "content-type", "anthropic-version", "anthropic-beta",
    "openai-organization", "openai-project", "user-agent",
}
MAX_REQUEST = int(os.environ.get("MAX_REQUEST_BYTES", str(20 * 1024 * 1024)))
ALLOWED_MODEL = os.environ.get("ALLOWED_MODEL", "")
MAX_OUTPUT_TOKENS = int(os.environ.get("MAX_OUTPUT_TOKENS", "0"))
MAX_PROMPT_USD_PER_MILLION = float(os.environ.get("MAX_PROMPT_USD_PER_MILLION", "0"))
MAX_COMPLETION_USD_PER_MILLION = float(os.environ.get("MAX_COMPLETION_USD_PER_MILLION", "0"))
ALLOWED_ENDPOINT = re.compile(
    r"^/(?:api/)?v1/(?:chat/completions|completions|responses|messages|messages/count_tokens)$"
    r"|^/v1beta/models/[A-Za-z0-9._:/-]+:(?:generateContent|streamGenerateContent)$"
)
ACCOUNTING_FILE = os.environ.get("ACCOUNTING_FILE", "")
MAX_USD = float(os.environ.get("MAX_USD", "0"))
MAX_REQUEST_USD = float(os.environ.get("MAX_REQUEST_USD", "0"))
MAX_PRICED_REQUEST_USD = (
    MAX_REQUEST * MAX_PROMPT_USD_PER_MILLION
    + MAX_OUTPUT_TOKENS * MAX_COMPLETION_USD_PER_MILLION
) / 1_000_000
if MAX_USD > 0 and (
    MAX_REQUEST_USD <= 0
    or MAX_REQUEST_USD > MAX_USD
    or MAX_REQUEST_USD + 1e-12 < MAX_PRICED_REQUEST_USD
):
    raise ValueError("MAX_REQUEST_USD must cover the bounded request and not exceed MAX_USD")
ACCOUNT_LOCK = threading.Lock()
SPENT_USD = 0.0
RESERVED_USD = 0.0
COST_COMPLETE = True


def write_accounting(exceeded=False):
    if not ACCOUNTING_FILE:
        return
    pending = RESERVED_USD > 0
    payload = {
        "estimated_usd": SPENT_USD + RESERVED_USD,
        "complete": COST_COMPLETE and not pending,
        "exceeded": exceeded,
    }
    temporary = ACCOUNTING_FILE + ".tmp"
    with open(temporary, "w", encoding="utf-8") as output:
        json.dump(payload, output, separators=(",", ":"))
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, ACCOUNTING_FILE)


def bounded_request(path, raw):
    if not ALLOWED_MODEL:
        return raw
    if path != "/api/v1/chat/completions":
        raise ValueError("capped relay permits only chat completions")
    try:
        payload = json.loads(raw)
    except (json.JSONDecodeError, UnicodeDecodeError) as error:
        raise ValueError("request body must be JSON") from error
    if not isinstance(payload, dict) or payload.get("model") != ALLOWED_MODEL:
        raise ValueError("request model does not match the approved model")
    if payload.get("n", 1) != 1 or payload.get("plugins"):
        raise ValueError("multiple choices and plugins are not permitted")
    token_keys = [key for key in ("max_tokens", "max_completion_tokens") if key in payload]
    if len(token_keys) > 1:
        raise ValueError("request must use one output-token limit")
    token_key = token_keys[0] if token_keys else "max_tokens"
    requested = payload.get(token_key, MAX_OUTPUT_TOKENS)
    if not isinstance(requested, int) or isinstance(requested, bool) or requested <= 0 or requested > MAX_OUTPUT_TOKENS:
        raise ValueError("request output-token limit exceeds the approved bound")
    payload[token_key] = requested
    payload["provider"] = {
        "max_price": {
            "prompt": MAX_PROMPT_USD_PER_MILLION,
            "completion": MAX_COMPLETION_USD_PER_MILLION,
            "request": 0,
            "image": 0,
        }
    }
    return json.dumps(payload, separators=(",", ":")).encode()


def response_cost(raw):
    candidates = []
    try:
        candidates.append(json.loads(raw))
    except (json.JSONDecodeError, UnicodeDecodeError):
        for line in raw.splitlines():
            if not line.startswith(b"data:"):
                continue
            data = line[5:].strip()
            if not data or data == b"[DONE]":
                continue
            try:
                candidates.append(json.loads(data))
            except (json.JSONDecodeError, UnicodeDecodeError):
                continue
    cost = None
    for candidate in candidates:
        usage = candidate.get("usage") if isinstance(candidate, dict) else None
        value = usage.get("cost") if isinstance(usage, dict) else None
        if isinstance(value, (int, float)) and value >= 0:
            cost = float(value)
    return cost


class Relay(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.send_header("Content-Length", "2")
            self.end_headers()
            self.wfile.write(b"ok")
            return
        self.send_error(405)

    def do_POST(self):
        self.relay()

    def do_DELETE(self):
        self.send_error(405)

    def do_PUT(self):
        self.send_error(405)

    def relay(self):
        global SPENT_USD, RESERVED_USD, COST_COMPLETE
        provided = self.headers.get(AUTH_HEADER, "").removeprefix(AUTH_SCHEME).strip()
        if provided != CLIENT_TOKEN:
            self.send_error(401)
            return
        length = int(self.headers.get("content-length", "0"))
        if length > MAX_REQUEST:
            self.send_error(413)
            return
        incoming = urlsplit(self.path)
        path = incoming.path or "/"
        if incoming.query:
            path += "?" + incoming.query
        upstream_path = UPSTREAM.path.rstrip("/") + path
        if not ALLOWED_ENDPOINT.match(upstream_path.split("?", 1)[0]):
            self.send_error(403)
            return

        body = self.rfile.read(length) if length else b""
        try:
            body = bounded_request(upstream_path.split("?", 1)[0], body)
        except ValueError as error:
            self.send_error(403, str(error))
            return

        reserved = MAX_REQUEST_USD if MAX_USD > 0 else 0.0
        with ACCOUNT_LOCK:
            if MAX_USD > 0 and SPENT_USD + RESERVED_USD + reserved > MAX_USD + 1e-12:
                write_accounting(exceeded=True)
                self.send_error(402, "ride spend cap reached")
                return
            RESERVED_USD += reserved
            write_accounting()

        headers = {key: value for key, value in self.headers.items() if key.lower() in ALLOWED_HEADERS}
        headers[AUTH_HEADER] = f"{AUTH_SCHEME} {AUTH_VALUE}".strip()
        headers["Content-Length"] = str(len(body))
        connection = http.client.HTTPSConnection(UPSTREAM.hostname, UPSTREAM.port or 443, timeout=300, context=ssl.create_default_context())
        try:
            connection.request(self.command, upstream_path, body=body, headers=headers)
            response = connection.getresponse()
            raw = response.read()
            cost = response_cost(raw)
            with ACCOUNT_LOCK:
                RESERVED_USD -= reserved
                if cost is None:
                    SPENT_USD += reserved
                    COST_COMPLETE = False
                else:
                    SPENT_USD += cost
                exceeded = MAX_USD > 0 and SPENT_USD > MAX_USD + 1e-12
                write_accounting(exceeded=exceeded)
            self.send_response(response.status)
            for key, value in response.getheaders():
                if key.lower() not in {"connection", "transfer-encoding", "content-length", "content-encoding"}:
                    self.send_header(key, value)
            self.send_header("Content-Length", str(len(raw)))
            self.send_header("Connection", "close")
            self.end_headers()
            self.wfile.write(raw)
            self.wfile.flush()
        except Exception as error:
            with ACCOUNT_LOCK:
                RESERVED_USD -= reserved
                SPENT_USD += reserved
                COST_COMPLETE = False
                write_accounting()
            self.send_error(502, str(error))
        finally:
            self.close_connection = True
            connection.close()

    def log_message(self, format, *args):
        print(f"relay {self.address_string()} {format % args}", flush=True)


with ACCOUNT_LOCK:
    write_accounting()
ThreadingHTTPServer(("0.0.0.0", 8080), Relay).serve_forever()
