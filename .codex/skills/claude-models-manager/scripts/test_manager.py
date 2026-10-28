"""Behavioral tests use temporary settings and local fake gateways only."""

import contextlib
import json
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

from claude_config import ConfigError, ConflictError, Manager
from web_editor import Server


@contextlib.contextmanager
def running(server):
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield server
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


class Gateway(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        self.server.received.append(dict(self.headers))
        if self.path == "/redirect/v1/models":
            self.send_response(302)
            self.send_header("Location", self.server.redirect)
            self.end_headers()
            return
        body = json.dumps({"data": [{"id": "gateway-model", "owned_by": "local"},
                                    {"id": "gateway-model"}, {"bad": True}]}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


class ManagerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "settings.json"
        self.original = {"model": "sonnet", "env": {"ANTHROPIC_AUTH_TOKEN": "test-secret",
                         "ANTHROPIC_DEFAULT_SONNET_MODEL": "old-model", "UNRELATED": "keep",
                         "ANTHROPIC_DEFAULT_SONNET_MODEL_SUPPORTED_CAPABILITIES": "thinking"},
                         "permissions": {"allow": ["Read"]}, "hooks": {"test": []},
                         "enabledPlugins": {"plugin": True}, "availableModels": ["sonnet"],
                         "modelOverrides": {"claude-sonnet-4-6": "provider-id"}}
        self.path.write_text(json.dumps(self.original, indent=4), encoding="utf-8")
        self.manager = Manager(self.path)

    def update(self, changes):
        return self.manager.update(changes, self.manager.state()["revision"])

    def test_model_mapping_preserves_unmanaged_settings_and_secret(self):
        self.path.write_bytes(b"\xef\xbb\xbf" + self.path.read_bytes())
        before = self.path.read_bytes()
        self.update({"roles": [{"role": "sonnet", "model": "new-model", "name": "A model"}]})
        data = json.loads(self.path.read_text())
        for key in ("permissions", "hooks", "enabledPlugins", "availableModels", "modelOverrides"):
            self.assertEqual(data[key], self.original[key])
        self.assertEqual(data["env"]["ANTHROPIC_AUTH_TOKEN"], "test-secret")
        self.assertEqual(data["env"]["UNRELATED"], "keep")
        self.assertEqual(data["env"]["ANTHROPIC_DEFAULT_SONNET_MODEL_SUPPORTED_CAPABILITIES"], "thinking")
        self.assertNotIn("test-secret", json.dumps(self.manager.state()))
        backup = self.manager.backups()[0]["name"]
        self.manager.restore(backup, self.manager.state()["revision"])
        self.assertEqual(self.path.read_bytes(), before)
        self.assertEqual(len(self.manager.backups()), 2)

    def test_startup_and_noop_are_read_only(self):
        before = self.path.read_bytes()
        self.manager.state()
        self.manager.fetch = lambda *args: None
        self.update({"model": "sonnet"})
        self.assertEqual(self.path.read_bytes(), before)
        self.assertEqual(self.manager.backups(), [])
        missing = Manager(Path(self.temp.name) / "new" / "settings.json")
        self.assertFalse(missing.state()["exists"])
        self.assertFalse(missing.path.parent.exists())

    def test_new_file_restore_returns_to_nonexistent_state(self):
        manager = Manager(Path(self.temp.name) / "new" / "settings.json")
        manager.update({"model": "custom-id"}, "missing")
        original = manager.backups()[0]["name"]
        manager.restore(original, manager.state()["revision"])
        self.assertFalse(manager.path.exists())
        self.assertEqual(len(manager.backups()), 2)

    def test_stale_revision_does_not_overwrite_external_changes(self):
        revision = self.manager.state()["revision"]
        self.path.write_text('{"model":"external"}', encoding="utf-8")
        with self.assertRaises(ConflictError):
            self.manager.update({"model": "manager"}, revision)
        self.assertEqual(json.loads(self.path.read_text())["model"], "external")
        self.assertEqual(self.manager.backups(), [])

    def test_clear_mapping_and_credentials_are_explicit(self):
        self.update({"roles": [{"role": "sonnet", "model": "", "name": "Orphan"}]})
        env = json.loads(self.path.read_text())["env"]
        self.assertNotIn("ANTHROPIC_DEFAULT_SONNET_MODEL", env)
        self.assertNotIn("ANTHROPIC_DEFAULT_SONNET_MODEL_NAME", env)
        self.update({"auth_token": ""})
        self.assertTrue(self.manager.state()["auth_configured"])
        self.update({"auth_token": "replacement", "auth_kind": "ANTHROPIC_API_KEY"})
        env = json.loads(self.path.read_text())["env"]
        self.assertNotIn("ANTHROPIC_AUTH_TOKEN", env)
        self.assertEqual(env["ANTHROPIC_API_KEY"], "replacement")
        self.update({"clear_auth": True})
        self.assertFalse(self.manager.state()["auth_configured"])

    def test_invalid_changes_leave_original_untouched(self):
        before = self.path.read_bytes()
        for changes in ({"permissions": {}}, {"roles": [{"role": "unknown"}]},
                        {"base_url": "http://user:secret@localhost"}, {"base_url": "file:///tmp"},
                        {"base_url": "http://localhost/v1/models"}, {"model": "bad\nmodel"},
                        {"discovery": "false"}, {"roles": [{"role": "opus", "model": 1}]}):
            with self.subTest(changes=changes), self.assertRaises(ConfigError):
                self.update(changes)
        self.assertEqual(self.path.read_bytes(), before)
        self.assertEqual(self.manager.backups(), [])

    def test_invalid_json_is_not_reinitialized(self):
        self.path.write_text('{broken', encoding="utf-8")
        with self.assertRaises(ConfigError):
            self.manager.state()
        self.assertEqual(self.path.read_text(), '{broken')

    def test_backup_traversal_is_rejected(self):
        with self.assertRaises(ConfigError):
            self.manager.restore("../settings.bad.json", self.manager.state()["revision"])

    def test_config_directory_environment_is_honored(self):
        with patch.dict("os.environ", {"CLAUDE_CONFIG_DIR": str(Path(self.temp.name) / "custom")}):
            self.assertEqual(Manager().path, (Path(self.temp.name) / "custom" / "settings.json").resolve())

    def test_discovery_sends_saved_secret_only_to_configured_origin(self):
        gateway = ThreadingHTTPServer(("127.0.0.1", 0), Gateway)
        other = ThreadingHTTPServer(("127.0.0.1", 0), Gateway)
        gateway.received, other.received = [], []
        with running(gateway), running(other):
            base = f"http://127.0.0.1:{gateway.server_port}"
            foreign = f"http://127.0.0.1:{other.server_port}"
            gateway.redirect = foreign + "/v1/models"
            self.update({"base_url": base})
            before = self.path.read_bytes()
            self.assertEqual(self.manager.fetch()["models"][0]["id"], "gateway-model")
            self.assertEqual(gateway.received[-1]["Authorization"], "Bearer test-secret")
            self.manager.fetch(foreign)
            self.assertNotIn("Authorization", other.received[-1])
            self.manager.fetch(foreign + "/v1", "explicit")
            self.assertEqual(other.received[-1]["Authorization"], "Bearer explicit")
            count = len(other.received)
            with self.assertRaises(ConfigError):
                self.manager.fetch(base + "/redirect")
            self.assertEqual(len(other.received), count)
            self.assertEqual(self.path.read_bytes(), before)

    def test_server_requires_local_host_and_token_and_does_not_expose_secrets(self):
        with running(Server(0, self.manager)) as server:
            base = f"http://127.0.0.1:{server.server_port}"
            with urllib.request.urlopen(base + "/api/state") as response:
                state = json.load(response)
            self.assertNotIn("test-secret", json.dumps(state))
            with urllib.request.urlopen(base + "/") as response:
                html = response.read().decode()
            self.assertIn(server.token, html)
            self.assertNotIn("test-secret", html)
            request = urllib.request.Request(base + "/api/update", data=json.dumps({"changes": {"model": "new"}, "revision": state["revision"]}).encode(),
                                             headers={"Content-Type": "application/json"})
            with self.assertRaises(urllib.error.HTTPError) as failure:
                urllib.request.urlopen(request)
            self.assertEqual(failure.exception.code, 403)
            failure.exception.close()
            request.add_header("X-Manager-Token", server.token)
            with urllib.request.urlopen(request) as response:
                self.assertEqual(json.load(response)["model"], "new")
            with self.assertRaises(urllib.error.HTTPError) as failure:
                urllib.request.urlopen(request)
            self.assertEqual(failure.exception.code, 409)
            failure.exception.close()
            request = urllib.request.Request(base + "/api/state", headers={"Host": "evil.example"})
            with self.assertRaises(urllib.error.HTTPError) as failure:
                urllib.request.urlopen(request)
            self.assertEqual(failure.exception.code, 403)
            failure.exception.close()
            request = urllib.request.Request(base + "/api/state", headers={"Origin": "https://evil.example"})
            with self.assertRaises(urllib.error.HTTPError) as failure:
                urllib.request.urlopen(request)
            self.assertEqual(failure.exception.code, 403)
            failure.exception.close()


if __name__ == "__main__":
    unittest.main()
