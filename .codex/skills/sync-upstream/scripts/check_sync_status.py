#!/usr/bin/env python3
"""
Inspect synchronization status between current fork branch and upstream main.
"""

import json
import subprocess
import sys

UPSTREAM_REPO = "https://github.com/router-for-me/CLIProxyAPI.git"

CORE_INTERFACES = [
    "sdk/cliproxy/auth/conductor.go",
    "sdk/cliproxy/auth/conductor_execution.go",
    "sdk/cliproxy/auth/conductor_refresh.go",
    "sdk/cliproxy/executor/types.go",
    "sdk/auth/interfaces.go",
    "internal/thinking/apply.go",
]

def run_git(args, check=True):
    res = subprocess.run(["git"] + args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    if check and res.returncode != 0:
        raise RuntimeError(f"git {' '.join(args)} failed: {res.stderr.strip()}")
    return res.stdout.strip(), res.returncode

def ensure_upstream_remote():
    remotes_out, _ = run_git(["remote", "-v"])
    has_upstream = False
    for line in remotes_out.splitlines():
        parts = line.split()
        if len(parts) >= 2 and parts[0] == "upstream":
            has_upstream = True
            break
    if not has_upstream:
        print(f"[INFO] Adding upstream remote: {UPSTREAM_REPO}")
        run_git(["remote", "add", "upstream", UPSTREAM_REPO])
    return True

def main():
    try:
        ensure_upstream_remote()
    except Exception as e:
        print(f"[ERROR] Failed to ensure upstream remote: {e}", file=sys.stderr)
        sys.exit(1)

    print("[INFO] Fetching upstream/main...")
    _, code = run_git(["fetch", "upstream", "main"], check=False)
    if code != 0:
        print("[WARN] Could not fetch upstream main. Checking with cached refs if available...", file=sys.stderr)

    # Check branch status
    current_branch, _ = run_git(["rev-parse", "--abbrev-ref", "HEAD"])
    status_out, _ = run_git(["status", "--porcelain"])
    dirty = bool(status_out.strip())

    # Ahead / Behind
    try:
        rev_count, _ = run_git(["rev-list", "--left-right", "--count", "HEAD...upstream/main"])
        ahead, behind = rev_count.split()
        ahead, behind = int(ahead), int(behind)
    except Exception:
        ahead, behind = -1, -1

    print(f"Current branch: {current_branch}")
    print(f"Working tree dirty: {dirty}")
    print(f"Commits ahead of upstream: {ahead}, behind upstream: {behind}")

    if behind == 0:
        print("[OK] Current branch is completely up to date with upstream/main.")
        sys.exit(0)

    # List incoming commits
    log_out, _ = run_git(["log", "HEAD..upstream/main", "--oneline", "-n", "15"])
    print(f"\n--- Incoming upstream commits (latest up to 15) ---")
    print(log_out if log_out else "(none)")

    # Analyze touched files
    diff_stat, _ = run_git(["diff", "--name-status", "HEAD...upstream/main"])
    changed_files = diff_stat.splitlines()

    high_risk_reasons = []
    medium_risk_files = []

    for item in changed_files:
        parts = item.split(maxsplit=1)
        if len(parts) != 2:
            continue
        status, file_path = parts[0], parts[1]
        
        # Check if core interfaces are touched
        for core in CORE_INTERFACES:
            if core in file_path:
                high_risk_reasons.append(f"Core interface changed by upstream: {file_path}")

        # Check deletions of existing files
        if status.startswith("D"):
            high_risk_reasons.append(f"File deleted by upstream: {file_path}")

        # Check files frequently modified by fork extensions
        if any(f in file_path for f in [
            "cmd/server/main.go",
            "internal/registry/models/models.json",
            "internal/registry/model_definitions.go",
            "internal/registry/model_updater.go",
            "internal/watcher/synthesizer/file.go",
            "sdk/cliproxy/service_executors.go",
            "sdk/cliproxy/service_models.go",
            "internal/api/handlers/management/auth_files_v8.go",
            "internal/api/handlers/management/oauth_sessions.go",
        ]):
            medium_risk_files.append(file_path)

    print("\n--- Risk Assessment ---")
    if high_risk_reasons:
        print("[RISK LEVEL: HIGH]")
        for reason in high_risk_reasons:
            print(f"  - {reason}")
        print("  -> Requires user confirmation before merging.")
    elif medium_risk_files:
        print("[RISK LEVEL: MEDIUM]")
        print("  Shared registry or integration files modified by upstream:")
        for mf in medium_risk_files:
            print(f"  - {mf}")
        print("  -> Safe to auto-merge if fork-specific registrations are preserved and tests pass.")
    else:
        print("[RISK LEVEL: LOW]")
        print("  Upstream changes do not overlap with fork features.")
        print("  -> Safe to auto-merge directly.")

if __name__ == "__main__":
    main()
