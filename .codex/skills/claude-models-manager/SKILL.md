---
name: claude-models-manager
description: Manage Claude Code model aliases, default models, gateway credentials and model discovery in settings.json. Open a local visual editor, discover upstream /v1/models, edit Opus/Sonnet/Haiku/Fable/Custom mappings, or restore configuration backups. Does not manage Codex catalogs or the Claude desktop application's model picker.
metadata:
  short-description: Manage Claude Code models and gateway settings
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

The editor supports five native mapping slots, a default model selector,
upstream search, connection settings, a redacted JSON preview, backup restoration,
and server shutdown. Changes are saved explicitly, with optimistic conflict
detection, atomic replacement and a complete pre-change backup.

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
  the manager preserves it, along with `modelOverrides` and all unrelated settings.
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
