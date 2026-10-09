#!/usr/bin/env python3
"""Update CLIProxyAPI models.json codebuddy section from discovered CLI metadata.

Usage:
  update_models_json.py --discover <discover.json> [--ids id1,id2] [--add id1,id2]
                        [--remove id1,id2] [--metadata-only] [--dry-run]
                        [--models-json path]

Defaults to D:/workspace/CLIProxyAPI/internal/registry/models/models.json.
Creates a timestamped backup beside this script's caller-supplied --backup-dir or
in the current directory.

Only the top-level "codebuddy" list is changed. Existing unknown keys on each
model entry are preserved. New metadata overrides these known keys:
display_name, name, description, context_length, max_completion_tokens,
supportedInputModalities, supportedOutputModalities.
"""

from __future__ import annotations

import argparse
import collections
import datetime as dt
import json
import pathlib
import sys

DEFAULT_MODELS = pathlib.Path(
    "D:/workspace/CLIProxyAPI/internal/registry/models/models.json"
)
CANONICAL_ORDER = [
    "id",
    "object",
    "created",
    "owned_by",
    "type",
    "display_name",
    "name",
    "description",
    "context_length",
    "max_completion_tokens",
    "supportedInputModalities",
    "supportedOutputModalities",
]
METADATA_KEYS = {
    "display_name",
    "name",
    "description",
    "context_length",
    "max_completion_tokens",
    "supportedInputModalities",
    "supportedOutputModalities",
}


def parse_ids(value: str | None) -> list[str]:
    if not value:
        return []
    return [part.strip() for part in value.split(",") if part.strip()]


def load_models(path: pathlib.Path):
    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle, object_pairs_hook=collections.OrderedDict)


def discovered_index(discover: dict) -> dict[str, dict]:
    return {entry["id"]: entry for entry in discover.get("models", []) if entry.get("id")}


def canonical_entry(existing: dict, metadata: dict, model_id: str) -> collections.OrderedDict:
    entry = collections.OrderedDict()
    # Preserve unknown existing fields first so canonical order can override.
    for key, value in existing.items():
        if key not in CANONICAL_ORDER:
            entry[key] = value
    base = {
        "id": model_id,
        "object": existing.get("object", "model"),
        "created": existing.get("created", 1780000000),
        "owned_by": existing.get("owned_by", "codebuddy"),
        "type": existing.get("type", "codebuddy"),
    }
    for key, value in base.items():
        entry[key] = value
    for key in METADATA_KEYS:
        if key in metadata:
            entry[key] = metadata[key]
        elif key in existing:
            entry[key] = existing[key]
    ordered = collections.OrderedDict()
    for key in CANONICAL_ORDER:
        if key in entry:
            ordered[key] = entry[key]
    for key, value in entry.items():
        if key not in ordered:
            ordered[key] = value
    return ordered


def metadata_from_discovered(item: dict) -> dict:
    return {
        "display_name": item.get("display_name"),
        "name": item.get("id"),
        "description": item.get("description"),
        "context_length": item.get("context_length"),
        "max_completion_tokens": item.get("max_completion_tokens"),
        "supportedInputModalities": item.get("supportedInputModalities"),
        "supportedOutputModalities": item.get("supportedOutputModalities"),
    }


def write_models(path: pathlib.Path, data) -> None:
    with path.open("w", encoding="utf-8", newline="\n") as handle:
        json.dump(data, handle, ensure_ascii=False, indent=2)
        handle.write("\n")


def backup(path: pathlib.Path, backup_dir: pathlib.Path | None) -> pathlib.Path:
    destination_dir = backup_dir or path.parent
    destination_dir.mkdir(parents=True, exist_ok=True)
    stamp = dt.datetime.now().strftime("%Y%m%d-%H%M%S")
    destination = destination_dir / f"models.json.bak-codebuddy-{stamp}"
    destination.write_bytes(path.read_bytes())
    return destination


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--discover", required=True, help="JSON file from discover_codebuddy_models.py")
    parser.add_argument("--ids", help="replace section with exact IDs")
    parser.add_argument("--add", help="add IDs, preserving existing")
    parser.add_argument("--remove", help="remove IDs")
    parser.add_argument("--metadata-only", action="store_true", help="keep current IDs")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--models-json", type=pathlib.Path, default=DEFAULT_MODELS)
    parser.add_argument("--backup-dir", type=pathlib.Path)
    args = parser.parse_args()

    models_path = args.models_json
    data = load_models(models_path)
    old_section = data.get("codebuddy", [])
    old_ids = [entry.get("id") for entry in old_section if entry.get("id")]
    discover = json.loads(pathlib.Path(args.discover).read_text(encoding="utf-8"))
    index = discovered_index(discover)
    agent_ids = discover.get("agent_models", {}).get("cli")

    baseline_ids = list(old_ids)

    if args.ids:
        selected = parse_ids(args.ids)
    else:
        selected = list(old_ids)

    if args.add:
        for model_id in parse_ids(args.add):
            if model_id not in selected:
                selected.append(model_id)
    if args.remove:
        removed = set(parse_ids(args.remove))
        selected = [model_id for model_id in selected if model_id not in removed]

    entries = []
    changed = []
    unchanged = []
    missing_metadata = []
    for model_id in selected:
        existing = next((entry for entry in old_section if entry.get("id") == model_id), {})
        item = index.get(model_id)
        if item:
            metadata = metadata_from_discovered(item)
        else:
            metadata = {}
            missing_metadata.append(model_id)
        if metadata:
            metadata = {key: value for key, value in metadata.items() if value is not None}
        before = json.dumps(existing, ensure_ascii=False, sort_keys=True)
        entry = canonical_entry(existing, metadata, model_id)
        after = json.dumps(entry, ensure_ascii=False, sort_keys=True)
        if before != after:
            changed.append(model_id)
        else:
            unchanged.append(model_id)
        entries.append(entry)

    authoritative_ids = discover.get("account_supported_models") or []
    availability_hint = {
        # Authoritative account-supported list from the CLI (preferred).
        "account_supported_models": authoritative_ids,
        # Product metadata pool; broader than what --model actually accepts.
        "agent_cli_models": agent_ids,
        # Selected IDs the account cannot use: probe failures, fallback missing metadata.
        "selected_not_in_account": [
            model_id for model_id in selected if authoritative_ids and model_id not in authoritative_ids
        ],
    }

    summary = {
        "models_json": str(models_path),
        "old_ids": baseline_ids,
        "new_ids": selected,
        "changed": changed,
        "unchanged": unchanged,
        "missing_cli_metadata": missing_metadata,
        "availability": availability_hint,
        "backup": None,
        "dry_run": args.dry_run,
    }

    if args.dry_run:
        summary["preview"] = entries
        print(json.dumps(summary, ensure_ascii=False, indent=2))
        return 0

    summary["backup"] = str(backup(models_path, args.backup_dir))
    data["codebuddy"] = entries
    write_models(models_path, data)
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
