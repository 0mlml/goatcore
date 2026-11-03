#!/usr/bin/env python3
"""Tiny HTTP fixtures for the pinned Traefik ForwardAuth runtime test."""

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Thread
from urllib.parse import parse_qs, unquote, urlsplit


class QuietHandler(BaseHTTPRequestHandler):
    def log_message(self, _format, *_args):
        pass


class AuthHandler(QuietHandler):
    def do_GET(self):
        source = self.headers.get("X-Forwarded-For", "")
        if source != "127.0.0.1":
            self.send_error(403, "untrusted source")
            return

        forwarded_uri = unquote(urlsplit(self.headers.get("X-Forwarded-Uri", "")).path)
        forwarded_method = self.headers.get("X-Forwarded-Method", "")
        allowed = forwarded_uri == "/allowed" and forwarded_method == "POST"
        if allowed:
            self.send_response(204)
            self.end_headers()
            return

        body = (
            b'<!doctype html><form method="post" '
            b'action="/__goat/probation/request">'
            b'<input name="token" value="hold-token"></form>'
        )
        self.send_response(423)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


class BackendHandler(QuietHandler):
    def _body(self):
        return self.rfile.read(int(self.headers.get("Content-Length", "0")))

    def do_POST(self):
        body = self._body()
        content_type = self.headers.get("Content-Type", "")
        if unquote(urlsplit(self.path).path) == "/__goat/probation/request":
            response = b"reached application"
            self.send_response(418)
        else:
            response = f"content-type={content_type} body={body.decode('utf-8')}".encode()
            self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)


class ConsoleHandler(QuietHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        form = parse_qs(body.decode("utf-8"))
        content_type = self.headers.get("Content-Type", "")
        if content_type.startswith("application/x-www-form-urlencoded") and form.get("token") == ["hold-token"]:
            response = b"recorded"
            self.send_response(200)
        else:
            response = b"invalid petition"
            self.send_response(400)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)


def serve(port, handler):
    ThreadingHTTPServer(("127.0.0.1", port), handler).serve_forever()


Thread(target=serve, args=(18081, AuthHandler), daemon=True).start()
Thread(target=serve, args=(18083, ConsoleHandler), daemon=True).start()
serve(18082, BackendHandler)
