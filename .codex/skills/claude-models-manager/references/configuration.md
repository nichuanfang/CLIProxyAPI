# Claude Code configuration notes

The manager edits the user settings file, not `.claude.json`, account tokens,
project settings, managed policies or the Claude desktop application. Startup
and discovery are read-only. Model configuration follows Claude Code's native
settings rather than creating a private model catalog.

| Control | Native field |
| --- | --- |
| Default alias or model ID | `model` |
| Opus, Sonnet, Haiku, Fable mappings | `env.ANTHROPIC_DEFAULT_<ROLE>_MODEL` |
| Mapping display name/description | Mapping key plus `_NAME` / `_DESCRIPTION` |
| Extra picker entry | `env.ANTHROPIC_CUSTOM_MODEL_OPTION` |
| Extra entry display fields | Custom key plus `_NAME` / `_DESCRIPTION` |
| Gateway root | `env.ANTHROPIC_BASE_URL` |
| Bearer credential | `env.ANTHROPIC_AUTH_TOKEN` |
| API key credential | `env.ANTHROPIC_API_KEY` |
| Automatic gateway model discovery | `env.CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY` |

`ANTHROPIC_MODEL` and `ANTHROPIC_DEFAULT_MODEL` are preserved unless explicitly
cleared through the default-model selector or CLI option. They can affect the
effective default. Process environment values are reported by variable name
only. The manager cannot determine every override that a separately launched
Claude session receives.

`availableModels` restricts selection. `modelOverrides` remaps recognized picker
entries to provider IDs. Neither is treated as an arbitrary model catalog.
Model and preference actions preserve unmanaged fields, including capability declarations. A model
mapping can point at a non-Claude ID if the gateway handles Anthropic Messages
and the required tool calling behavior.

The editor sends a random per-server request token on mutations, requires a
loopback Host and same-origin requests, disables cross-origin access and never
returns saved gateway credentials through its APIs. Discovery refuses redirects to avoid leaking
credentials. There are no third-party frontend dependencies or remote assets.

The full JSON editor masks recognized credential field names and common inline
secret patterns. This is heuristic redaction, not a guarantee for arbitrary
custom fields. Retained placeholders restore the original value at the same
path. JSON edits can explicitly change other settings, remove fields or add
new client options. Duplicate JSON keys, invalid basic types and stale revisions
are rejected. Validation runs offline and is not a full official schema check.
The change preview includes operation types and JSON Pointer paths, never values.

Common preferences edit only fields changed in the form. They support
`permissions.defaultMode` and `allow`/`ask`/`deny`, `autoUpdatesChannel`,
`autoMemoryEnabled`, `cleanupPeriodDays`, `$schema`, `DISABLE_AUTOUPDATER`,
`CLAUDE_CODE_USE_POWERSHELL_TOOL`, `ENABLE_TOOL_SEARCH` and
`CLAUDE_CODE_AUTO_MODE_SERVER`. Session retention must be at least one day.
Unsetting the file's update-disable variable does not promise to override old
internal update flags, process environment, installation method or managed policy.

The static configuration report reads nearby settings-file metadata, global MCP
server names and literal tool lists in common PowerShell profiles. It never
executes user commands and does not merge policies into an authoritative effective
configuration. A shell wrapper can restrict built-in tools even when settings
enable a tool. The `--tools` flag does not restrict MCP tools.

`CLAUDE_CODE_AUTO_MODE_SERVER=0` disables server classifier checks in favor of
requests initiated by Claude Code. It does not disable auto-mode safety checks.
The compatibility switch is temporary according to current documentation.
Do not flag it as equivalent to bypassing permissions. Auto-mode compatibility
with an arbitrary non-Claude gateway model requires actual session validation.

Backups store the original JSON text and whether the settings file existed.
Restoring a snapshot from before initial creation removes the settings file,
after backing up its current contents. Every mutation requires the revision
that was read; a stale revision is rejected instead of overwriting external edits.
Concurrent writes from this manager are serialized; an external program writing
in the small final filesystem replacement window cannot be fully locked out.

## Sources

Configuration was checked for the installed Claude Code 2.1.296:

- [Claude Code model configuration](https://code.claude.com/docs/en/model-config)
- [Claude Code settings](https://code.claude.com/docs/en/settings)
- [Claude Code gateway configuration](https://code.claude.com/docs/en/llm-gateway)
- [Official changelog](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)
- [Settings reference](https://code.claude.com/docs/en/settings-reference)
- [Permission modes](https://code.claude.com/docs/en/permission-modes)
- [Classifier behavior and gateway compatibility](https://code.claude.com/docs/en/auto-mode-classifier-billing)
- [Tool loading](https://code.claude.com/docs/en/mcp#configure-tool-search)
- [Native PowerShell tool](https://code.claude.com/docs/en/tools-reference)

Additional configuration checks were reviewed on 2026-10-28 using the official
documentation indexed by Context7 at `/websites/code_claude`. Some documentation
describes newer client versions; the report does not claim that every current
feature works in every installed version.

The documentation mirror below was queried through Context7 because direct
requests to the documentation domain returned HTTP 403:

- [Model configuration mirror](https://github.com/pjbrito/claude-code-docs/blob/main/content/en/docs/claude-code/model-config.md)

Fable, custom picker labels and automatic gateway discovery depend on client
version/provider support. A successful discovery response is not a live model
inference test; the manager does not spend tokens testing requests automatically.
