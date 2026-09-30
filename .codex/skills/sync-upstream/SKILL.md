---
name: sync-upstream
description: Fetch and merge updates from the upstream CLIProxyAPI repository (https://github.com/router-for-me/CLIProxyAPI main branch) into this fork, preserving fork-specific features (such as CodeBuddy provider, custom models, and local catalog protections), auto-merging low/medium risk changes, and requesting user confirmation for high-risk breaking changes. Invoked explicitly via $sync-upstream.
---

# Sync Upstream

Sync updates from the upstream repository (`https://github.com/router-for-me/CLIProxyAPI.git`, branch `main`) into the current fork repository, retaining all custom fork features and managing risk safely.

> **Invocation Policy:** This skill is configured for **explicit user invocation only** (`allow_implicit_invocation: false`). It is triggered when explicitly mentioned as `$sync-upstream`.

## Workflow

### 1. Pre-flight & Remote Check
1. Ensure the upstream remote exists and points to `https://github.com/router-for-me/CLIProxyAPI.git`:
   ```bash
   git remote get-url upstream || git remote add upstream https://github.com/router-for-me/CLIProxyAPI.git
   ```
2. Verify the working directory is clean:
   ```bash
   git status --porcelain
   ```
   If there are uncommitted changes, pause and ask the user whether to commit or stash them before proceeding.
3. Fetch the latest upstream changes:
   ```bash
   git fetch upstream main
   ```

### 2. Risk Assessment
Run the helper script or inspect incoming commits:
```bash
python ./.codex/skills/sync-upstream/scripts/check_sync_status.py
```
Or manually inspect:
```bash
git log HEAD..upstream/main --oneline
git diff --name-status HEAD...upstream/main
```

Classify the sync according to `references/risk_matrix.md`:
- **Low Risk**: Upstream commits do not overlap with fork modifications (independent provider fixes, docs, tests, non-breaking dependencies).
- **Medium Risk**: Additive conflicts in shared integration files (e.g. `main.go`, `service_executors.go`, `service_models.go`, `models.json`, `auth_manager.go`, `file.go`).
- **High Risk**: Breaking changes to core interfaces (`coreauth.ProviderExecutor`, `sdkauth.Authenticator`, `sdktranslator.Format`, `apply.go`), deletion of modules used by the fork, or conflicting architectural refactors.

### 3. Merging & Conflict Handling

#### For Low Risk
Execute `git merge upstream/main`. If fast-forward or clean merge occurs, proceed directly to Step 4.

#### For Medium Risk
1. Run `git merge upstream/main`.
2. For any conflict in shared registration files, **retain both sides**: keep upstream's new additions and preserve all fork invariants listed in `references/fork_features.md`.
   - Never overwrite or drop CodeBuddy registration lines or model entries.
   - Ensure the catalog protection in `internal/registry/model_updater.go` (`tryRefreshModels` preserving `parsed.CodeBuddy`) is intact.
3. Mark conflicts resolved: `git add <resolved-files>`.
4. If merge is clean, commit the merge.

#### For High Risk
**Stop immediately.** Do not resolve speculatively.
Explain clearly to the user:
- What upstream changed (commits, interface diffs, deleted/moved files).
- Which fork features are affected.
- The concrete tradeoffs and options (e.g., adapt fork code to the new upstream signature, cherry-pick non-breaking commits, or postpone).
- Wait for user confirmation before taking any destructive action.

### 4. Verification & Validation
After completing any merge:
1. Format all code:
   ```bash
   gofmt -w .
   ```
2. Verify compile (MANDATORY):
   ```powershell
   go build -o test-output ./cmd/server
   if ($LASTEXITCODE -eq 0) { Remove-Item test-output } else { exit 1 }
   ```
3. Run test suite:
   ```powershell
   go test ./internal/auth/codebuddy ./internal/runtime/executor ./internal/registry ./internal/watcher/synthesizer ./sdk/auth ./sdk/cliproxy -count=1
   ```
4. Verify fork catalog sanity:
   - Check that `go test ./internal/registry -run TestCodeBuddyModels` passes.
   - Verify `models.json` retains the `"codebuddy"` section with its fixed models (`deepseek-v4.1-flash`, `glm-5.3-flashx`, `glm5.3`, `kimi-k3-2`).

### 5. Final Report
Summarize to the user:
- Number and key highlights of merged upstream commits.
- Files updated or conflicts resolved.
- Confirmation that all fork features and unit tests remain green.
