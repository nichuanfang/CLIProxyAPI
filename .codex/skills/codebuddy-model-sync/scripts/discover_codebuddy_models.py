#!/usr/bin/env python3
"""Discover CodeBuddy CLI model metadata from product.internal.json.

Usage:
  discover_codebuddy_models.py [package-root]

Search order:
  1. explicit package-root argument
  2. global npm package
  3. Codex work cache: %USERPROFILE%/Documents/Codex/work/cbcli/node_modules/@tencent-ai/codebuddy-code
  4. current workspace work/cbcli/... if present

Prints JSON:
{
  "cli_version": "...",
  "package_root": "...",
  "product": {"commit": "...", "date": "...", "endpoint": "..."},
  "models": [...],
  "agent_models": {"cli": [...]},
  "account_supported_models": [...]
}

"account_supported_models" is the authoritative list the CLI returns for the
signed-in account (captured from the "--model <invalid>" error). Prefer it over
"agent_models.cli" when deciding which IDs the CLI actually accepts.
"""

from __future__ import annotations

import json
import os
import pathlib
import re
import subprocess
import sys

HOME = pathlib.Path.home()


def candidate_roots(explicit: str | None) -> list[pathlib.Path]:
    roots: list[pathlib.Path] = []
    if explicit:
        roots.append(pathlib.Path(explicit))
    appdata = os.environ.get("APPDATA")
    if appdata:
        roots.append(pathlib.Path(appdata) / "npm" / "node_modules" / "@tencent-ai" / "codebuddy-code")
    roots.extend(
        [
            HOME / "Documents" / "Codex" / "work" / "cbcli" / "node_modules" / "@tencent-ai" / "codebuddy-code",
            HOME / "Documents" / "Codex" / "2026-09-30" / "yo" / "work" / "cbcli" / "node_modules" / "@tencent-ai" / "codebuddy-code",
            pathlib.Path.cwd() / "work" / "cbcli" / "node_modules" / "@tencent-ai" / "codebuddy-code",
        ]
    )
    unique = []
    seen = set()
    for root in roots:
        key = str(root).lower()
        if key not in seen:
            seen.add(key)
            unique.append(root)
    return unique


def find_product(root: pathlib.Path) -> pathlib.Path:
    path = root / "product.internal.json"
    if path.exists():
        return path
    raise FileNotFoundError(f"product.internal.json not found under {root}")


def package_version(root: pathlib.Path) -> str:
    package = root / "package.json"
    if package.exists():
        try:
            return str(json.loads(package.read_text(encoding="utf-8")).get("version", ""))
        except Exception:
            return ""
    return ""


def cli_version(root: pathlib.Path) -> str:
    binpath = root / "bin" / "codebuddy"
    if not binpath.exists():
        return package_version(root)
    try:
        result = subprocess.run(
            ["node", str(binpath), "--version"],
            check=False,
            capture_output=True,
            text=True,
            timeout=30,
        )
        lines = [line.strip() for line in (result.stdout or result.stderr or "").splitlines() if line.strip()]
        return lines[-1] if lines else package_version(root)
    except Exception:
        return package_version(root)


def account_supported_models(root: pathlib.Path) -> list[str]:
    """Return the CLI's authoritative model list for the signed-in account.

    The CLI prints this list when asked for an unknown model, which is more
    accurate than product metadata because it reflects the account's real
    entitlements. Returns [] if the probe cannot be parsed.
    """
    binpath = root / "bin" / "codebuddy"
    if not binpath.exists():
        return []
    try:
        result = subprocess.run(
            [
                "node",
                str(binpath),
                "--model",
                "__codebuddy_model_sync_probe__",
                "-p",
                "Say OK only.",
                "--output-format",
                "text",
                "--dangerously-skip-permissions",
            ],
            check=False,
            capture_output=True,
            text=True,
            timeout=120,
        )
    except Exception:
        return []
    text = "\n".join(part for part in (result.stdout, result.stderr) if part)
    match = re.search(r"Currently supported models for your account:\s*((?:\s*-\s*\S+)+)", text)
    if not match:
        return []
    return re.findall(r"-\s*(\S+)", match.group(1))


def normalize_model(raw: dict) -> dict:
    reasoning = raw.get("reasoning") if isinstance(raw.get("reasoning"), dict) else None
    images = raw.get("supportsImages")
    return {
        "id": raw.get("id"),
        "display_name": raw.get("name"),
        "description": raw.get("descriptionEn") or raw.get("descriptionZh"),
        "context_length": raw.get("maxInputTokens"),
        "max_completion_tokens": raw.get("maxOutputTokens"),
        "supportedInputModalities": ["text", "image"] if images is True else ["text"],
        "supportedOutputModalities": ["text"],
        "supports_images": images,
        "supports_tool_call": raw.get("supportsToolCall"),
        "supports_reasoning": raw.get("supportsReasoning"),
        "reasoning": reasoning,
        "credits": raw.get("credits"),
        "vendor": raw.get("vendor"),
        "related_models": raw.get("relatedModels"),
    }


def main() -> int:
    explicit = sys.argv[1] if len(sys.argv) > 1 else None
    last_error = None
    for root in candidate_roots(explicit):
        try:
            product_path = find_product(root)
            product = json.loads(product_path.read_text(encoding="utf-8"))
            agent_models = {}
            for agent in product.get("agents", []):
                name = agent.get("name")
                models = agent.get("models")
                if name and isinstance(models, list):
                    agent_models[str(name)] = [str(x) for x in models]
            models = [
                normalize_model(m)
                for m in product.get("models", [])
                if isinstance(m, dict) and m.get("id")
            ]
            result = {
                "package_root": str(root),
                "cli_version": cli_version(root),
                "product": {
                    "commit": product.get("commit"),
                    "date": product.get("date"),
                    "endpoint": product.get("endpoint"),
                },
                "models": models,
                "agent_models": agent_models,
                "account_supported_models": account_supported_models(root),
            }
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 0
        except Exception as exc:
            last_error = exc
    print(json.dumps({"error": str(last_error)}))
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
