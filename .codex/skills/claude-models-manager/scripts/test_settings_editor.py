"""Regression tests for config editing, redaction, previews and audit findings."""

import copy
import json
import tempfile
import unittest
import urllib.error
import urllib.request
from pathlib import Path

from claude_config import ConfigError, ConflictError, Manager
from config_audit import report
from settings_editor import KEEP_SECRET
from test_manager import running
from web_editor import Server


class SettingsEditorTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "settings.json"
        self.original = {"model": "sonnet", "env": {"ANTHROPIC_AUTH_TOKEN": "auth-secret",
                         "ANTHROPIC_BASE_URL": "http://127.0.0.1:8317", "UNRELATED": "keep",
                         "CLAUDE_CODE_AUTO_MODE_SERVER": "0", "DISABLE_AUTOUPDATER": "1"},
                         "permissions": {"defaultMode": "auto", "deny": ["SendMessage"], "additionalDirectories": ["D:/work"]},
                         "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "tool --token hidden-value"}]}]},
                         "credentials": {"custom": "nested-secret"}, "enabledPlugins": {"plugin": False}}
        self.path.write_text(json.dumps(self.original), encoding="utf-8")
        self.manager = Manager(self.path)

    def test_full_document_preserves_all_secrets_and_unmanaged_fields(self):
        document = self.manager.document()
        for secret in ("auth-secret", "hidden-value", "nested-secret"):
            self.assertNotIn(secret, json.dumps(document))
        candidate = json.loads(document["content"])
        candidate["theme"] = "dark"
        self.manager.edit_settings({"mode": "json", "content": json.dumps(candidate)}, document["revision"], save=True)
        saved = json.loads(self.path.read_text())
        expected = copy.deepcopy(self.original)
        expected["theme"] = "dark"
        self.assertEqual(saved, expected)
        self.assertEqual(len(self.manager.backups()), 1)

    def test_preview_is_read_only_and_flags_secret_deletions_without_values(self):
        before = self.path.read_bytes()
        document = self.manager.document()
        candidate = json.loads(document["content"])
        candidate.pop("env")
        preview = self.manager.edit_settings({"mode": "json", "content": json.dumps(candidate)}, document["revision"])
        self.assertEqual(preview["change_count"], 1)
        self.assertIn("/env/ANTHROPIC_AUTH_TOKEN", preview["secret_changes"])
        self.assertNotIn("auth-secret", json.dumps(preview))
        self.assertEqual(self.path.read_bytes(), before)
        self.assertEqual(self.manager.backups(), [])

    def test_secret_placeholders_cannot_be_moved_or_fabricated(self):
        document = self.manager.document()
        candidate = json.loads(document["content"])
        candidate["env"]["NEW_SECRET"] = KEEP_SECRET
        with self.assertRaises(ConfigError):
            self.manager.edit_settings({"mode": "json", "content": json.dumps(candidate)}, document["revision"], save=True)
        self.assertEqual(self.manager.backups(), [])

    def test_preference_patch_changes_only_selected_fields(self):
        revision = self.manager.state()["revision"]
        result = self.manager.edit_settings({"mode": "preferences", "changes": {
            "autoUpdatesChannel": "stable", "powershell": "on", "cleanupPeriodDays": 60,
            "schema": True, "ask": ["Bash(git push *)"]}}, revision, save=True)
        saved = json.loads(self.path.read_text())
        self.assertEqual(saved["env"]["CLAUDE_CODE_USE_POWERSHELL_TOOL"], "1")
        self.assertEqual(saved["env"]["ANTHROPIC_AUTH_TOKEN"], "auth-secret")
        self.assertEqual(saved["permissions"]["deny"], ["SendMessage"])
        self.assertEqual(saved["permissions"]["additionalDirectories"], ["D:/work"])
        self.assertEqual(saved["hooks"], self.original["hooks"])
        self.assertEqual(saved["enabledPlugins"], self.original["enabledPlugins"])
        self.assertEqual(result["preferences"]["autoUpdatesChannel"], "stable")

    def test_invalid_json_duplicate_keys_and_types_never_write(self):
        before = self.path.read_bytes()
        revision = self.manager.state()["revision"]
        invalid = ['{bad', '{"model":"a","model":"b"}', '{"x": NaN}', '[]',
                   '{"cleanupPeriodDays":0}', '{"cleanupPeriodDays":true}',
                   '{"env":{"FLAG":true}}', '{"permissions":{"deny":"Bash"}}']
        for content in invalid:
            with self.subTest(content=content), self.assertRaises(ConfigError):
                self.manager.edit_settings({"mode": "json", "content": content}, revision, save=True)
        self.assertEqual(self.path.read_bytes(), before)
        self.assertEqual(self.manager.backups(), [])

    def test_stale_preview_cannot_be_saved(self):
        document = self.manager.document()
        payload = {"mode": "preferences", "changes": {"cleanupPeriodDays": 60}}
        self.manager.edit_settings(payload, document["revision"])
        self.manager.update({"model": "opus"}, document["revision"])
        with self.assertRaises(ConflictError):
            self.manager.edit_settings(payload, document["revision"], save=True)
        self.assertEqual(json.loads(self.path.read_text())["model"], "opus")

    def test_audit_explains_classifier_and_wrapper_without_changing_config(self):
        local = self.path.parent / "settings.local.json"
        local.write_text('{"permissions":{"allow":["mcp__shell"]}}', encoding="utf-8")
        before = self.path.read_bytes()
        context = {"platform": "nt", "user_config": True, "wrappers": [{"path": "profile.ps1", "tools": ["Bash", "Read"]}],
                   "mcp_servers": [{"name": "context7", "transport": "http"}], "scope_files": []}
        result = report(self.original, self.path, context)
        findings = {f["id"]: f for f in result["findings"]}
        self.assertIn("local-scope", findings)
        self.assertIn("classifier", findings)
        self.assertIn("并不关闭安全检查", findings["classifier"]["detail"])
        self.assertTrue(any("PowerShell" in f["detail"] for f in result["findings"]))
        self.assertNotIn("auth-secret", json.dumps(result))
        self.assertEqual(self.path.read_bytes(), before)

    def test_http_config_writes_require_session_and_revision(self):
        with running(Server(0, self.manager)) as server:
            base = f"http://127.0.0.1:{server.server_port}"
            with urllib.request.urlopen(base + "/api/document") as response:
                document = json.load(response)
            self.assertNotIn("auth-secret", json.dumps(document))
            payload = {"mode": "preferences", "revision": document["revision"], "changes": {"autoUpdatesChannel": "stable"}}
            request = urllib.request.Request(base + "/api/settings/save", data=json.dumps(payload).encode(),
                                             headers={"Content-Type": "application/json"})
            with self.assertRaises(urllib.error.HTTPError) as failure:
                urllib.request.urlopen(request)
            self.assertEqual(failure.exception.code, 403)
            failure.exception.close()
            request.add_header("X-Manager-Token", server.token)
            with urllib.request.urlopen(request) as response:
                result = json.load(response)
            self.assertEqual(result["preferences"]["autoUpdatesChannel"], "stable")
            self.assertNotIn("auth-secret", json.dumps(result))


if __name__ == "__main__":
    unittest.main()
