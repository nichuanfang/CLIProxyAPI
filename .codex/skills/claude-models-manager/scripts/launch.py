"""Launch or reuse a local editor without changing Claude settings."""

import argparse
import json
import os
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request
import webbrowser
from pathlib import Path

from claude_config import Manager


def ready(port, settings):
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{port}/api/health", timeout=0.15) as response:
            data = json.load(response)
        return data.get("app") == "claude-models-manager" and data.get("version") == 2 and data.get("settings_path") == str(settings)
    except urllib.error.HTTPError as exc:
        exc.close()
        return False
    except (OSError, ValueError):
        return False


def main():
    parser = argparse.ArgumentParser(description="Launch Claude Models Manager")
    parser.add_argument("--port", type=int, default=8390)
    parser.add_argument("--settings")
    parser.add_argument("--no-browser", action="store_true")
    args = parser.parse_args()
    manager = Manager(args.settings)
    if not 1024 <= args.port <= 65525:
        parser.error("Port must be between 1024 and 65525")
    available = None
    active = None
    for port in range(args.port, args.port + 10):
        if ready(port, manager.path):
            active = port
            break
        if available is None:
            with socket.socket() as probe:
                try:
                    probe.bind(("127.0.0.1", port))
                    available = port
                except OSError:
                    pass
    if active is None:
        if available is None:
            parser.exit(1, "No free local editor port found\n")
        log_path = Path(os.environ.get("TEMP", str(Path.home()))) / f"claude-models-manager-{available}.log"
        command = [sys.executable, str(Path(__file__).with_name("web_editor.py")), "--port", str(available),
                   "--settings", str(manager.path)]
        with log_path.open("ab") as log:
            process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                       creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
                                       start_new_session=os.name != "nt", close_fds=True)
        for _ in range(40):
            if ready(available, manager.path):
                active = available
                break
            if process.poll() is not None:
                break
            time.sleep(0.1)
        if active is None:
            if process.poll() is None:
                process.terminate()
            parser.exit(1, f"Editor did not start; see {log_path}\n")
    url = f"http://127.0.0.1:{active}"
    print(url)
    if not args.no_browser:
        webbrowser.open(url)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
