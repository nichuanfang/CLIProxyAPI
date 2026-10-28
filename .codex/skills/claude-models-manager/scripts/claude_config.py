"""Claude Code model settings, shared by the CLI and local web editor."""

import copy
import hashlib
import json
import os
import tempfile
import threading
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path


ROLES = {
    "opus": "ANTHROPIC_DEFAULT_OPUS_MODEL",
    "sonnet": "ANTHROPIC_DEFAULT_SONNET_MODEL",
    "haiku": "ANTHROPIC_DEFAULT_HAIKU_MODEL",
    "fable": "ANTHROPIC_DEFAULT_FABLE_MODEL",
    "custom": "ANTHROPIC_CUSTOM_MODEL_OPTION",
}
DISCOVERY = "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY"
AUTH_KEYS = ("ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY")
MAX_BYTES = 4 * 1024 * 1024


class ConfigError(ValueError):
    pass


class ConflictError(ConfigError):
    pass


def text(value, name, limit=4096):
    if not isinstance(value, str) or len(value) > limit or any(ord(c) < 32 for c in value):
        raise ConfigError(f"{name} must be a single-line string (maximum {limit} characters)")
    return value.strip()


def validate_url(value):
    value = text(value, "URL")
    parts = urllib.parse.urlsplit(value)
    if parts.scheme not in ("http", "https") or not parts.hostname or parts.username or parts.password:
        raise ConfigError("URL must use HTTP(S), without embedded credentials")
    try:
        parts.port
    except ValueError as exc:
        raise ConfigError("Invalid URL port") from exc
    if parts.query or parts.fragment:
        raise ConfigError("URL must not include a query string or fragment")
    return value.rstrip("/")


def origin(value):
    parts = urllib.parse.urlsplit(validate_url(value))
    return parts.scheme, parts.hostname, parts.port or (443 if parts.scheme == "https" else 80)


def models_url(base):
    base = validate_url(base)
    if base.endswith("/v1/models"):
        return base
    return base + ("/models" if base.endswith("/v1") else "/v1/models")


def atomic_write(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, name = tempfile.mkstemp(prefix=".claude-models-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Manager:
    def __init__(self, settings=None):
        config_dir = Path(os.environ.get("CLAUDE_CONFIG_DIR") or Path.home() / ".claude")
        self.path = Path(settings or config_dir / "settings.json").expanduser().resolve()
        self.backup_dir = self.path.parent / "model-manager" / "backups" / self.path.name
        self.lock = threading.RLock()

    def read(self):
        raw = self.path.read_bytes() if self.path.exists() else None
        if raw is not None and len(raw) > MAX_BYTES:
            raise ConfigError("Settings file exceeds 4 MiB")
        try:
            data = json.loads(raw.decode("utf-8-sig")) if raw is not None else {}
        except (ValueError, UnicodeError) as exc:
            raise ConfigError("Settings is not valid UTF-8 JSON; original file has been left untouched") from exc
        if not isinstance(data, dict) or not isinstance(data.get("env", {}), dict):
            raise ConfigError("Settings and its env field must be JSON objects")
        if any(not isinstance(value, str) for value in data.get("env", {}).values()):
            raise ConfigError("Settings env values must be strings")
        revision = hashlib.sha256(raw).hexdigest() if raw is not None else "missing"
        return data, raw, revision

    def state(self):
        with self.lock:
            data, raw, revision = self.read()
            env = data.get("env", {})
            roles = []
            for role, key in ROLES.items():
                roles.append({"role": role, "model": env.get(key, ""), "name": env.get(key + "_NAME", ""),
                              "description": env.get(key + "_DESCRIPTION", "")})
            base = env.get("ANTHROPIC_BASE_URL", "")
            overrides = [key for key in (*ROLES.values(), "ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_MODEL",
                                        "ANTHROPIC_BASE_URL", DISCOVERY) if os.environ.get(key)]
            return {"settings_path": str(self.path), "exists": raw is not None, "revision": revision,
                    "model": data.get("model", ""), "roles": roles, "base_url": base,
                    "discovery": env.get(DISCOVERY) == "1", "auth_configured": any(env.get(k) for k in AUTH_KEYS),
                    "auth_kind": next((k for k in AUTH_KEYS if env.get(k)), ""),
                    "env_model": env.get("ANTHROPIC_MODEL", ""),
                    "env_default_model": env.get("ANTHROPIC_DEFAULT_MODEL", ""),
                    "available_models": data.get("availableModels"), "environment_overrides": overrides}

    def _backup(self, raw):
        stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S.%fZ")
        snapshot = {"version": 1, "settings_path": str(self.path), "existed": raw is not None,
                    "content": raw.decode("utf-8") if raw is not None else None}
        atomic_write(self.backup_dir / f"settings.{stamp}.json", json.dumps(snapshot, ensure_ascii=False).encode("utf-8"))

    def _check_revision(self, expected, revision):
        if not isinstance(expected, str) or expected != revision:
            raise ConflictError("配置已被其他程序修改，请刷新页面后重试。")

    def update(self, patch, expected):
        if not isinstance(patch, dict):
            raise ConfigError("Changes must be an object")
        allowed = {"model", "roles", "base_url", "auth_token", "auth_kind", "clear_auth", "discovery", "clear_model_override"}
        if set(patch) - allowed:
            raise ConfigError("Unsupported settings fields")
        with self.lock:
            data, raw, revision = self.read()
            self._check_revision(expected, revision)
            result = copy.deepcopy(data)
            env = result.setdefault("env", {})
            if "model" in patch:
                model = text(patch["model"], "Model ID")
                if model:
                    result["model"] = model
                else:
                    result.pop("model", None)
            if "roles" in patch:
                if not isinstance(patch["roles"], list):
                    raise ConfigError("Roles must be an array")
                seen = set()
                for entry in patch["roles"]:
                    if not isinstance(entry, dict) or entry.get("role") not in ROLES or set(entry) - {"role", "model", "name", "description"}:
                        raise ConfigError("Unknown model role or mapping field")
                    role = entry["role"]
                    if role in seen:
                        raise ConfigError("Duplicate model role")
                    seen.add(role)
                    key = ROLES[role]
                    for field, suffix in (("model", ""), ("name", "_NAME"), ("description", "_DESCRIPTION")):
                        if field in entry:
                            value = text(entry[field], field)
                            if value:
                                env[key + suffix] = value
                            else:
                                env.pop(key + suffix, None)
                    if not env.get(key):
                        for suffix in ("_NAME", "_DESCRIPTION"):
                            env.pop(key + suffix, None)
            if "base_url" in patch:
                value = text(patch["base_url"], "Base URL")
                if value:
                    value = validate_url(value)
                    if urllib.parse.urlsplit(value).path.endswith("/v1/models"):
                        raise ConfigError("Base URL is the gateway root, not its /v1/models endpoint")
                    env["ANTHROPIC_BASE_URL"] = value
                else:
                    env.pop("ANTHROPIC_BASE_URL", None)
            for key in ("discovery", "clear_auth", "clear_model_override"):
                if key in patch and not isinstance(patch[key], bool):
                    raise ConfigError(f"{key} must be a boolean")
            if "discovery" in patch:
                env[DISCOVERY] = "1" if patch["discovery"] else "0"
            if patch.get("clear_auth"):
                for key in AUTH_KEYS:
                    env.pop(key, None)
            if "auth_kind" in patch and patch["auth_kind"] not in AUTH_KEYS:
                raise ConfigError("Unsupported authentication variable")
            if "auth_token" in patch:
                token = text(patch["auth_token"], "API key", 16384)
                if token:
                    for key in AUTH_KEYS:
                        env.pop(key, None)
                    env[patch.get("auth_kind", "ANTHROPIC_AUTH_TOKEN")] = token
            if patch.get("clear_model_override"):
                env.pop("ANTHROPIC_MODEL", None)
                env.pop("ANTHROPIC_DEFAULT_MODEL", None)
            if not env and "env" not in data:
                result.pop("env", None)
            if result != data:
                encoded = (json.dumps(result, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
                # Check again after validation so external edits are not silently replaced.
                self._check_revision(revision, self.read()[2])
                self._backup(raw)
                atomic_write(self.path, encoded)
            return self.state()

    def backups(self):
        with self.lock:
            return [{"name": p.name, "bytes": p.stat().st_size} for p in sorted(self.backup_dir.glob("settings.*.json"), reverse=True)]

    def restore(self, name, expected):
        if not isinstance(name, str) or Path(name).name != name or not name.startswith("settings.") or not name.endswith(".json"):
            raise ConfigError("Invalid backup name")
        with self.lock:
            _, raw, revision = self.read()
            self._check_revision(expected, revision)
            path = (self.backup_dir / name).resolve()
            if path.parent != self.backup_dir.resolve() or path.is_symlink():
                raise ConfigError("Invalid backup path")
            snapshot = json.loads(path.read_text(encoding="utf-8"))
            if snapshot.get("version") != 1 or snapshot.get("settings_path") != str(self.path):
                raise ConfigError("Backup belongs to another settings file")
            content = snapshot.get("content")
            if snapshot.get("existed") is True:
                data = json.loads(content.lstrip("\ufeff"))
                if (not isinstance(data, dict) or not isinstance(data.get("env", {}), dict)
                        or any(not isinstance(v, str) for v in data.get("env", {}).values())):
                    raise ConfigError("Invalid backup settings")
            elif snapshot.get("existed") is not False or content is not None:
                raise ConfigError("Invalid backup content")
            self._check_revision(revision, self.read()[2])
            self._backup(raw)
            if snapshot["existed"]:
                atomic_write(self.path, content.encode("utf-8"))
            else:
                self.path.unlink(missing_ok=True)
            return self.state()

    def fetch(self, url="", api_key=None):
        data, _, _ = self.read()
        env = data.get("env", {})
        base = env.get("ANTHROPIC_BASE_URL", "")
        target = models_url(url or base or "http://127.0.0.1:8317")
        key = text(api_key, "API key", 16384) if api_key is not None else ""
        # Never forward the saved credential to a different origin or a redirect.
        if api_key is None and base and origin(base) == origin(target):
            key = next((env[k] for k in AUTH_KEYS if env.get(k)), "")
        headers = {"Accept": "application/json", "User-Agent": "Claude-Models-Manager/1"}
        if key:
            headers["Authorization"] = "Bearer " + key
            headers["x-api-key"] = key
        try:
            with urllib.request.build_opener(NoRedirect).open(urllib.request.Request(target, headers=headers)) as response:
                raw = response.read(MAX_BYTES + 1)
            if len(raw) > MAX_BYTES:
                raise ConfigError("Upstream response exceeds 4 MiB")
            payload = json.loads(raw)
        except urllib.error.HTTPError as exc:
            code = exc.code
            exc.close()
            raise ConfigError(f"上游返回 HTTP {code}，请检查地址、API Key 和网关状态。") from None
        except (urllib.error.URLError, OSError):
            raise ConfigError("无法连接上游，请检查地址和网关状态。") from None
        except ValueError:
            raise ConfigError("上游没有返回有效的 JSON。") from None
        items = payload.get("data") if isinstance(payload, dict) else None
        if not isinstance(items, list):
            raise ConfigError("Expected an OpenAI/Anthropic-compatible models response with a data array")
        found = {}
        for item in items:
            if isinstance(item, dict) and isinstance(item.get("id"), str):
                model_id = text(item["id"], "Upstream model ID")
                if model_id:
                    found[model_id] = {"id": model_id,
                                       "name": str(item.get("display_name") or item.get("name") or model_id),
                                       "owned_by": str(item.get("owned_by") or "")}
        return {"url": target, "models": sorted(found.values(), key=lambda item: item["id"].casefold()),
                "note": "模型列表仅说明网关公布了该 ID，不代表它已通过 Claude Messages 和工具调用兼容性验证。"}
