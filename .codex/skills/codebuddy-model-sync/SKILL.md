---
name: codebuddy-model-sync
description: Use when updating CLIProxyAPI CodeBuddy models. Discover installed CodeBuddy CLI model metadata, probe account availability, and update the codebuddy section of D:/workspace/CLIProxyAPI/internal/registry/models/models.json safely.
metadata:
  short-description: Sync CodeBuddy models into CLIProxyAPI
---

# CodeBuddy Model Sync

Update the `codebuddy` array in `D:\workspace\CLIProxyAPI\internal\registry\models\models.json` from the installed CodeBuddy CLI metadata and live availability.

## Source of truth (read this first)

There are three different model lists and they must not be confused:

1. **Authoritative account list** — what `--model` actually accepts for the
   signed-in account. Capture it by asking for an invalid model; the CLI replies
   with `Currently supported models for your account:` followed by the list.
   This is the only list that should drive add/remove decisions.
2. **`product.internal.json` metadata** — per-model fields (context length, max
   output, image support, descriptions). Used to fill metadata, not to decide
   support. `models` there is the broadest catalog.
3. **`agents.cli` pool in `product.internal.json`** — a routing pool, NOT the
   supported `--model` list. It contains IDs the CLI may not accept (for example
   `glm-5.2`, `kimi-k2.8-preview`, `minimax-m3-pay`). Do not present it as the
   CLI's supported models.

`discover_codebuddy_models.py` returns all three: `models` (metadata catalog),
`agent_models.cli` (routing pool), and `account_supported_models` (authoritative
account list). Always prefer `account_supported_models` when reporting which
models the CLI supports.

IDs can appear in `account_supported_models` without a `product.internal.json`
entry (for example `hy4-preview-f`, `space-bunny`, `step-5-preview`). For those,
probe live availability and either add them with fields confirmed by the probe
or report the metadata as unavailable.

## Inputs

- Required: target file is fixed to `D:\workspace\CLIProxyAPI\internal\registry\models\models.json`.
- Optional user input:
  - `--ids id1,id2`: replace the section with only these IDs.
  - `--add id1,id2`: add IDs, preserving existing ones.
  - `--remove id1,id2`: remove IDs.
  - `--metadata-only`: keep IDs unchanged, refresh metadata.
  - `--no-restart`: build/check only; do not restart PM2.
- If the user says “current CodeBuddy models”, prefer the CLI product metadata and account availability; do not invent IDs.

## Procedure

1. Resolve the CLI:

   Prefer an already-installed global CLI:

   ```powershell
   node "$env:APPDATA\npm\node_modules\@tencent-ai\codebuddy-code\bin\codebuddy" --version
   ```

   If missing or broken, install into the Codex work area:

   ```powershell
   npm install --prefix "$env:USERPROFILE\Documents\Codex\work\cbcli" @tencent-ai/codebuddy-code
   ```

2. Read CLI metadata:

   Locate `product.internal.json` in the resolved package root. Parse its top-level `models`, `agents`, `commit`, `date`, and `endpoint`.

   For each model, use these fields when present:

   - `id`
   - `name`
   - `maxInputTokens` -> `context_length`
   - `maxOutputTokens` -> `max_completion_tokens`
   - `supportsImages` -> include `image` in `supportedInputModalities`
   - `reasoning.defaultEffort` / `reasoning.supportedEfforts`
   - `credits`
   - `descriptionEn` / `descriptionZh`

   The current CLI embeds authoritative entries such as:

   - `glm-5.3-flashx`: 1,000,000 input / 131,072 output / image support.
   - `kimi-k3-2`: 1,000,000 input / 32,000 output / image support.
   - `deepseek-v4.1-flash`: 1,000,000 input / 128,000 output / image support.

   If an existing models.json ID has no matching product entry, preserve the ID and update only fields with reliable data; do not silently delete it unless the user requested removal.

3. Check account availability:

   First read `account_supported_models` from the discovery output; that is the
   authoritative list and no network probe is needed to determine membership.

   Then, for each candidate ID, run one non-mutating CLI invocation:

   ```powershell
   node <codebuddy> --model <id> -p "Say OK only." --output-format text --dangerously-skip-permissions
   ```

   Record `OK` as available. Record an error containing `service info not found` as unavailable, and capture the `Currently supported models for your account:` list printed alongside it. Do not delete unavailable IDs unless requested; report them as compatibility IDs.

   Treat `agents.cli` as a routing pool only. A model can be executable upstream but absent from the account list, and an `agents.cli` entry is not proof of `--model` support.

4. Update JSON safely:

   - Load the full models.json with `json.load`.
   - Modify only `d["codebuddy"]`.
   - Preserve all other sections and unknown model keys.
   - Keep IDs stable unless the user requested add/remove.
   - Canonical key order for a CodeBuddy entry:

     `id`, `object`, `created`, `owned_by`, `type`, `display_name`, `name`, `description`, `context_length`, `max_completion_tokens`, `supportedInputModalities`, `supportedOutputModalities`

   - Use `owned_by: "codebuddy"`, `type: "codebuddy"`, `object: "model"`, `created: 1780000000`.
   - Write UTF-8, `ensure_ascii=False`, `indent=2`, and a trailing newline.
   - Create a timestamped backup in the Codex `work/` directory before writing.

5. Validate and apply:

   From `D:\workspace\CLIProxyAPI`, run:

   ```powershell
   go test ./internal/registry -count=1
   go build ./...
   ```

   If successful and restart was not disabled:

   ```powershell
   D:\workspace\CLIProxyAPI\restart-pm2.ps1
   ```

   Then verify:

   ```powershell
   Invoke-RestMethod -Headers @{Authorization="Bearer <local-api-key>"} http://127.0.0.1:8317/v1/models
   ```

   Confirm the expected CodeBuddy IDs appear.

## Reporting

Report:

- resolved CLI version and product commit/date
- the authoritative account-supported model list captured from the CLI
- added / updated / removed / preserved IDs
- unavailable IDs found during probing
- test/build/restart result
- final `/v1/models` CodeBuddy IDs

Never claim a live model works without either a successful CLI probe or a successful request through CPA.

## Script Workflow

Run discovery and save JSON:

```powershell
python "C:\Users\R00021355\.codex\skills\codebuddy-model-sync\scripts\discover_codebuddy_models.py" > work\codebuddy-discovery.json
```

Optionally probe IDs:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "C:\Users\R00021355\.codex\skills\codebuddy-model-sync\scripts\probe_codebuddy_models.ps1" -CodeBuddy <path-to-codebuddy> -Ids "id1,id2"
```

Dry-run the update:

```powershell
python "C:\Users\R00021355\.codex\skills\codebuddy-model-sync\scripts\update_models_json.py" --discover work\codebuddy-discovery.json --metadata-only --dry-run
```

Apply:

```powershell
python "C:\Users\R00021355\.codex\skills\codebuddy-model-sync\scripts\update_models_json.py" --discover work\codebuddy-discovery.json --metadata-only
```

Use --ids, --add, or --remove instead of --metadata-only when the user requests ID changes. Review the JSON summary before running Go validation.
