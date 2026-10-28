"""Command-line model management for Claude Code; credentials are never printed."""

import argparse
import json
import sys

from claude_config import AUTH_KEYS, ConfigError, Manager, ROLES


def main():
    parser = argparse.ArgumentParser(description="Claude Models Manager")
    parser.add_argument("--settings", help="Override the Claude Code settings.json path")
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("list", help="Show model mappings and redacted connection state")
    assign = commands.add_parser("set", help="Assign a model to a Claude picker role")
    assign.add_argument("role", choices=ROLES)
    assign.add_argument("model")
    assign.add_argument("--name", default="")
    assign.add_argument("--description", default="")
    clear = commands.add_parser("clear", help="Reset a picker role to Claude defaults")
    clear.add_argument("role", choices=ROLES)
    select = commands.add_parser("select", help="Set the default alias/model; an empty string resets it")
    select.add_argument("model")
    select.add_argument("--clear-env-override", action="store_true")
    connection = commands.add_parser("connection", help="Update gateway settings")
    connection.add_argument("--base-url")
    connection.add_argument("--auth-token-stdin", action="store_true", help="Read a secret from stdin, never shell arguments")
    connection.add_argument("--auth-kind", choices=AUTH_KEYS, default=AUTH_KEYS[0])
    connection.add_argument("--clear-auth", action="store_true")
    connection.add_argument("--discovery", choices=("on", "off"))
    fetch = commands.add_parser("fetch", help="Fetch /v1/models without changing settings")
    fetch.add_argument("--url", default="")
    fetch.add_argument("--key-stdin", action="store_true")
    commands.add_parser("backups")
    commands.add_parser("audit", help="Inspect settings, scopes and launch-tool restrictions without executing them")
    commands.add_parser("document", help="Print the settings JSON with secret-preserving placeholders")
    edit = commands.add_parser("edit", help="Validate/preview a JSON file; --apply explicitly saves it")
    edit.add_argument("file")
    edit.add_argument("--revision", help="Pin an earlier document/preview revision to detect intervening changes")
    edit.add_argument("--apply", action="store_true")
    restore = commands.add_parser("restore")
    restore.add_argument("name")
    args = parser.parse_args()
    manager = Manager(args.settings)
    try:
        if args.command == "list":
            result = manager.state()
        elif args.command == "audit":
            result = manager.audit()
        elif args.command == "document":
            result = manager.document()
        elif args.command == "edit":
            from pathlib import Path
            result = manager.edit_settings({"mode": "json", "content": Path(args.file).read_text(encoding="utf-8-sig")},
                                           args.revision or manager.state()["revision"], save=args.apply)
        elif args.command == "backups":
            result = manager.backups()
        elif args.command == "fetch":
            result = manager.fetch(args.url, sys.stdin.read().strip() if args.key_stdin else None)
        else:
            revision = manager.state()["revision"]
            if args.command == "restore":
                result = manager.restore(args.name, revision)
            else:
                patch = {}
                if args.command in ("set", "clear"):
                    patch = {"roles": [{"role": args.role, "model": args.model if args.command == "set" else "",
                                        "name": args.name if args.command == "set" else "",
                                        "description": args.description if args.command == "set" else ""}]}
                elif args.command == "select":
                    patch = {"model": args.model, "clear_model_override": args.clear_env_override}
                elif args.command == "connection":
                    if args.base_url is not None:
                        patch["base_url"] = args.base_url
                    if args.auth_token_stdin:
                        patch.update(auth_token=sys.stdin.read().strip(), auth_kind=args.auth_kind)
                    if args.clear_auth:
                        patch["clear_auth"] = True
                    if args.discovery:
                        patch["discovery"] = args.discovery == "on"
                result = manager.update(patch, revision)
        print(json.dumps(result, ensure_ascii=True, indent=2))
    except (ConfigError, OSError, ValueError) as exc:
        print(f"Error: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
