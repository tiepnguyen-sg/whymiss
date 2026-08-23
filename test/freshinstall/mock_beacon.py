#!/usr/bin/env python3
"""Replay captured Beacon API fixtures for the fresh-install workflow."""

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit


FIXTURES = Path(__file__).resolve().parents[2] / "internal/source/beaconapi/testdata"
ROUTES = {
    "/eth/v1/beacon/genesis": ("genesis.json", "application/json"),
    "/eth/v1/config/spec": ("spec.json", "application/json"),
    "/eth/v1/events": ("sse_stream.txt", "text/event-stream"),
}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, _format, *_args):
        return

    def do_GET(self):
        path = urlsplit(self.path).path
        route = ROUTES.get(path)
        if route is None:
            self.send_error(404)
            return
        filename, content_type = route
        body = (FIXTURES / filename).read_bytes()
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


class Server(ThreadingHTTPServer):
    allow_reuse_address = True


if __name__ == "__main__":
    server = Server(("0.0.0.0", 5052), Handler)
    server.daemon_threads = True
    server.serve_forever()
