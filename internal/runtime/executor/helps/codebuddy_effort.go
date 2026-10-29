package helps

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// codeBuddyRequiredEffortModels lists CodeBuddy models whose upstream rejects a
// request that omits reasoning_effort with error 11150 ("the reasoning effort
// value is not supported by the current model"). They also reject "none", so the
// default must be a real level rather than a disabled thinking request.
var codeBuddyRequiredEffortModels = map[string]struct{}{
	"glm-5.0-turbo": {},
	"glm-5.1":       {},
}

// EnsureCodeBuddyReasoningEffort sets a default reasoning_effort for models that
// require one. It runs before user payload rules so configured values stay final.
func EnsureCodeBuddyReasoningEffort(payload []byte, model string) []byte {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return payload
	}
	if _, ok := codeBuddyRequiredEffortModels[model]; !ok {
		return payload
	}
	if effort := gjson.GetBytes(payload, "reasoning_effort"); effort.Exists() && effort.String() != "" && effort.String() != "none" {
		return payload
	}
	updated, errSet := sjson.SetBytes(payload, "reasoning_effort", "medium")
	if errSet != nil {
		return payload
	}
	return updated
}
