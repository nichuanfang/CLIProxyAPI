# Fork Features & Invariants Catalog

This document details the custom features and code paths specific to this fork repository. During any upstream merge or rebase, these components and their registration wiring **must be preserved**.

---

## 1. CodeBuddy Provider Integration

### Standalone Files (Must never be deleted or replaced)
- `internal/auth/codebuddy/client.go`: Device auth client (state, poll, refresh, headers).
- `internal/auth/codebuddy/token.go`: Token storage, file serialization (`SaveTokenToFile`), expiry check.
- `internal/auth/codebuddy/client_test.go`, `token_test.go`: Unit tests.
- `internal/runtime/executor/codebuddy_executor.go`: CodeBuddy runtime executor (`Identifier() == "codebuddy"`, endpoint `/v2/chat/completions`, headers, SSE parsing).
- `internal/runtime/executor/codebuddy_executor_test.go`: End-to-end non-stream, streaming, and tool conversion tests.
- `sdk/auth/codebuddy.go`: Authenticator implementing `Login()` and `RefreshLead() = 30*24h`.
- `sdk/auth/codebuddy_test.go`: Refresh lead tests.
- `internal/cmd/codebuddy_login.go`: CLI login handler `DoCodeBuddyLogin`.
- `internal/api/handlers/management/codebuddy_oauth.go`: V8 management endpoint `RequestCodeBuddyToken`.

### Shared Registration Invariants
When upstream modifies these shared files, merge conflicts may occur. Always keep BOTH upstream changes AND the CodeBuddy lines:

1. **`cmd/server/main.go`**:
   - Flag declaration: `var codebuddyLogin bool`
   - Flag binding: `flag.BoolVar(&codebuddyLogin, "codebuddy-login", false, ...)`
   - `commandMode` condition: `|| codebuddyLogin`
   - Dispatch: `else if codebuddyLogin { cmd.DoCodeBuddyLogin(cfg, options) }`
   - `argvFlagConsumesValue`: include `"codebuddy-login"`

2. **`internal/cmd/auth_manager.go`**:
   - `newAuthManager()`: includes `sdkAuth.NewCodeBuddyAuthenticator()`

3. **`sdk/auth/refresh_registry.go`**:
   - `init()`: `registerRefreshLead("codebuddy", func() Authenticator { return NewCodeBuddyAuthenticator() })`

4. **`sdk/cliproxy/service_auth.go`**:
   - `newDefaultAuthManager()`: includes `sdkAuth.NewCodeBuddyAuthenticator()`

5. **`sdk/cliproxy/service_executors.go`**:
   - `baselineExecutorAuths()`: includes `"codebuddy"`
   - `registerExecutorForAuth()`: `case "codebuddy": s.coreManager.RegisterExecutor(executor.NewCodeBuddyExecutor(cfg))`

6. **`sdk/cliproxy/service_models.go`**:
   - `registerModelsForAuthWithCache()`:
     ```go
     case "codebuddy":
         models = registry.GetCodeBuddyModels()
         models = applyExcludedModels(models, excluded)
     ```

7. **`internal/registry/model_definitions.go`**:
   - `staticModelsJSON`: field `CodeBuddy []*ModelInfo json:"codebuddy"`
   - Function: `GetCodeBuddyModels() []*ModelInfo`
   - Channel switch in `GetStaticModelDefinitionsByChannel`: `case "codebuddy": return GetCodeBuddyModels()`
   - `LookupStaticModelInfo`: `allModels` slice includes `data.CodeBuddy`

8. **`internal/registry/models/models.json`**:
   - Top-level `"codebuddy": [...]` array with the 4 fixed models:
     - `deepseek-v4.1-flash`
     - `glm-5.3-flashx`
     - `glm5.3`
     - `kimi-k3-2`

9. **`internal/registry/model_updater.go`**:
   - In `tryRefreshModels()`:
     ```go
     if len(parsed.CodeBuddy) == 0 && oldData != nil && len(oldData.CodeBuddy) > 0 {
         parsed.CodeBuddy = oldData.CodeBuddy
     }
     ```
     *(CRITICAL: Prevents remote catalog updates from wiping local CodeBuddy models)*
   - `detectChangedProviders`: section `{"codebuddy", oldData.CodeBuddy, newData.CodeBuddy}`
   - `validateModelsCatalog`: section `{name: "codebuddy", models: data.CodeBuddy}`

10. **`internal/watcher/synthesizer/file.go`**:
    - Synthesizer logic for `provider == "codebuddy"` setting `a.Attributes["base_url"]`

11. **`internal/api/handlers/management/auth_files_v8.go`**:
    - `StartOAuthV8`: `case "codebuddy": h.RequestCodeBuddyToken(c)`

12. **`internal/api/handlers/management/oauth_sessions.go`**:
    - `NormalizeOAuthProvider`: `case "codebuddy": return "codebuddy", nil`

13. **`config.example.yaml`**:
    - "Supported channels" comments include `codebuddy`.
    - Sample model-alias / excluded-models entries for `codebuddy`.

---

## 2. Additional Custom Features in Future
If future commits add other fork features, identify them via:
```bash
git log upstream/main..HEAD --oneline
git diff upstream/main...HEAD --stat
```
Verify any file modified by those commits is similarly preserved during upstream sync.
