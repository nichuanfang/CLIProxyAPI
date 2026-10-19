import argparse
import json
import sys
from typing import Optional
import model_catalog_core as core


def cmd_list(args: argparse.Namespace) -> None:
    catalog = core.load_catalog(args.catalog)
    models = catalog.get("models", [])
    if args.json:
        print(json.dumps(models, ensure_ascii=False, indent=2))
        return

    print(f"\n[Codex Model Catalog] ({len(models)} configured models in {core.get_catalog_path(args.catalog)})")
    print(f"{'#':<3} {'Priority':<9} {'Slug':<25} {'Display Name':<25} {'Reasoning':<10} {'Shell Type':<15} {'Vis':<6}")
    print("-" * 96)
    for i, m in enumerate(models, 1):
        slug = m.get("slug", "")
        name = m.get("display_name", "")
        prio = str(m.get("priority", ""))
        reason = m.get("default_reasoning_level", "")
        shell = m.get("shell_type", "")
        vis = m.get("visibility", "")
        print(f"{i:<3} {prio:<9} {slug:<25} {name:<25} {reason:<10} {shell:<15} {vis:<6}")
    print()


def cmd_fetch(args: argparse.Namespace) -> None:
    catalog = core.load_catalog(args.catalog)
    print(f"Fetching models from {args.url} ...")
    try:
        upstream_models = core.fetch_upstream_models(url=args.url, api_key=args.key, timeout=args.timeout)
    except Exception as e:
        print(f"[Error] Failed to fetch upstream models: {e}", file=sys.stderr)
        sys.exit(1)

    comp = core.compare_models(catalog, upstream_models)
    if args.json:
        print(json.dumps(comp, ensure_ascii=False, indent=2))
        return

    print(f"\n[Upstream Discovery] ({len(upstream_models)} upstream models found, {comp['total_configured']} in catalog)")
    print(f"{'Status':<10} {'Slug / ID':<28} {'Owned By':<15} {'Display Name in Catalog'}")
    print("-" * 80)
    for item in comp["upstream_models"]:
        status = "[ENABLED]" if item["is_enabled"] else "[DISABLED]"
        slug = item["slug"]
        owned_by = item["owned_by"]
        cfg_name = item["current_config"].get("display_name", "") if item["current_config"] else "-"
        print(f"{status:<10} {slug:<28} {owned_by:<15} {cfg_name}")

    if comp["catalog_only_models"]:
        print(f"\n[Catalog-only models (not in current upstream /v1/models)]:")
        for m in comp["catalog_only_models"]:
            print(f"  - {m.get('slug')} ({m.get('display_name')})")
    print()


def cmd_add(args: argparse.Namespace) -> None:
    catalog = core.load_catalog(args.catalog)
    slug = args.slug.strip()

    new_cfg = {"slug": slug}
    if args.name:
        new_cfg["display_name"] = args.name
    else:
        new_cfg["display_name"] = slug

    if args.desc:
        new_cfg["description"] = args.desc
    if args.priority is not None:
        new_cfg["priority"] = args.priority
    if args.reasoning:
        new_cfg["default_reasoning_level"] = args.reasoning
    if args.shell:
        new_cfg["shell_type"] = args.shell
    if args.visibility:
        new_cfg["visibility"] = args.visibility

    try:
        added = core.add_model(catalog, new_cfg, template_slug=args.template)
        saved_path = core.save_catalog(catalog, args.catalog, do_backup=not args.no_backup)
        print(f"[Success] Model '{slug}' added successfully!")
        print(f"  Display Name : {added.get('display_name')}")
        print(f"  Template     : {args.template or 'default'}")
        print(f"  Priority     : {added.get('priority')}")
        print(f"  Catalog saved: {saved_path}")
    except Exception as e:
        print(f"[Error] Failed to add model '{slug}': {e}", file=sys.stderr)
        sys.exit(1)


def cmd_update(args: argparse.Namespace) -> None:
    catalog = core.load_catalog(args.catalog)
    slug = args.slug.strip()

    updates = {}
    if args.name:
        updates["display_name"] = args.name
    if args.desc:
        updates["description"] = args.desc
    if args.priority is not None:
        updates["priority"] = args.priority
    if args.reasoning:
        updates["default_reasoning_level"] = args.reasoning
    if args.shell:
        updates["shell_type"] = args.shell
    if args.visibility:
        updates["visibility"] = args.visibility

    if not updates:
        print("[Warning] No update fields specified. Pass --name, --desc, --priority, --reasoning, --shell, or --visibility.")
        return

    try:
        updated = core.update_model(catalog, slug, updates)
        saved_path = core.save_catalog(catalog, args.catalog, do_backup=not args.no_backup)
        print(f"[Success] Model '{slug}' updated successfully!")
        for k, v in updates.items():
            print(f"  {k}: {v}")
        print(f"  Catalog saved: {saved_path}")
    except Exception as e:
        print(f"[Error] Failed to update model '{slug}': {e}", file=sys.stderr)
        sys.exit(1)


def cmd_remove(args: argparse.Namespace) -> None:
    catalog = core.load_catalog(args.catalog)
    slug = args.slug.strip()

    try:
        removed = core.remove_model(catalog, slug)
        saved_path = core.save_catalog(catalog, args.catalog, do_backup=not args.no_backup)
        print(f"[Success] Model '{slug}' removed successfully!")
        print(f"  Removed display name: {removed.get('display_name')}")
        print(f"  Catalog saved: {saved_path}")
    except Exception as e:
        print(f"[Error] Failed to remove model '{slug}': {e}", file=sys.stderr)
        sys.exit(1)


def cmd_backup(args: argparse.Namespace) -> None:
    try:
        backup_path = core.create_backup(args.catalog, args.backup_dir)
        print(f"[Success] Created backup at: {backup_path}")
    except Exception as e:
        print(f"[Error] Backup failed: {e}", file=sys.stderr)
        sys.exit(1)


def cmd_backups(args: argparse.Namespace) -> None:
    bdir = core.get_backup_dir(args.backup_dir)
    items = core.list_backups(bdir)
    print(f"\n[Catalog Backups] ({len(items)} backups in {bdir})")
    print(f"{'Time':<20} {'Size':<10} {'Filename'}")
    print("-" * 70)
    for it in items:
        sz_kb = f"{it['size'] / 1024:.1f} KB"
        print(f"{it['time_str']:<20} {sz_kb:<10} {it['filename']}")
    print()


def cmd_restore(args: argparse.Namespace) -> None:
    try:
        restored_target = core.restore_backup(args.file, args.catalog)
        print(f"[Success] Restored catalog from {args.file} to {restored_target}")
    except Exception as e:
        print(f"[Error] Restore failed: {e}", file=sys.stderr)
        sys.exit(1)


def cmd_move(args: argparse.Namespace) -> None:
    catalog = core.load_catalog(args.catalog)
    slug = args.slug.strip()

    direction = None
    if args.up:
        direction = "up"
    elif args.down:
        direction = "down"
    elif args.top:
        direction = "top"
    elif args.bottom:
        direction = "bottom"

    target_idx = None
    if args.to is not None:
        # 用户输入 1-based index 转换为 0-based
        target_idx = args.to - 1

    if direction is None and target_idx is None:
        print("[Error] Please specify move direction (--up, --down, --top, --bottom) or target position (--to <1-based index>)", file=sys.stderr)
        sys.exit(1)

    try:
        new_idx = core.move_model(catalog, slug, direction=direction, target_index=target_idx)
        saved_path = core.save_catalog(catalog, args.catalog, do_backup=not args.no_backup)
        print(f"[Success] Moved model '{slug}' to position #{new_idx + 1}")
        print(f"  Catalog saved: {saved_path}")
    except Exception as e:
        print(f"[Error] Failed to move model '{slug}': {e}", file=sys.stderr)
        sys.exit(1)


def cmd_reorder(args: argparse.Namespace) -> None:
    catalog = core.load_catalog(args.catalog)
    slugs = []
    for s in args.slugs:
        for item in s.split(","):
            trimmed = item.strip()
            if trimmed:
                slugs.append(trimmed)

    if not slugs:
        print("[Error] Please provide ordered slugs", file=sys.stderr)
        sys.exit(1)

    try:
        core.reorder_models(catalog, slugs)
        saved_path = core.save_catalog(catalog, args.catalog, do_backup=not args.no_backup)
        print(f"[Success] Reordered {len(slugs)} models successfully!")
        print(f"  Catalog saved: {saved_path}")
    except Exception as e:
        print(f"[Error] Failed to reorder models: {e}", file=sys.stderr)
        sys.exit(1)


def main():
    parser = argparse.ArgumentParser(description="Codex Model Catalog CLI Manager")
    parser.add_argument("--catalog", "-c", help="Path to default.json", default=None)
    subparsers = parser.add_subparsers(dest="subcommand", required=True)

    # list
    p_list = subparsers.add_parser("list", help="List configured models in catalog")
    p_list.add_argument("--json", action="store_true", help="Output raw JSON")
    p_list.set_defaults(func=cmd_list)

    # fetch
    p_fetch = subparsers.add_parser("fetch", help="Fetch available models from /v1/models and compare")
    p_fetch.add_argument("--url", "-u", default=core.DEFAULT_UPSTREAM_URL, help="Upstream /v1/models URL")
    p_fetch.add_argument("--key", "-k", default=core.DEFAULT_API_KEY, help="Upstream API key")
    p_fetch.add_argument("--timeout", type=int, default=10, help="HTTP timeout in seconds")
    p_fetch.add_argument("--json", action="store_true", help="Output comparison JSON")
    p_fetch.set_defaults(func=cmd_fetch)

    # add
    p_add = subparsers.add_parser("add", help="Add / enable a new model in catalog")
    p_add.add_argument("slug", help="Model slug/ID (must match upstream model id)")
    p_add.add_argument("--name", "-n", help="Display name")
    p_add.add_argument("--desc", "-d", help="Description")
    p_add.add_argument("--template", "-t", help="Template model slug to clone from (e.g. deepseek-v4.1-flash)")
    p_add.add_argument("--priority", "-p", type=int, help="Priority integer")
    p_add.add_argument("--reasoning", "-r", help="Default reasoning level (none, low, medium, high, max)")
    p_add.add_argument("--shell", "-s", choices=["shell_command", "unified_exec"], help="Shell type")
    p_add.add_argument("--visibility", "-v", choices=["list", "hidden"], help="Visibility")
    p_add.add_argument("--no-backup", action="store_true", help="Do not create backup before save")
    p_add.set_defaults(func=cmd_add)

    # update
    p_update = subparsers.add_parser("update", help="Update an existing model in catalog")
    p_update.add_argument("slug", help="Existing model slug")
    p_update.add_argument("--name", "-n", help="New display name")
    p_update.add_argument("--desc", "-d", help="New description")
    p_update.add_argument("--priority", "-p", type=int, help="New priority integer")
    p_update.add_argument("--reasoning", "-r", help="Default reasoning level")
    p_update.add_argument("--shell", "-s", choices=["shell_command", "unified_exec"], help="Shell type")
    p_update.add_argument("--visibility", "-v", choices=["list", "hidden"], help="Visibility")
    p_update.add_argument("--no-backup", action="store_true", help="Do not create backup before save")
    p_update.set_defaults(func=cmd_update)

    # remove
    p_remove = subparsers.add_parser("remove", help="Remove an existing model from catalog")
    p_remove.add_argument("slug", help="Model slug to remove")
    p_remove.add_argument("--no-backup", action="store_true", help="Do not create backup before save")
    p_remove.set_defaults(func=cmd_remove)

    # backup
    p_backup = subparsers.add_parser("backup", help="Create a manual backup of default.json")
    p_backup.add_argument("--backup-dir", help="Backup destination directory")
    p_backup.set_defaults(func=cmd_backup)

    # backups
    p_backups = subparsers.add_parser("backups", help="List available backups")
    p_backups.add_argument("--backup-dir", help="Backup directory")
    p_backups.set_defaults(func=cmd_backups)

    # restore
    p_restore = subparsers.add_parser("restore", help="Restore catalog from a backup file")
    p_restore.add_argument("file", help="Backup filename or full path")
    p_restore.set_defaults(func=cmd_restore)

    # move
    p_move = subparsers.add_parser("move", help="Move a model up/down/top/bottom to adjust priority and order")
    p_move.add_argument("slug", help="Model slug to move")
    p_move_group = p_move.add_mutually_exclusive_group(required=True)
    p_move_group.add_argument("--up", action="store_true", help="Move model up by 1 position")
    p_move_group.add_argument("--down", action="store_true", help="Move model down by 1 position")
    p_move_group.add_argument("--top", action="store_true", help="Move model to the very top (highest priority)")
    p_move_group.add_argument("--bottom", action="store_true", help="Move model to the bottom")
    p_move_group.add_argument("--to", type=int, help="Move model to specific 1-based position")
    p_move.add_argument("--no-backup", action="store_true", help="Do not create backup before save")
    p_move.set_defaults(func=cmd_move)

    # reorder
    p_reorder = subparsers.add_parser("reorder", help="Reorder entire models list by giving slugs order")
    p_reorder.add_argument("slugs", nargs="+", help="Ordered slugs (space-separated or comma-separated)")
    p_reorder.add_argument("--no-backup", action="store_true", help="Do not create backup before save")
    p_reorder.set_defaults(func=cmd_reorder)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
