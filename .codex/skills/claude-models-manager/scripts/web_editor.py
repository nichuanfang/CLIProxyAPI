"""Loopback-only, dependency-free Claude Models Manager web server."""

import argparse
import json
import secrets
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from claude_config import ConfigError, ConflictError, Manager, MAX_BYTES


class Server(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, port, manager):
        self.manager = manager
        self.token = secrets.token_urlsafe(32)
        super().__init__(("127.0.0.1", port), Handler)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        pass

    def respond(self, status, value, kind="application/json; charset=utf-8"):
        body = value.encode("utf-8") if isinstance(value, str) else json.dumps(value, ensure_ascii=True).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("Referrer-Policy", "no-referrer")
        self.send_header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'nonce-" + self.server.token + "'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
        self.end_headers()
        self.wfile.write(body)

    def local_request(self):
        expected = "127.0.0.1:" + str(self.server.server_port)
        return self.headers.get("Host") == expected and self.headers.get("Origin", "http://" + expected) == "http://" + expected

    def do_GET(self):
        if not self.local_request():
            return self.respond(403, {"error": "Only same-origin loopback requests are allowed"})
        try:
            if self.path == "/":
                page = Path(__file__).with_name("web_editor.html").read_text(encoding="utf-8")
                return self.respond(200, page.replace("__SESSION_TOKEN__", self.server.token), "text/html; charset=utf-8")
            if self.path == "/api/health":
                return self.respond(200, {"app": "claude-models-manager", "version": 2,
                                          "settings_path": str(self.server.manager.path)})
            if self.path == "/api/state":
                return self.respond(200, self.server.manager.state())
            if self.path == "/api/backups":
                return self.respond(200, self.server.manager.backups())
            if self.path == "/api/document":
                return self.respond(200, self.server.manager.document())
            if self.path == "/api/audit":
                return self.respond(200, self.server.manager.audit())
            if self.path == "/favicon.ico":
                return self.respond(204, "")
            self.respond(404, {"error": "Not found"})
        except (ConfigError, OSError, ValueError):
            self.respond(400, {"error": "无法读取配置，请检查文件内容和访问权限。原文件未改动。"})

    def do_POST(self):
        if not self.local_request() or not secrets.compare_digest(self.headers.get("X-Manager-Token", ""), self.server.token):
            return self.respond(403, {"error": "Invalid local session"})
        if self.headers.get("Content-Type", "").split(";")[0] != "application/json":
            return self.respond(415, {"error": "Expected application/json"})
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length <= 0 or length > MAX_BYTES:
                return self.respond(413, {"error": "Invalid request size"})
            payload = json.loads(self.rfile.read(length))
            if not isinstance(payload, dict):
                raise ConfigError("Expected an object")
            manager = self.server.manager
            if self.path == "/api/update":
                return self.respond(200, manager.update(payload.get("changes"), payload.get("revision")))
            if self.path in ("/api/settings/preview", "/api/settings/save"):
                return self.respond(200, manager.edit_settings(payload, payload.get("revision"), save=self.path.endswith("/save")))
            if self.path == "/api/fetch":
                return self.respond(200, manager.fetch(payload.get("url", ""), payload.get("api_key")))
            if self.path == "/api/restore":
                return self.respond(200, manager.restore(payload.get("name"), payload.get("revision")))
            if self.path == "/api/shutdown":
                self.respond(200, {"stopped": True})
                threading.Thread(target=self.server.shutdown, daemon=True).start()
                return
            self.respond(404, {"error": "Not found"})
        except ConflictError as exc:
            self.respond(409, {"error": str(exc)})
        except ConfigError as exc:
            self.respond(400, {"error": str(exc)})
        except (ValueError, TypeError, OSError):
            self.respond(400, {"error": "操作失败，请检查输入和文件权限。"})


def main():
    parser = argparse.ArgumentParser(description="Local Claude Models Manager")
    parser.add_argument("--port", type=int, default=8390)
    parser.add_argument("--settings")
    args = parser.parse_args()
    with Server(args.port, Manager(args.settings)) as server:
        print(f"http://127.0.0.1:{server.server_port}", flush=True)
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass


if __name__ == "__main__":
    main()
