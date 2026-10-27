# -*- coding: utf-8 -*-
"""
AI Model Evaluation Engine using deepseek-v4.1-flash with high reasoning
connected via MCP (Model Context Protocol) to WebSearch service.
"""

import json
import os
import re
import subprocess
import time
import urllib.error
import urllib.request
from typing import Any, Dict, List, Optional

import model_catalog_core as core

DEFAULT_MCP_CONFIG = {
    "command": r"D:\workspace\WebSearch\bin\websearch.exe",
    "args": ["serve", "--config", r"D:\workspace\WebSearch\config.yaml"],
}

DEFAULT_API_URL = "http://127.0.0.1:8317/v1/chat/completions"
DEFAULT_API_KEY = os.environ.get("CLIPROXY_API_KEY", "")
EVAL_MODEL_NAME = "deepseek-v4.1-flash(high)"


def get_ai_benchmark_cache_path(catalog_path: Optional[str] = None) -> str:
    cat_dir = os.path.dirname(core.get_catalog_path(catalog_path))
    return os.path.join(cat_dir, "ai_benchmark_cache.json")


def get_cached_ai_evaluation(catalog_path: Optional[str] = None) -> Optional[Dict[str, Any]]:
    cache_path = get_ai_benchmark_cache_path(catalog_path)
    if os.path.exists(cache_path):
        try:
            with open(cache_path, "r", encoding="utf-8") as f:
                return json.load(f)
        except Exception:
            pass
    return None


def save_cached_ai_evaluation(data: Dict[str, Any], catalog_path: Optional[str] = None) -> None:
    cache_path = get_ai_benchmark_cache_path(catalog_path)
    try:
        os.makedirs(os.path.dirname(cache_path), exist_ok=True)
        with open(cache_path, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
    except Exception as e:
        print(f"[Warn] Failed to save AI benchmark cache: {e}")


class WebSearchMcpClient:
    """Stdio-based MCP Client for WebSearch service."""

    def __init__(self, command: str, args: List[str]):
        self.command = command
        self.args = args
        self.proc: Optional[subprocess.Popen] = None
        self.req_id = 0

    def start(self) -> None:
        if not os.path.exists(self.command):
            raise FileNotFoundError(f"WebSearch binary not found: {self.command}")

        cmd = [self.command] + self.args
        creationflags = 0
        startupinfo = None
        if os.name == "nt":
            creationflags = getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000)
            startupinfo = subprocess.STARTUPINFO()
            startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW
            startupinfo.wShowWindow = 0  # SW_HIDE

        work_dir = os.path.dirname(os.path.dirname(os.path.abspath(self.command)))
        if not os.path.isdir(work_dir):
            work_dir = None

        self.proc = subprocess.Popen(
            cmd,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            encoding="utf-8",
            bufsize=1,
            creationflags=creationflags,
            startupinfo=startupinfo,
            cwd=work_dir,
        )

        # Initialize handshake
        init_req = {
            "jsonrpc": "2.0",
            "id": self._next_id(),
            "method": "initialize",
            "params": {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "codex-models-evaluator", "version": "1.0.0"},
            },
        }
        self._send(init_req)
        init_resp = self._read_response()
        if "error" in init_resp:
            raise RuntimeError(f"MCP Initialize error: {init_resp['error']}")

        # Notify initialized
        notify = {"jsonrpc": "2.0", "method": "notifications/initialized"}
        self._send(notify)

    def list_tools(self) -> List[Dict[str, Any]]:
        req = {"jsonrpc": "2.0", "id": self._next_id(), "method": "tools/list", "params": {}}
        self._send(req)
        resp = self._read_response()
        if "error" in resp:
            raise RuntimeError(f"MCP tools/list error: {resp['error']}")
        tools = resp.get("result", {}).get("tools", [])
        return tools

    def call_tool(self, name: str, arguments: Dict[str, Any]) -> str:
        req = {
            "jsonrpc": "2.0",
            "id": self._next_id(),
            "method": "tools/call",
            "params": {"name": name, "arguments": arguments},
        }
        self._send(req)
        resp = self._read_response()
        if "error" in resp:
            return f"Tool Error: {resp['error']}"
        result = resp.get("result", {})
        contents = result.get("content", [])
        texts = []
        for item in contents:
            if isinstance(item, dict) and "text" in item:
                texts.append(item["text"])
        if texts:
            return "\n".join(texts)
        return json.dumps(result, ensure_ascii=False)

    def _next_id(self) -> int:
        self.req_id += 1
        return self.req_id

    def _send(self, payload: Dict[str, Any]) -> None:
        if not self.proc or not self.proc.stdin:
            raise RuntimeError("MCP process not running")
        line = json.dumps(payload, ensure_ascii=False) + "\n"
        self.proc.stdin.write(line)
        self.proc.stdin.flush()

    def _read_response(self) -> Dict[str, Any]:
        if not self.proc or not self.proc.stdout:
            raise RuntimeError("MCP process not running")
        line = self.proc.stdout.readline()
        if not line:
            err_out = ""
            if self.proc.stderr:
                err_out = self.proc.stderr.read()
            raise RuntimeError(f"MCP process closed unexpectedly. Stderr: {err_out}")
        return json.loads(line)

    def close(self) -> None:
        if self.proc:
            try:
                self.proc.terminate()
                self.proc.wait(timeout=2)
            except Exception:
                try:
                    self.proc.kill()
                except Exception:
                    pass
            self.proc = None


def _normalize_chat_endpoint(url: str) -> str:
    u = (url or "").strip()
    if not u:
        return DEFAULT_API_URL
    u = u.rstrip("/")
    if u.endswith("/v1/models"):
        return u[:-7] + "/chat/completions"
    if u.endswith("/models"):
        return u[:-7] + "/chat/completions"
    if u.endswith("/v1"):
        return u + "/chat/completions"
    if not u.endswith("/chat/completions"):
        return u + "/chat/completions"
    return u


def evaluate_catalog_with_ai(
    catalog_data: Dict[str, Any],
    catalog_path: Optional[str] = None,
    api_url: Optional[str] = None,
    api_key: Optional[str] = None,
) -> Dict[str, Any]:
    """
    Evaluate configured catalog models using deepseek-v4.1-flash with high reasoning,
    incorporating live web searches through WebSearch MCP.
    """
    models = catalog_data.get("models", [])
    if not models:
        raise ValueError("当前尚未配置任何已启用模型，请先添加或开启模型后再执行评估")

    # Resolve upstream credentials
    detected = core.detect_codex_upstream_config()
    resolved_url = _normalize_chat_endpoint(api_url or detected.get("url") or DEFAULT_API_URL)
    resolved_key = (api_key or detected.get("key") or DEFAULT_API_KEY).strip()

    # Connect to WebSearch MCP
    mcp = WebSearchMcpClient(DEFAULT_MCP_CONFIG["command"], DEFAULT_MCP_CONFIG["args"])
    mcp.start()

    try:
        mcp_tools = mcp.list_tools()
        openai_tools = [
            {
                "type": "function",
                "function": {
                    "name": t["name"],
                    "description": t.get("description", ""),
                    "parameters": t.get("inputSchema", {"type": "object", "properties": {}}),
                },
            }
            for t in mcp_tools
        ]

        # Build model descriptors
        model_briefs = []
        for m in models:
            slug = m.get("slug", "")
            disp = m.get("display_name") or slug
            ctx = m.get("context_window", 1048576)
            reasoning = m.get("default_reasoning_level", "none")
            shell = m.get("shell_type", "unified_exec")
            model_briefs.append(
                f"- Slug: {slug} (显示名称: {disp}), 上下文窗口: {ctx} tokens, 默认思考: {reasoning}, 终端模式: {shell}"
            )

        models_text = "\n".join(model_briefs)

        system_prompt = (
            "你是一位世界顶级的软件工程大模型能力测评专家与架构师。\n"
            "请针对用户当前配置中已启用的 AI 模型，基于最新权威实测数据进行编程能力画像与双维度量化评估。\n\n"
            "【评估维度说明】\n"
            "1. 日常开发评分 (daily_score, 60.0 ~ 99.0分)：关注单文件敏捷代码补全、函数实现、单元测试、首字速度与极速响应能力。\n"
            "2. 复杂架构评分 (arch_score, 60.0 ~ 99.0分)：关注多文件复杂重构、端到端 Agentic 自动化开发、深层逻辑推演、长上下文全库理解与严谨度。\n\n"
            "【工具调用要求】\n"
            "你已接入 WebSearch MCP (包含 web_search 和 web_fetch 工具)。\n"
            "请结合需要调用 1~2 轮 web_search，检索关于这些待评模型在 2026 最新 Aider 榜单、SWE-bench Verified、LiveCodeBench 或官方真实代码评测的表现。\n"
            "获取到足够信息后，请给出最终 JSON 评估报告。\n\n"
            "【输出格式强制要求】\n"
            "最终回答必须严格为纯 JSON 格式（可包含在 ```json ``` 块中），格式结构必须与以下完全一致：\n"
            "{\n"
            '  "models": [\n'
            "    {\n"
            '      "slug": "模型 slug (必须与输入完全一致)",\n'
            '      "display_name": "显示名称",\n'
            '      "family": "家族名，如 deepseek / claude / gemini / glm / minimax",\n'
            '      "daily_score": 92.5,\n'
            '      "arch_score": 95.0,\n'
            '      "tag": "定位标签 (如：日常利器·极速秒响应 / 复杂架构·顶尖深度逻辑推演 / 全能旗舰·架构深度与高敏捷兼备)",\n'
            '      "tag_class": "flagship | arch | daily | general",\n'
            '      "speed": "ultra-fast | fast | medium",\n'
            '      "default_reasoning": "建议配置的思考等级 (如 high / medium / none / max)",\n'
            '      "context_window": 1048576,\n'
            '      "shell_type": "unified_exec | shell_command",\n'
            '      "strengths": ["优势特点 1", "优势特点 2", "优势特点 3", "优势特点 4"],\n'
            '      "best_for": "典型适用场景推荐 (如：日常高频代码迭代、单文件敏捷重构与单元测试编写)",\n'
            '      "priority": 1000\n'
            "    }\n"
            "  ],\n"
            '  "top_daily_pick_slug": "日常开发最佳首选模型的 slug",\n'
            '  "top_complex_pick_slug": "复杂架构最佳首选模型的 slug",\n'
            '  "ai_insights": "2-3句话总结当前模型组合的架构选型建议与搭配策略"\n'
            "}\n"
        )

        user_prompt = f"待评估的当前已启用模型列表如下：\n{models_text}\n\n请进行检索和深度思考评估，直接返回最终 JSON。"

        messages = [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt},
        ]

        headers = {
            "Authorization": f"Bearer {resolved_key}",
            "Content-Type": "application/json",
        }

        max_turns = 3
        final_json_text = ""

        for turn in range(max_turns):
            is_last_chance = turn == max_turns - 1
            body: Dict[str, Any] = {
                "model": EVAL_MODEL_NAME,
                "reasoning_effort": "high",
                "messages": messages,
                "max_tokens": 4096,
            }

            # Disable tool calls on the final turn so the model returns JSON.
            if not is_last_chance and openai_tools:
                body["tools"] = openai_tools
                body["tool_choice"] = "auto"

            req = urllib.request.Request(
                resolved_url,
                headers=headers,
                data=json.dumps(body, ensure_ascii=False).encode("utf-8"),
            )

            try:
                with urllib.request.urlopen(req, timeout=120) as resp:
                    res_data = json.loads(resp.read().decode("utf-8"))
            except urllib.error.HTTPError as he:
                err_body = he.read().decode("utf-8", errors="ignore")
                raise RuntimeError(f"Upstream API Error (HTTP {he.code}): {err_body}")
            except Exception as e:
                raise RuntimeError(f"Request failed: {e}")

            choice = res_data.get("choices", [{}])[0]
            msg = choice.get("message", {})
            messages.append(msg)

            tool_calls = msg.get("tool_calls")
            if not tool_calls:
                final_json_text = msg.get("content", "")
                break

            # Execute tool calls.
            for tc in tool_calls:
                fn = tc.get("function", {})
                fn_name = fn.get("name", "")
                args_raw = fn.get("arguments", "{}")
                try:
                    fn_args = json.loads(args_raw)
                except Exception:
                    fn_args = {}

                tool_output = mcp.call_tool(fn_name, fn_args)
                # Limit tool output to stay within the context window.
                truncated_output = tool_output[:3000] if tool_output else "无搜索结果"

                messages.append(
                    {
                        "role": "tool",
                        "tool_call_id": tc.get("id", ""),
                        "content": truncated_output,
                    }
                )

        if not final_json_text:
            # Make a fallback request for the final formatted result.
            messages.append(
                {
                    "role": "user",
                    "content": "请基于以上检索与思考，立即输出符合要求的完整评估结果 JSON，不要输出任何其他前缀或后缀。",
                }
            )
            body = {
                "model": EVAL_MODEL_NAME,
                "reasoning_effort": "high",
                "messages": messages,
                "max_tokens": 4096,
            }
            req = urllib.request.Request(
                resolved_url,
                headers=headers,
                data=json.dumps(body, ensure_ascii=False).encode("utf-8"),
            )
            try:
                with urllib.request.urlopen(req, timeout=120) as resp:
                    res_data = json.loads(resp.read().decode("utf-8"))
            except urllib.error.HTTPError as he:
                err_body = he.read().decode("utf-8", errors="ignore")
                raise RuntimeError(f"Upstream API Error (HTTP {he.code}): {err_body}")
            except Exception as e:
                raise RuntimeError(f"Request failed: {e}")
            final_json_text = res_data.get("choices", [{}])[0].get("message", {}).get("content", "")

    finally:
        mcp.close()

    # Parse and normalize the JSON result.
    parsed = _parse_ai_eval_json(final_json_text, models)
    save_cached_ai_evaluation(parsed, catalog_path)
    return parsed


def _parse_ai_eval_json(raw_text: str, original_models: List[Dict[str, Any]]) -> Dict[str, Any]:
    """Extract and validate evaluation payload, falling back safely if needed."""
    cleaned = raw_text.strip()
    match = re.search(r"```(?:json)?\s*([\s\S]*?)\s*```", cleaned)
    if match:
        cleaned = match.group(1).strip()

    data = {}
    try:
        data = json.loads(cleaned)
    except Exception:
        # Extract text between the first opening and last closing brace.
        m_brace = re.search(r"(\{[\s\S]*\})", cleaned)
        if m_brace:
            try:
                data = json.loads(m_brace.group(1))
            except Exception as e:
                print(f"[Error] Failed to parse JSON from AI response: {e}")

    ai_models_raw = data.get("models", [])
    orig_map = {m.get("slug"): m for m in original_models}
    evaluated_models = []

    for m in ai_models_raw:
        slug = m.get("slug")
        if not slug or slug not in orig_map:
            continue
        orig = orig_map[slug]
        fam_key, baseline = core.identify_model_family(slug)

        daily_score = float(m.get("daily_score", baseline.get("daily_base", 85)))
        arch_score = float(m.get("arch_score", baseline.get("arch_base", 85)))
        daily_score = max(60.0, min(99.0, round(daily_score, 1)))
        arch_score = max(60.0, min(99.0, round(arch_score, 1)))

        tag_class = m.get("tag_class")
        if tag_class not in ["flagship", "arch", "daily", "general"]:
            if daily_score >= 91.0 and arch_score >= 96.0:
                tag_class = "flagship"
            elif arch_score >= 95.0:
                tag_class = "arch"
            elif daily_score >= 91.0:
                tag_class = "daily"
            else:
                tag_class = "general"

        evaluated_models.append(
            {
                "slug": slug,
                "display_name": m.get("display_name") or orig.get("display_name") or slug,
                "family": m.get("family") or fam_key,
                "daily_score": daily_score,
                "arch_score": arch_score,
                "tag": m.get("tag") or "AI 评估智能定位",
                "tag_class": tag_class,
                "speed": m.get("speed") or baseline.get("speed", "fast"),
                "default_reasoning": m.get("default_reasoning") or orig.get("default_reasoning_level", "none"),
                "context_window": int(m.get("context_window") or orig.get("context_window", 1048576)),
                "shell_type": m.get("shell_type") or orig.get("shell_type", "unified_exec"),
                "strengths": m.get("strengths") or baseline.get("strengths", []),
                "best_for": m.get("best_for") or baseline.get("best_for", ""),
                "priority": orig.get("priority", 1000),
            }
        )

    # Use baseline scores for models omitted from the AI evaluation.
    evaluated_slugs = {m["slug"] for m in evaluated_models}
    for slug, orig in orig_map.items():
        if slug not in evaluated_slugs:
            fam_key, baseline = core.identify_model_family(slug)
            evaluated_models.append(
                {
                    "slug": slug,
                    "display_name": orig.get("display_name") or slug,
                    "family": fam_key,
                    "daily_score": float(baseline.get("daily_base", 85)),
                    "arch_score": float(baseline.get("arch_base", 84)),
                    "tag": "标准辅助·基准评定",
                    "tag_class": "general",
                    "speed": baseline.get("speed", "fast"),
                    "default_reasoning": orig.get("default_reasoning_level", "none"),
                    "context_window": int(orig.get("context_window", 1048576)),
                    "shell_type": orig.get("shell_type", "unified_exec"),
                    "strengths": baseline.get("strengths", []),
                    "best_for": baseline.get("best_for", ""),
                    "priority": orig.get("priority", 1000),
                }
            )

    # Select the top recommendations.
    top_daily_slug = data.get("top_daily_pick_slug")
    top_complex_slug = data.get("top_complex_pick_slug")

    top_daily_pick = next((m for m in evaluated_models if m["slug"] == top_daily_slug), None)
    top_complex_pick = next((m for m in evaluated_models if m["slug"] == top_complex_slug), None)

    if not top_daily_pick and evaluated_models:
        top_daily_pick = sorted(evaluated_models, key=lambda x: (x["daily_score"], -x["priority"]), reverse=True)[0]
    if not top_complex_pick and evaluated_models:
        top_complex_pick = sorted(evaluated_models, key=lambda x: (x["arch_score"], -x["priority"]), reverse=True)[0]

    now_str = time.strftime("%Y-%m-%d %H:%M:%S", time.localtime())

    return {
        "models": evaluated_models,
        "top_daily_pick": top_daily_pick,
        "top_complex_pick": top_complex_pick,
        "source_status": "ai_evaluated",
        "updated_at_str": now_str,
        "sources": [
            "DeepSeek-V4.1-Flash (High 深度思考)",
            "WebSearch MCP (实时联网搜索与抓取)",
            "Aider / SWE-bench / 官方实测动态画像",
        ],
        "ai_insights": data.get("ai_insights", "已基于 DeepSeek-V4.1-Flash 结合 WebSearch 联网检索完成全方位编程能力量化画像。"),
    }
