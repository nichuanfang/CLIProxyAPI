# Risk Matrix & Merge Decision Guidelines

When synchronizing changes from `upstream/main` (`https://github.com/router-for-me/CLIProxyAPI`), classify the sync risk into one of three tiers:

---

## 1. Low Risk (自动合并 / Auto-Merge)

### Criteria
- `git merge upstream/main` merges cleanly with **zero conflicts**.
- Upstream changes do not touch files modified by fork-specific features.
- Changes are purely documentation, CI workflows, or independent upstream providers (e.g. vertex, claude, gemini, xai, devin, meta).
- Non-breaking dependency updates (`go.mod` / `go.sum`).

### Action
1. Run `git merge upstream/main`.
2. Format code: `gofmt -w .`.
3. Verify compilation: `go build -o test-output ./cmd/server && Remove-Item test-output`.
4. Run quick test suite.
5. If clean, proceed automatically without interrupting the user.

---

## 2. Medium Risk (自动智能合并并验证 / Auto-Merge with Verification)

### Criteria
- Standard git conflict in shared registration/wiring files where both upstream and the fork added items:
  - `cmd/server/main.go` (new upstream CLI flags vs fork flags)
  - `sdk/cliproxy/service_executors.go` (new upstream baseline providers vs fork providers)
  - `sdk/cliproxy/service_models.go` (new upstream model dispatch vs fork dispatch)
  - `internal/registry/models/models.json` (new upstream models vs fork models)
  - `internal/registry/model_definitions.go` / `model_updater.go`
  - `internal/watcher/synthesizer/file.go`
  - `internal/api/handlers/management/auth_files_v8.go` or `oauth_sessions.go`
  - `config.example.yaml`
- Minor signature updates in internal helpers where the fix is unambiguous and mechanical (e.g. helper added a new unused parameter or context).

### Resolution Rule
1. **Never discard fork features**: Keep BOTH upstream additions and fork code.
2. Refer to `references/fork_features.md` to ensure all 13 registration invariants are retained.
3. Apply format: `gofmt -w .`.
4. Verify compilation:
   ```powershell
   go build -o test-output ./cmd/server
   if ($LASTEXITCODE -eq 0) { Remove-Item test-output }
   ```
5. Run unit tests covering the fork features:
   ```powershell
   go test ./internal/auth/codebuddy ./internal/runtime/executor ./internal/registry ./internal/watcher/synthesizer ./sdk/auth ./sdk/cliproxy -count=1
   ```
6. If build and tests pass cleanly, complete the merge commit automatically. If build fails and cannot be unambiguously fixed, escalate to **High Risk**.

---

## 3. High Risk (暂停并征求用户意见 / Request User Confirmation)

### Criteria
- **Breaking Interface Changes**:
  - Upstream changed core interface contracts used by the fork, such as:
    - `coreauth.ProviderExecutor` (e.g. changed `Execute`, `ExecuteStream`, or `Refresh` signature)
    - `coreauth.Store` / `TokenStorage`
    - `sdkauth.Authenticator`
    - `sdktranslator.Format` or translator registry architecture
- **Destructive File Changes**:
  - Upstream deleted, renamed, or completely restructured a subsystem on which fork features depend (e.g. refactored `synthesizer/file.go`, removed `model_updater.go`, or moved provider models).
- **Behavioral or Schema Conflicts**:
  - Upstream modified config version, database schemas, or credential storage semantics in a way that breaks existing fork credentials.
- **Unresolved Build or Test Failures**:
  - Code compiles with conflicts that require design choices (e.g. semantic tradeoffs, choosing between upstream behavior and fork behavior).
- **Destructive Git Operations**:
  - Fast-forward impossible and user explicitly requested rebase; or history rewrite would orphan fork commits.

### Action
1. **Stop execution immediately.** Do not force-resolve or commit speculative changes.
2. Present a clear explanation to the user:
   - What upstream changed (specific commits, files, and interface diffs).
   - What fork features are affected (e.g. CodeBuddy executor, auth file synthesis).
   - The conflict or incompatibility details.
   - 2-3 concrete options (e.g., adapt fork implementation to the new upstream interface, postpone merge, or cherry-pick specific commits).
3. Wait for user instructions before taking action.
