package helps

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// codeBuddyDefaultReasoningEffort is the level used when a CodeBuddy model
// rejects a request without an explicit reasoning_effort.
const codeBuddyDefaultReasoningEffort = "medium"

// codeBuddyRequiredEffortModel reports whether upstream rejects a request that
// omits reasoning_effort with error 11150 ("the reasoning effort value is not
// supported by the current model"). The catalog marks these models as unable to
// disable thinking, and they also reject "none", so the default must be a real
// level rather than a disabled thinking request. Deriving this from the catalog
// keeps newly added models covered after a remote catalog refresh.
func codeBuddyRequiredEffortModel(model string) bool {
	info := registry.LookupModelInfo(model, "codebuddy")
	if info == nil || info.Thinking == nil || info.Thinking.ZeroAllowed {
		return false
	}
	return len(info.Thinking.Levels) > 0
}

// EnsureCodeBuddyReasoningEffort sets a default reasoning_effort for models that
// require one. It runs before user payload rules so configured values stay final.
func EnsureCodeBuddyReasoningEffort(payload []byte, model string) []byte {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return payload
	}
	if !codeBuddyRequiredEffortModel(strings.TrimSpace(model)) {
		return payload
	}
	if effort := gjson.GetBytes(payload, "reasoning_effort"); effort.Exists() && effort.String() != "" && effort.String() != "none" {
		return payload
	}
	updated, errSet := sjson.SetBytes(payload, "reasoning_effort", codeBuddyDefaultReasoningEffort)
	if errSet != nil {
		return payload
	}
	return updated
}
