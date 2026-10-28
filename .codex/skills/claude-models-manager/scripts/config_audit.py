"""Read-only, documented configuration checks; never execute user commands."""

import json
import os
import re
from pathlib import Path


DOCS = "https://code.claude.com/docs/en/"


def read_object(path):
    try:
        value = json.loads(path.read_text(encoding="utf-8-sig"))
        return value if isinstance(value, dict) else {}
    except (OSError, ValueError):
        return {}


def inspect_context(settings, workspace):
    scope_files = []
    candidates = [(settings, "当前编辑文件"), (settings.parent / "settings.local.json", "配置目录旁的 local 文件")]
    for parent in reversed([workspace, *workspace.parents]):
        candidates.extend([(parent / ".claude/settings.json", "项目设置"),
                           (parent / ".claude/settings.local.json", "项目本地设置")])
    if os.name == "nt":
        candidates.append((Path(os.environ.get("ProgramFiles", "C:/Program Files")) / "ClaudeCode/managed-settings.json", "组织管理设置"))
    seen = set()
    for path, scope in candidates:
        if path.is_file() and str(path.resolve()) not in seen:
            seen.add(str(path.resolve()))
            scope_files.append({"path": str(path.resolve()), "scope": scope,
                                "fields": list(read_object(path))})
    homes = [Path.home() / "Documents"]
    if os.name == "nt":
        try:
            import winreg
            with winreg.OpenKey(winreg.HKEY_CURRENT_USER, r"Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders") as key:
                documents = winreg.QueryValueEx(key, "Personal")[0]
            homes.append(Path(os.path.expandvars(documents)))
        except OSError:
            pass
    wrappers = []
    for home in dict.fromkeys(homes):
        for folder in ("PowerShell", "WindowsPowerShell"):
            for filename in ("Microsoft.PowerShell_profile.ps1", "profile.ps1"):
                profile = home / folder / filename
                try:
                    content = profile.read_text(encoding="utf-8-sig")
                except (OSError, UnicodeError):
                    continue
                if "--tools" not in content:
                    continue
                # Only report literal comma-separated tool names, never shell code.
                matches = re.findall(r"\$[\w:]*Tools\s*=\s*['\"]([A-Za-z, ]+)['\"]", content, re.I)
                for tools in matches:
                    wrappers.append({"path": str(profile.resolve()), "tools": [v.strip() for v in tools.split(",")]})
    internal = read_object(Path.home() / ".claude.json")
    mcp = internal.get("mcpServers", {})
    servers = [{"name": name, "transport": spec.get("type", "stdio")} for name, spec in mcp.items()
               if isinstance(spec, dict)] if isinstance(mcp, dict) else []
    return {"platform": os.name, "scope_files": scope_files, "wrappers": wrappers, "mcp_servers": servers,
            "native_auto_updates_disabled": internal.get("autoUpdates") is False,
            "workspace": str(workspace), "user_config": settings == (Path(os.environ.get("CLAUDE_CONFIG_DIR") or Path.home() / ".claude") / "settings.json").resolve()}


def report(data, settings, context):
    findings = []
    env = data.get("env", {})
    permissions = data.get("permissions", {})
    permissions = permissions if isinstance(permissions, dict) else {}

    def add(code, level, title, detail, page, target="preferences"):
        findings.append({"id": code, "level": level, "title": title, "detail": detail,
                         "source": DOCS + page, "target": target})

    if "$schema" not in data:
        add("schema", "info", "为配置补上编辑器校验提示", "当前没有 $schema。加入后，支持 JSON Schema 的编辑器可提示字段和类型；本地体检不代替完整官方校验。", "settings")
    if env.get("DISABLE_AUTOUPDATER") == "1" or context.get("native_auto_updates_disabled"):
        add("updates", "warning", "自动更新已关闭，需维护手动升级节奏", "固定版本有助于网关兼容性。若没有固定版本需求，可选择 stable 通道并检查自动更新；旧 .claude.json 中也可能保留关闭标记，最终状态以 Claude /config 为准。", "setup#disable-auto-updates")
    else:
        add("updates", "good", "自动更新未被此文件关闭", "个人开发可考虑 stable 通道减少回归；latest 适合愿意及时验证新功能的使用方式。", "setup#configure-release-channel")
    mode = permissions.get("defaultMode", "default")
    if mode == "bypassPermissions":
        add("permission-mode", "warning", "当前跳过权限确认", "日常本机开发可改用 auto 或 acceptEdits。原生 Windows 不提供内置沙箱，权限模式与操作系统隔离是不同机制。", "permission-modes")
    elif mode == "auto":
        add("permission-mode", "good", "auto 模式适合减少日常确认", "auto 使用分类器评估工具动作，并非 bypassPermissions。网关和非 Claude 模型的分类器兼容性仍需在实际会话中核实。", "permission-modes")
    else:
        add("permission-mode", "info", "当前权限模式：" + str(mode), "default、acceptEdits、plan、auto 有不同交互成本，按项目风险与个人习惯选择即可，不需要套用统一模板。", "permission-modes")
    if env.get("CLAUDE_CODE_AUTO_MODE_SERVER") == "0":
        add("classifier", "info", "网关改用客户端分类器请求", "CLAUDE_CODE_AUTO_MODE_SERVER=0 关闭服务端分类器检查，改用 Claude Code 自己发起的分类器请求，并不关闭安全检查。该兼容开关可能随版本变化。", "auto-mode-classifier-billing")
    mapped = [env.get("ANTHROPIC_DEFAULT_" + role + "_MODEL") for role in ("OPUS", "SONNET", "HAIKU", "FABLE")]
    mapped = [value for value in mapped if value]
    if len(mapped) > 1 and len(set(mapped)) == 1:
        add("same-model", "info", "多个别名指向同一个模型", "当前别名切换不会改变实际模型。统一模型可简化费用与行为；若要按任务分层，可再分别映射快模型和强模型。", "model-config", "models")
    allow = permissions.get("allow", [])
    if isinstance(allow, list) and any(rule in ("Bash(*)", "PowerShell(*)", "mcp__*", "*") for rule in allow):
        add("broad-allow", "warning", "存在整类工具的长期放行", "宽泛 allow 会让对应动作绕过常规权限提示。建议按具体开发命令或 MCP 工具缩小范围。", "permissions")
    if context.get("user_config") and (settings.parent / "settings.local.json").is_file():
        add("local-scope", "warning", "用户目录旁的 local 文件不是全局配置入口", "官方全局入口是 ~/.claude/settings.json；.claude/settings.local.json 属于项目本地设置。不要假定旁边 local 文件中的 MCP 授权会在所有项目生效。", "settings#settings-files-and-who-they-affect", "audit")
    if context.get("platform") == "nt":
        for wrapper in context.get("wrappers", []):
            missing = [tool for tool in ("PowerShell", "Agent", "ToolSearch") if tool not in wrapper["tools"]]
            if missing:
                add("tool-wrapper-" + str(len(findings)), "warning", "PowerShell 包装器使用固定工具集", "检测到 " + wrapper["path"] + " 的固定 --tools 列表未包含 " + "、".join(missing) + "。通过该包装器启动时受影响，修改 settings.json 无法解除命令行限制。刻意保持精简可保留，需要时再单独放行对应工具；MCP 工具不受 --tools 限制。", "cli-reference#tools-available-to-claude", "audit")
        if isinstance(data.get("sandbox"), dict) and data["sandbox"].get("enabled"):
            add("windows-sandbox", "warning", "原生 Windows 不支持内置沙箱", "不能仅靠 sandbox.enabled 获得隔离。需要隔离时使用 WSL2、容器或虚拟机。", "sandboxing", "json")
    if env.get("ENABLE_TOOL_SEARCH") == "false" and context.get("mcp_servers"):
        add("tool-search", "info", "MCP 工具全部预加载", "若工具定义占用较多上下文，可试用默认按需加载。第三方模型的工具搜索兼容性需自行验证。", "mcp#configure-tool-search")
    if env.get("ANTHROPIC_BASE_URL"):
        add("gateway", "info", "当前使用模型网关", "连接本地网关是合理方案。模型列表只反映公布的 ID，不验证工具调用、自动分类器或计费行为。密钥仅在本地文件保存，页面不会回显。", "llm-gateway", "connection")
    return {"findings": findings, "counts": {level: sum(f["level"] == level for f in findings) for level in ("warning", "info", "good")},
            "scope_files": context.get("scope_files", []), "mcp_servers": context.get("mcp_servers", []),
            "workspace": context.get("workspace", ""), "rules_reviewed_on": "2026-10-28",
            "note": "依据已核对的官方文档做静态检查，不联网、不运行配置中的命令，也不替代 Claude /config、/doctor 和实际会话验证。"}
