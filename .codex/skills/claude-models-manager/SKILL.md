---
name: claude-models-manager
description: Manage Claude Code settings.json with a local visual editor, model mappings, gateway discovery, common preferences, permission rules, secret-preserving JSON edits, configuration checks and backup restoration. Use to review or edit personal Claude Code configuration. Does not manage Codex catalogs or the Claude desktop application.
metadata:
  short-description: Manage and review Claude Code models and settings
---

# Claude Models Manager

Manage Claude Code's native `settings.json`, normally `~/.claude/settings.json`.
Honor `CLAUDE_CONFIG_DIR` when present. All scripts accept `--settings <path>`
for isolated testing or an explicitly requested alternative file.

## Visual editor

When invoked without an operational task, or asked to open the visual editor,
launch it directly:

```powershell
python D:\workspace\CLIProxyAPI\.codex\skills\claude-models-manager\scripts\launch.py --no-browser
```

Return the actual printed URL as a clickable link. The preferred port is 8390;
the launcher reuses only a matching manager instance and otherwise tries the
next nine ports. It does not initialize or modify Claude settings on startup.
Honor the user's browser rules; use `--no-browser` by default in this workspace.
For an explicit request to open a browser, use the permitted browser tool.

The editor supports five native mapping slots, default-model selection,
upstream search, connections, common preferences, permission rules, a full JSON
editor, configuration checks, backup restoration and server shutdown. Common
preferences and JSON edits show changed field paths before saving. All writes
use revision checks, atomic replacement and a complete pre-change backup.

## Configuration review and editing

For a configuration review, run `catalog_cli.py audit`. It reads settings scopes,
registered MCP names and literal `--tools` lists in common PowerShell profiles;
it does not execute hooks, shell commands, MCP tools or model requests. Report
documented findings and optional preferences separately. The rules were checked
against official documentation on 2026-10-28, not fetched live on each audit.
Use current documentation for a request about the latest behavior.

The common-preferences tab edits permission mode and rules, update channel,
the update disable switch, memory, session retention, PowerShell, MCP tool search,
the gateway classifier compatibility switch and the editor schema hint.
Only changed form fields are submitted; unknown settings are preserved.

The JSON editor loads recognized credential fields and common inline secret
patterns as `[KEEP_EXISTING_SECRET]`. Keeping a placeholder at its original path
preserves that value. Moving a placeholder is rejected. Deleting its field
deletes the saved value; the preview flags affected secret paths. Use Connection
Settings to replace credentials through a masked input. Secret detection uses
heuristics, so inspect unusual custom fields before copying a draft or output.

For CLI editing, `document` emits a JSON envelope containing the redacted
`content` string and revision. Save the decoded content to a draft file, edit it,
then preview or apply:

```powershell
python $cli audit
python $cli document
python $cli edit D:\temp\claude-settings-draft.json
python $cli edit D:\temp\claude-settings-draft.json --revision <revision-from-preview> --apply
```

Without `--apply`, `edit` validates and previews without writing. Supply the
preview/document revision with `--revision` when applying an earlier draft, so
intervening changes are rejected. Without it, the CLI reads the current revision
at invocation time. JSON validation
checks structure and known field types, rejects duplicate keys, and preserves
unknown fields; it is not a complete official JSON Schema validator.

## CLI tasks

For concrete changes, use `scripts/catalog_cli.py` directly rather than
launching the editor. Resolve scripts relative to this skill's directory.

```powershell
$cli = 'D:\workspace\CLIProxyAPI\.codex\skills\claude-models-manager\scripts\catalog_cli.py'
python $cli list
python $cli fetch --url http://127.0.0.1:8317/v1/models
python $cli set sonnet deepseek-v4.1-flash --name 'DeepSeek V4.1 Flash'
python $cli set custom my-model --name 'My Model' --description 'Gateway deployment'
python $cli select sonnet
python $cli clear custom
python $cli connection --base-url http://127.0.0.1:8317 --discovery on
python $cli backups
python $cli restore settings.<timestamp>.json
```

`select --clear-env-override` also removes model override variables from the
file when the user wants to remove those overrides. `connection --clear-auth`
explicitly removes stored authentication. To supply credentials, use
`connection --auth-token-stdin` or `fetch --key-stdin`, with a credential supplied
through stdin rather than a literal shell argument. Do not print credentials or
read the entire real settings file into tool output. The `list` command is
redacted. A blank new credential in the editor preserves the existing one.

## Claude-specific behavior

- Model mappings use `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU,FABLE}_MODEL` and
  optional `_NAME` / `_DESCRIPTION` environment fields.
- Custom uses `ANTHROPIC_CUSTOM_MODEL_OPTION`, a single extra picker entry.
  It is not an arbitrary Codex-style catalog. Do not invent sorting or priority
  settings that Claude does not consume.
- Fable and gateway model discovery require a Claude Code version that supports
  those features. The editor preserves any pre-existing capability overrides.
- Gateway discovery uses `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1`.
- `availableModels` is an existing restriction, not a custom model catalog;
  model mapping actions preserve it, along with `modelOverrides` and unrelated settings.
- Display name overrides apply to gateways/supported third-party providers.
- `/v1/models` discovery alone does not prove Messages or tool-call compatibility.
- The configured base URL must support Anthropic Messages. The saved credential
  is used for discovery only when the requested URL has the same origin as the
  configured gateway. Cross-origin queries require an explicit temporary key.
- CLI `--model`, process environment, project settings and managed policies can
  influence effective selection. Restart existing Claude sessions after saving.
- Backups contain the complete settings file and may include credentials. They
  stay under `<settings-dir>/model-manager/backups/<settings-filename>/`.
  Restoring one replaces the whole file and first backs up the current file.

Read [references/configuration.md](references/configuration.md) when changing
the implementation or investigating configuration semantics.

## Validation

```powershell
python -m unittest discover -s D:\workspace\CLIProxyAPI\.codex\skills\claude-models-manager\scripts -p 'test_*.py' -v
```

Use `--settings` with a temporary path for write tests. Do not test writes against
the user's actual Claude settings.
