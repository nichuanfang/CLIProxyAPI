# Codex Model Catalog Schema & Specifications

## Overview
Codex model catalog is stored as a JSON file at:
`C:\Users\R00021355\.codex\model-catalogs\default.json`

The root object contains an array of model descriptors under `"models"`:
```json
{
  "models": [
    { ...model_object... }
  ]
}
```

## Core Model Fields

| Field Name | Type | Description | Common Values |
|---|---|---|---|
| `slug` | string | Unique model identifier matching upstream API model name | e.g. `deepseek-v4.1-flash`, `gemini-3.8-flash-high` |
| `display_name` | string | Human-readable name shown in Codex model selector UI | e.g. `DeepSeek-V4.1-Flash`, `Gemini 3.8 Flash High` |
| `description` | string | Brief description shown in model details | Model features or capabilities summary |
| `default_reasoning_level` | string | Default reasoning effort | `none`, `low`, `medium`, `high`, `xhigh`, `max` |
| `supported_reasoning_levels` | array | Supported reasoning effort objects | `[{"effort": "high", "description": "..."}, {"effort": "max", "description": "..."}]` |
| `shell_type` | string | Terminal tool executor | `shell_command` or `unified_exec` |
| `visibility` | string | Visibility in UI model dropdown | `"list"` (shown in dropdown) or `"hidden"` |
| `supported_in_api` | boolean | Whether supported in API calls | `true` |
| `priority` | integer | Display sorting priority | `1000`, `1001`, `1002`, ... |
| `context_window` | integer | Maximum context window in tokens | e.g. `1048576` (1M) or `272000` |
| `max_context_window` | integer | Max allowed context window limit | e.g. `1048576` |
| `max_output_tokens` | integer | Maximum generation tokens | e.g. `131072`, `393216` |
| `auto_compact_token_limit` | integer | Threshold to trigger context compaction | e.g. `943718` or `244800` |
| `input_modalities` | array | Supported input types | `["text", "image"]` or `["text"]` |
| `apply_patch_tool_type` | string | Patch tool variant | `"freeform"` |
| `web_search_tool_type` | string | Web search result format | `"text"` or `"text_and_image"` |
| `supports_search_tool` | boolean | Enable search capability | `true` or `false` |
| `supports_parallel_tool_calls`| boolean | Multi-tool calling support | `true` |
| `node_repl_disabled` | boolean | Disable Node.js REPL tool | `false` |
| `multi_agent_version` | string | Multi-agent protocol version | `"v2"` |
| `model_messages` | object | Instruction template and protocol configs | Inherited from base template (`instructions_template`, etc.) |

## Upstream Model Discovery (`/v1/models`)
OpenAI-compatible `/v1/models` returns:
```json
{
  "object": "list",
  "data": [
    {
      "id": "gemini-3.8-flash-high",
      "object": "model",
      "created": 1780000000,
      "owned_by": "antigravity"
    }
  ]
}
```
The `id` field maps directly to the catalog `slug`.

## Cloning Strategy for Adding New Models
When adding an upstream model from `/v1/models`, select an existing model with similar architecture as a base template:
- **Fast / Agentic models**: clone `deepseek-v4.1-flash` or `space-bunny`
- **Large Context / Reasoning models**: clone `glm-5.3-flashx` or `kimi-k3-2`
- **Multimodal / Thinking models**: clone `gemini-3.8-flash-high`

Clone deep-copies the entire structure (including `model_messages`), then replaces `slug`, `display_name`, `description`, `priority`, and any custom reasoning levels.
