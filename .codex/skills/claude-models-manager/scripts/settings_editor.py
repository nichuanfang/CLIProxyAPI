"""Secret-preserving document editing and offline settings validation."""

import copy
import json
import re


KEEP_SECRET = "[KEEP_EXISTING_SECRET]"
SCHEMA = "https://json.schemastore.org/claude-code-settings.json"
MODES = {"default", "acceptEdits", "plan", "auto", "bypassPermissions", "dontAsk"}
SECRET_KEY = re.compile(r"token|secret|password|credential|authorization|cookie|api.?key|(?:^|[_-])(?:private_)?key(?:$|[_-])", re.I)
SECRET_VALUE = re.compile(r"\b(?:sk-|ghp_|github_pat_|xox[baprs]-)[\w-]+|\bbearer\s+\S+|(?:--(?:token|password|api-key)|(?:TOKEN|SECRET|API_KEY)=)\s*\S+|https?://[^\s/@]+:[^\s/@]+@", re.I)


def redact(value, key="", sensitive=False):
    sensitive = sensitive or bool(SECRET_KEY.search(key))
    if isinstance(value, dict):
        return {k: redact(v, k, sensitive) for k, v in value.items()}
    if isinstance(value, list):
        return [redact(v, key, sensitive) for v in value]
    if isinstance(value, str) and value and (sensitive or SECRET_VALUE.search(value)):
        return KEEP_SECRET
    return value


def resolve_secrets(candidate, original, key="", sensitive=False):
    sensitive = sensitive or bool(SECRET_KEY.search(key))
    if candidate == KEEP_SECRET:
        if isinstance(original, str) and redact(original, key, sensitive) == KEEP_SECRET:
            return original
        raise ValueError("密钥占位符只能保留原字段，不能移动或复制到其他字段。")
    if isinstance(candidate, dict):
        old = original if isinstance(original, dict) else {}
        return {k: resolve_secrets(v, old.get(k), k, sensitive) for k, v in candidate.items()}
    if isinstance(candidate, list):
        old = original if isinstance(original, list) else []
        return [resolve_secrets(v, old[i] if i < len(old) else None, key, sensitive) for i, v in enumerate(candidate)]
    return candidate


def parse_document(content):
    if not isinstance(content, str):
        raise ValueError("配置内容必须是 JSON 文本。")

    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ValueError("JSON 包含重复字段，请先移除重复项。")
            result[key] = value
        return result

    def invalid_constant(value):
        raise ValueError("JSON 不支持 NaN 或 Infinity。")

    try:
        return json.loads(content, object_pairs_hook=pairs, parse_constant=invalid_constant)
    except json.JSONDecodeError as exc:
        raise ValueError(f"JSON 格式错误：第 {exc.lineno} 行，第 {exc.colno} 列。") from None


def validate(data):
    if not isinstance(data, dict):
        raise ValueError("配置根节点必须是 JSON 对象。")
    env = data.get("env", {})
    if not isinstance(env, dict) or any(not isinstance(v, str) for v in env.values()):
        raise ValueError("env 必须是对象，所有环境变量值都必须是字符串。")
    permissions = data.get("permissions", {})
    if not isinstance(permissions, dict):
        raise ValueError("permissions 必须是对象。")
    for key in ("allow", "ask", "deny"):
        if key in permissions and (not isinstance(permissions[key], list) or any(not isinstance(v, str) for v in permissions[key])):
            raise ValueError(f"permissions.{key} 必须是字符串数组。")
    if "defaultMode" in permissions and permissions["defaultMode"] not in MODES:
        raise ValueError("不支持的权限模式。")
    if "autoUpdatesChannel" in data and data["autoUpdatesChannel"] not in ("stable", "latest"):
        raise ValueError("更新通道只能是 stable 或 latest。")
    for key in ("autoMemoryEnabled", "disableClaudeAiConnectors"):
        if key in data and not isinstance(data[key], bool):
            raise ValueError(f"{key} 必须是布尔值。")
    if "cleanupPeriodDays" in data and (type(data["cleanupPeriodDays"]) is not int or data["cleanupPeriodDays"] < 1):
        raise ValueError("cleanupPeriodDays 必须是至少为 1 的整数。")
    if "model" in data and not isinstance(data["model"], str):
        raise ValueError("model 必须是字符串。")
    for key in ("enabledPlugins", "hooks", "sandbox"):
        if key in data and not isinstance(data[key], dict):
            raise ValueError(f"{key} 必须是对象。")


def apply_preferences(data, changes):
    if not isinstance(changes, dict):
        raise ValueError("常用设置必须是对象。")
    allowed = {"defaultMode", "allow", "ask", "deny", "autoUpdatesChannel", "autoMemoryEnabled",
               "cleanupPeriodDays", "updates", "powershell", "toolSearch", "autoModeServer", "schema"}
    if set(changes) - allowed:
        raise ValueError("包含不支持的常用设置字段。")
    result = copy.deepcopy(data)
    for key in ("autoUpdatesChannel", "autoMemoryEnabled", "cleanupPeriodDays"):
        if key in changes:
            if changes[key] is None:
                result.pop(key, None)
            else:
                result[key] = changes[key]
    permissions = result.setdefault("permissions", {})
    for key in ("defaultMode", "allow", "ask", "deny"):
        if key in changes:
            if changes[key] is None:
                permissions.pop(key, None)
            else:
                permissions[key] = changes[key]
    if not permissions and "permissions" not in data:
        result.pop("permissions", None)
    env = result.setdefault("env", {})
    fields = {"updates": ("DISABLE_AUTOUPDATER", {"default": None, "disabled": "1"}),
              "powershell": ("CLAUDE_CODE_USE_POWERSHELL_TOOL", {"default": None, "on": "1", "off": "0"}),
              "autoModeServer": ("CLAUDE_CODE_AUTO_MODE_SERVER", {"default": None, "client": "0"})}
    for key, (native, options) in fields.items():
        if key in changes:
            if not isinstance(changes[key], str) or changes[key] not in options:
                raise ValueError(f"{key} 的选项无效。")
            value = options[changes[key]]
            if value is None:
                env.pop(native, None)
            else:
                env[native] = value
    if "toolSearch" in changes:
        value = changes["toolSearch"]
        if not isinstance(value, str) or not re.fullmatch(r"default|true|false|auto|auto:(?:100|[1-9]?\d)", value):
            raise ValueError("工具搜索可选 default、true、false、auto 或 auto:0 到 auto:100。")
        if value == "default":
            env.pop("ENABLE_TOOL_SEARCH", None)
        else:
            env["ENABLE_TOOL_SEARCH"] = value
    if not env and "env" not in data:
        result.pop("env", None)
    if "schema" in changes:
        if not isinstance(changes["schema"], bool):
            raise ValueError("schema 必须是布尔值。")
        if changes["schema"]:
            result["$schema"] = SCHEMA
        else:
            result.pop("$schema", None)
    result = resolve_secrets(result, data)
    validate(result)
    return result


def differences(before, after, path=""):
    """Return changed paths and operation types without exposing any values."""
    changes = []
    if isinstance(before, dict) and isinstance(after, dict):
        for key in sorted(before.keys() | after.keys()):
            child = path + "/" + key.replace("~", "~0").replace("/", "~1")
            if key not in before:
                changes.append({"path": child, "operation": "新增"})
            elif key not in after:
                changes.append({"path": child, "operation": "删除"})
            else:
                changes.extend(differences(before[key], after[key], child))
    elif before != after:
        changes.append({"path": path or "/", "operation": "修改"})
    return changes
