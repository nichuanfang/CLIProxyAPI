package helps

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codeBuddyClaudeCodeIdentity = "You are Claude Code, Anthropic's official CLI for Claude."

// StripCodeBuddyClaudeCodeIdentity removes the standalone Claude Code identity
// from translated system messages. CodeBuddy rejects this foreign-client
// declaration with code 11128. Other instructions and conversation text stay intact.
// Call this before applying user payload rules so explicit overrides remain final.
func StripCodeBuddyClaudeCodeIdentity(payload []byte) []byte {
	messages := gjson.GetBytes(payload, "messages")
	if !messages.IsArray() {
		return payload
	}
	keptMessages := make([]string, 0, len(messages.Array()))
	changed := false
	for _, message := range messages.Array() {
		if message.Get("role").String() != "system" {
			keptMessages = append(keptMessages, message.Raw)
			continue
		}
		content := message.Get("content")
		if content.Type == gjson.String && strings.TrimSpace(content.String()) == codeBuddyClaudeCodeIdentity {
			changed = true
			continue
		}
		if !content.IsArray() {
			keptMessages = append(keptMessages, message.Raw)
			continue
		}
		parts := content.Array()
		keptParts := make([]string, 0, len(parts))
		for _, part := range parts {
			if part.Get("type").String() == "text" && strings.TrimSpace(part.Get("text").String()) == codeBuddyClaudeCodeIdentity {
				continue
			}
			keptParts = append(keptParts, part.Raw)
		}
		if len(keptParts) == len(parts) {
			keptMessages = append(keptMessages, message.Raw)
			continue
		}
		changed = true
		if len(keptParts) == 0 {
			continue
		}
		updated, errSet := sjson.SetRaw(message.Raw, "content", "["+strings.Join(keptParts, ",")+"]")
		if errSet != nil {
			return payload
		}
		keptMessages = append(keptMessages, updated)
	}
	if !changed {
		return payload
	}
	updated, errSet := sjson.SetRawBytes(payload, "messages", []byte("["+strings.Join(keptMessages, ",")+"]"))
	if errSet != nil {
		return payload
	}
	return updated
}
