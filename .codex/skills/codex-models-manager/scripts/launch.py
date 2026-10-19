#!/usr/bin/env python3
"""
launch.py - Ultra-fast one-step launcher for Codex Models Manager visual editor.
1. Checks & initializes Codex config and default.json if missing.
2. Probes if web_editor is already running on port 8388 (or 8388-8408).
3. If running, opens browser immediately and exits (<50ms).
4. If not running, launches web_editor.py in background, waits for port readiness, opens browser, and exits (<500ms).
"""

import argparse
import os
import socket
import subprocess
import sys
import time
import urllib.request
import webbrowser

import model_catalog_core as core


def is_server_ready(host: str, port: int) -> bool:
    try:
        with urllib.request.urlopen(f"http://{host}:{port}/api/catalog", timeout=0.3) as resp:
            return resp.status == 200
    except Exception:
        return False


def find_active_server(host: str = "127.0.0.1", start_port: int = 8388, max_ports: int = 20) -> int:
    for p in range(start_port, start_port + max_ports):
        try:
            s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            s.settimeout(0.05)
            res = s.connect_ex((host, p))
            s.close()
            if res == 0:
                if is_server_ready(host, p):
                    return p
        except Exception:
            continue
    return 0


def main():
    parser = argparse.ArgumentParser(description="Ultra-fast launcher for Codex Visual Web Editor")
    parser.add_argument("--port", type=int, default=8388, help="Preferred port (default: 8388)")
    parser.add_argument("--no-browser", action="store_true", help="Do not open browser")
    parser.add_argument("--init-only", action="store_true", help="Only initialize Codex config without starting server")
    args = parser.parse_args()

    # 1. 自动检测并初始化 Codex 配置与 default.json
    init_res = core.ensure_codex_initialized()
    if init_res.get("catalog_initialized") or init_res.get("config_initialized"):
        print(f"[Codex Model Catalog] Auto-initialized: catalog={init_res.get('catalog_initialized')}, config={init_res.get('config_initialized')}")

    if args.init_only:
        print("[Codex Model Catalog] Configuration check and initialization complete.")
        return

    host = "127.0.0.1"
    active_port = find_active_server(host, args.port)

    if active_port > 0:
        url = f"http://{host}:{active_port}"
        print(f"[Codex Model Catalog] Visual Web Editor is active at {url}")
        if not args.no_browser:
            webbrowser.open(url)
        return

    # 若未运行，后台快速启动服务
    script_dir = os.path.dirname(os.path.abspath(__file__))
    web_editor_script = os.path.join(script_dir, "web_editor.py")

    cmd = [sys.executable, web_editor_script, "--port", str(args.port), "--no-browser"]

    creationflags = 0
    if sys.platform == "win32":
        creationflags = 0x00000008  # DETACHED_PROCESS

    subprocess.Popen(
        cmd,
        cwd=script_dir,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        creationflags=creationflags,
        close_fds=True
    )

    # 快速等待服务就绪（通常 0.2 秒内响应）
    target_port = 0
    for _ in range(30):
        time.sleep(0.1)
        active_port = find_active_server(host, args.port)
        if active_port > 0:
            target_port = active_port
            break

    if target_port == 0:
        target_port = args.port

    url = f"http://{host}:{target_port}"
    print(f"[Codex Model Catalog] Visual Web Editor started at {url}")
    if not args.no_browser:
        webbrowser.open(url)


if __name__ == "__main__":
    main()
