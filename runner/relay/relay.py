import http.client
import os
import re
import ssl
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

UPSTREAM = urlsplit(os.environ["UPSTREAM"])
AUTH_HEADER = os.environ.get("AUTH_HEADER", "Authorization")
AUTH_SCHEME = os.environ.get("AUTH_SCHEME", "Bearer").strip()
AUTH_VALUE = os.environ["AUTH_VALUE"]
ALLOWED_HEADERS = {
    "accept", "content-type", "anthropic-version", "anthropic-beta",
    "openai-organization", "openai-project", "user-agent",
}
MAX_REQUEST = 20 * 1024 * 1024
ALLOWED_ENDPOINT = re.compile(
    r"^/(?:api/)?v1/(?:chat/completions|completions|responses|messages|messages/count_tokens)$"
    r"|^/v1beta/models/[A-Za-z0-9._:/-]+:(?:generateContent|streamGenerateContent)$"
)


class Relay(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        self.send_error(405)

    def do_POST(self):
        self.relay()

    def do_DELETE(self):
        self.send_error(405)

    def do_PUT(self):
        self.send_error(405)

    def relay(self):
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
        headers = {key: value for key, value in self.headers.items() if key.lower() in ALLOWED_HEADERS}
        headers[AUTH_HEADER] = f"{AUTH_SCHEME} {AUTH_VALUE}".strip()
        body = self.rfile.read(length) if length else None
        connection = http.client.HTTPSConnection(UPSTREAM.hostname, UPSTREAM.port or 443, timeout=300, context=ssl.create_default_context())
        try:
            connection.request(self.command, upstream_path, body=body, headers=headers)
            response = connection.getresponse()
            self.send_response(response.status)
            for key, value in response.getheaders():
                if key.lower() not in {"connection", "transfer-encoding", "content-length", "content-encoding"}:
                    self.send_header(key, value)
            self.send_header("Connection", "close")
            self.end_headers()
            while chunk := response.read(64 * 1024):
                self.wfile.write(chunk)
                self.wfile.flush()
        except Exception as error:
            self.send_error(502, str(error))
        finally:
            self.close_connection = True
            connection.close()

    def log_message(self, format, *args):
        print(f"relay {self.address_string()} {format % args}", flush=True)


ThreadingHTTPServer(("0.0.0.0", 8080), Relay).serve_forever()
