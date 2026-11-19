package helps

import (
	"strconv"

	"github.com/MoeForge/moego/ojson"
)

// NormalizeCodeBuddyDeveloperRole rewrites OpenAI "developer" messages to
// "system". CodeBuddy's channel verification rejects any request that carries a
// developer-role message with code 11128 ("Illegal API invocation from an
// unapproved channel"), regardless of that message's content and regardless of
// the request headers. The official client only sends system/user/assistant/tool
// roles, and "developer" is the OpenAI alias of "system", so folding one into
// the other keeps the instruction semantics intact.
// Call this before applying user payload rules so explicit overrides remain final.
func NormalizeCodeBuddyDeveloperRole(payload []byte) []byte {
	messages := ojson.GetBytes(payload, "messages")
	if !messages.IsArray() {
		return payload
	}
	doc := ojson.NewDoc(payload)
	changed := false
	for i, message := range messages.Array() {
		if message.Get("role").String() != "developer" {
			continue
		}
		if errSet := doc.Set("messages."+strconv.Itoa(i)+".role", "system"); errSet != nil {
			return payload
		}
		changed = true
	}
	if !changed {
		return payload
	}
	return doc.Bytes()
}
