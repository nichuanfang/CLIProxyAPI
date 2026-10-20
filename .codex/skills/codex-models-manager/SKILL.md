---
name: codex-models-manager
description: Manage, update, add, remove, and reorder models in Codex configuration (C:\Users\R00021355\.codex\model-catalogs\default.json). Fetch upstream models from /v1/models, adjust model ordering/priority, launch the visual web editor, or run CLI management tasks.
metadata:
  short-description: Manage and reorder Codex models & visual web editor
---

# Codex Models Manager

Manage and maintain the model catalog for the Codex desktop application, stored at:
`C:\Users\R00021355\.codex\model-catalogs\default.json`

## Default Action & Invocation Rules

1. **Default Action (No Arguments / No Description)**:
   When the user invokes `$codex-models-manager` directly **without specific arguments or description**, or requests "启用可视化界面", "打开可视化界面", "打开编辑器":
   **Execute the fast one-step launcher immediately in a single command:**
   ```powershell
   python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\launch.py
   ```
   The launcher completes in <0.5s, auto-initializes configuration if needed, opens the browser, and prints the URL. Return the URL (`http://127.0.0.1:8388`) and ready status to the user in 1 sentence. Do NOT run exploratory tool calls or list models in terminal.

2. **Automatic Codex Configuration Initialization**:
   The manager automatically verifies:
   - Catalog JSON: `C:\Users\R00021355\.codex\model-catalogs\default.json` (auto-creates from template if missing).
   - Codex Config: `C:\Users\R00021355\.codex\config.toml` (auto-injects `model_catalog_json = "model-catalogs/default.json"` if missing).

3. **Direct CLI / Silent Execution**:
   When the user asks concrete non-visual operational tasks (e.g. "列出模型", "帮我添加 gemini-3-flash", "把 deepseek 移到最前面", "删除某个模型"), use `catalog_cli.py` directly to execute the change swiftly without launching a browser.

## Capabilities

- **Upstream Discovery**: Fetch live model lists from any OpenAI-compatible `/v1/models` endpoint (such as CLIProxyAPI at `http://127.0.0.1:8317/v1/models`), comparing what is currently enabled versus available.
- **Add / Enable Models**: Clone architecture, instructions templates, and configurations from existing models (e.g. `deepseek-v4.1-flash`, `gemini-3.8-flash-high`, `glm-5.3-flashx`) to onboard new models safely.
- **Update Models**: Adjust `display_name`, `description`, `priority`, `default_reasoning_level`, `supported_reasoning_levels`, `shell_type`, `visibility`, context windows, and output limits.
- **Reorder & Adjust Priority**: Reorder models via drag-and-drop in the Web UI, or with CLI commands (`--up`, `--down`, `--top`, `--bottom`, `--to <idx>`, `reorder`). Automatically maintains consistent array order and priority values.
- **Remove Models**: Delete unneeded models cleanly.
- **Visual Web Editor & Instant Rendering**: Dedicated static HTML (`web_editor.html`) with pre-injected data on page delivery for 0ms instant rendering, plus background asynchronous sync.
- **Automatic Safety Backups**: Every add, edit, delete, or save operation automatically generates a timestamped backup in `C:\Users\R00021355\.codex\model-catalogs\backups\`, with one-click restoration.

## Visual Web Editor

To launch the visual editing web application in the browser instantly:

```powershell
# Fast one-step launcher (auto-checks config, reuses active server, opens browser in <0.5s)
python D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\launch.py

# Or run the wrapper script
powershell -File D:\workspace\CLIProxyAPI\.codex\skills\codex-models-manager\scripts\start_web_editor.ps1
```

Features in the Web Editor:
1. **Configured Models Tab**:
   - **Drag-and-Drop Sorting**: Directly drag table rows via the handle (☰) to reorder models easily.
   - **Clean Actions**: Focused row actions for "编辑" (edit) and "删除" (delete) with automatic safety backups.
   - Search, edit parameters, and remove models with backup.
2. **Upstream Discovery Tab**: Input endpoint (default `http://127.0.0.1:8317/v1/models`) and API Key to compare models. Click `+ 开启到 Codex` to instantly clone from template and enable.
3. **Raw JSON Tab**: Directly view and edit the entire `default.json` with format validation.
4. **Backups Tab**: View backup history and roll back with one click.
5. **Coding Recommendations Tab (模型编程推荐)**: Query authoritative benchmark APIs (OpenRouter 400+ models + Aider/SWE-bench leaderboard) to evaluate active models across "日常开发" (Daily Dev) and "复杂架构" (Complex Architecture), complete with top champion recommendations, scoring matrix, and dynamic quadrant distribution.
6. **Process Lifecycle**: One-click "终止进程" in the header to safely shut down the background server and release the port.

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
