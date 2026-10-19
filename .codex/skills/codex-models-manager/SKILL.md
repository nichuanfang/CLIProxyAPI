---
name: codex-models-manager
description: Manage, update, add, remove, and reorder models in Codex configuration (C:\Users\R00021355\.codex\model-catalogs\default.json). Fetch upstream models from /v1/models, adjust model ordering/priority, launch the visual web editor, or run CLI management tasks.
metadata:
  short-description: Manage and reorder Codex models & visual web editor
---

# Codex Models Manager

Manage and maintain the model catalog for the Codex desktop application, stored at:
`C:\Users\R00021355\.codex\model-catalogs\default.json`

## When to Launch Visual Web Editor

- **Launch Web Editor**: When the user explicitly asks to "open the editor", "launch visual interface", "可视化编辑", or interactively browse/configure models in the browser. In such cases, start the web server in the background and share the local URL (`http://127.0.0.1:8388`).
- **Direct CLI / Silent Execution**: When the user asks concrete operational tasks (e.g. "列出模型", "帮我添加 gemini-3-flash", "把 deepseek 移到最前面", "删除某个模型"), use `catalog_cli.py` or the Python module directly to execute the change swiftly without popping up a browser.

## Capabilities

- **Upstream Discovery**: Fetch live model lists from any OpenAI-compatible `/v1/models` endpoint (such as CLIProxyAPI at `http://127.0.0.1:8317/v1/models`), comparing what is currently enabled versus available.
- **Add / Enable Models**: Clone architecture, instructions templates, and configurations from existing models (e.g. `deepseek-v4.1-flash`, `gemini-3.8-flash-high`, `glm-5.3-flashx`) to onboard new models safely.
- **Update Models**: Adjust `display_name`, `description`, `priority`, `default_reasoning_level`, `supported_reasoning_levels`, `shell_type`, `visibility`, context windows, and output limits.
- **Reorder & Adjust Priority**: Reorder models via drag-and-drop in the Web UI, or with CLI commands (`--up`, `--down`, `--top`, `--bottom`, `--to <idx>`, `reorder`). Automatically maintains consistent array order and priority values.
- **Remove Models**: Delete unneeded models cleanly.
- **Visual Web Editor**: Launch an embedded, zero-dependency browser-based GUI for visual model management, template cloning, live diffing, drag/reorder, and raw JSON editing.
- **Automatic Safety Backups**: Every add, edit, delete, or save operation automatically generates a timestamped backup in `C:\Users\R00021355\.codex\model-catalogs\backups\`, with one-click restoration.

## Visual Web Editor

To launch the visual editing web application in the browser:

```powershell
# Default port: 8388 (auto-opens default browser)
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\web_editor.py

# Or run the wrapper script
powershell -File D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\start_web_editor.ps1
```

Features in the Web Editor:
1. **Configured Models Tab**:
   - **Drag-and-Drop Sorting**: Directly drag table rows to reorder models.
   - **Quick Action Buttons**: One-click to Top (🔝), Up (▲), Down (▼), or Bottom (🔚).
   - Search, edit parameters, and remove models with backup.
2. **Upstream Discovery Tab**: Input endpoint (default `http://127.0.0.1:8317/v1/models`) and API Key to compare models. Click `+ 开启到 Codex` to instantly clone from template and enable.
3. **Raw JSON Tab**: Directly view and edit the entire `default.json` with format validation.
4. **Backups Tab**: View backup history and roll back with one click.

## CLI Commands

Use `scripts/catalog_cli.py` for automated or terminal operations:

### 1. List Current Configured Models
```powershell
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py list
```

### 2. Fetch and Compare with Upstream /v1/models
```powershell
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py fetch --url http://127.0.0.1:8317/v1/models --key sk-U6cnDx0z3xtOGLoZXSGo
```

### 3. Add / Enable a New Model
Clone from an existing template (e.g. `gemini-3.8-flash-high` or `deepseek-v4.1-flash`):
```powershell
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py add gemini-3-flash --name "Gemini 3 Flash" --template gemini-3.8-flash-high --desc "Fast multimodal thinking model"
```

### 4. Update an Existing Model
```powershell
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py update deepseek-v4.1-flash --name "DeepSeek V4.1 Flash" --reasoning high --priority 990
```

### 5. Adjust Model Order & Priority
Move a single model up/down/top/bottom, or to an exact position:
```powershell
# Move to the very top (highest priority)
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py move deepseek-v4.1-flash --top

# Move up or down by 1 position
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py move gemini-3.8-flash-high --up
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py move space-bunny --down

# Move to specific 1-based position (e.g. position #2)
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py move kimi-k3-2 --to 2

# Specify full order for all models
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py reorder deepseek-v4.1-flash,gemini-3.8-flash-high,space-bunny,glm-5.3-flashx
```

### 6. Remove a Model
```powershell
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py remove model-slug-to-delete
```

### 7. Backups and Rollback
```powershell
# List backups
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py backups

# Restore a backup
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\catalog_cli.py restore default.json.<timestamp>.bak
```

## Reference Documentation

For detailed catalog schema fields and template structures, see [references/catalog_schema.md](references/catalog_schema.md).
