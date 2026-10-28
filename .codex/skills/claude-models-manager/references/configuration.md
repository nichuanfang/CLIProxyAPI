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
entries to provider IDs. Neither is treated as an editable arbitrary catalog.
All unmanaged fields, including capability declarations, are preserved. A model
mapping can point at a non-Claude ID if the gateway handles Anthropic Messages
and the required tool calling behavior.

The editor sends a random per-server request token on mutations, requires a
loopback Host and same-origin requests, disables cross-origin access and never
returns secrets through its APIs. Discovery refuses redirects to avoid leaking
credentials. There are no third-party frontend dependencies or remote assets.

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

The documentation mirror below was queried through Context7 because direct
requests to the documentation domain returned HTTP 403:

- [Model configuration mirror](https://github.com/pjbrito/claude-code-docs/blob/main/content/en/docs/claude-code/model-config.md)

Fable, custom picker labels and automatic gateway discovery depend on client
version/provider support. A successful discovery response is not a live model
inference test; the manager does not spend tokens testing requests automatically.
