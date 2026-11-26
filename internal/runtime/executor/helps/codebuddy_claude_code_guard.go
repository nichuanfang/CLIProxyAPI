package helps

import (
	"github.com/MoeForge/moego/ojson"
	"github.com/MoeForge/moego/regexp"
)

// codeBuddyClaudeCodeRule describes one CodeBuddy channel-verification
// fingerprint emitted by Claude Code. Keep rules declarative: adding a new
// fingerprint should only require another table entry, not traversal changes.
type codeBuddyClaudeCodeRule struct {
	name        string
	pattern     *regexp.Regexp
	action      codeBuddyClaudeCodeRuleAction
	replacement string
}

type codeBuddyClaudeCodeRuleAction uint8

const (
	codeBuddyClaudeCodeReplace codeBuddyClaudeCodeRuleAction = iota
	codeBuddyClaudeCodeDrop
)

// codeBuddyClaudeCodeRules only inspects system messages. Drop rules are
// anchored to the full text block so a project instruction that merely mentions
// a marker is never discarded.
var codeBuddyClaudeCodeRules = []codeBuddyClaudeCodeRule{
	{
		name:    "claude-code-identity",
		pattern: regexp.MustCompile(`(?i)^\s*You are Claude Code, Anthropic's official CLI for Claude\.?\s*$`),
		action:  codeBuddyClaudeCodeDrop,
	},
	{
		name:    "anthropic-billing-header",
		pattern: regexp.MustCompile(`(?i)^\s*x-anthropic-billing-header:[^\r\n]*\s*$`),
		action:  codeBuddyClaudeCodeDrop,
	},
	{
		name:        "claude-code-git-main-branch",
		pattern:     regexp.MustCompile(`(?i)Main branch \(you will usually use this for PRs\):`),
		action:      codeBuddyClaudeCodeReplace,
		replacement: "Main branch:",
	},
}

// codeBuddyClaudeCodeGuard applies the CodeBuddy-specific Claude Code
// compatibility rules to a translated payload.
type codeBuddyClaudeCodeGuard struct {
	rules []codeBuddyClaudeCodeRule
}

var defaultCodeBuddyClaudeCodeGuard = codeBuddyClaudeCodeGuard{rules: codeBuddyClaudeCodeRules}

// SanitizeCodeBuddyClaudeCodePayload removes CodeBuddy channel-verification
// fingerprints that Claude Code carries in system messages. Only system messages
// are touched. Other instructions and conversation text stay intact. Call this
// before applying user payload rules so explicit overrides remain final.
func SanitizeCodeBuddyClaudeCodePayload(payload []byte) []byte {
	return defaultCodeBuddyClaudeCodeGuard.Apply(payload)
}

func (g codeBuddyClaudeCodeGuard) Apply(payload []byte) []byte {
	doc := ojson.NewDoc(payload)
	messages := doc.Get("messages")
	if !messages.IsArray() {
		return payload
	}
	keptMessages := ojson.NewDocString("[]")
	changed := false
	for _, message := range messages.Array() {
		if message.Get("role").String() != "system" {
			if errAppend := keptMessages.SetRawBytes("-1", []byte(message.Raw)); errAppend != nil {
				return payload
			}
			continue
		}
		content := message.Get("content")
		if content.Type == ojson.String {
			scrubbed, scrubbedAny := g.scrubText(content.String())
			if !scrubbedAny {
				if errAppend := keptMessages.SetRawBytes("-1", []byte(message.Raw)); errAppend != nil {
					return payload
				}
				continue
			}
			changed = true
			if scrubbed == "" {
				continue
			}
			messageDoc := ojson.NewDoc([]byte(message.Raw))
			if errSet := messageDoc.Set("content", scrubbed); errSet != nil {
				return payload
			}
			if errAppend := keptMessages.SetRawBytes("-1", messageDoc.Bytes()); errAppend != nil {
				return payload
			}
			continue
		}
		if !content.IsArray() {
			if errAppend := keptMessages.SetRawBytes("-1", []byte(message.Raw)); errAppend != nil {
				return payload
			}
			continue
		}
		parts := content.Array()
		keptParts := ojson.NewDocString("[]")
		keptPartCount := 0
		messageChanged := false
		for _, part := range parts {
			if part.Get("type").String() != "text" {
				if errAppend := keptParts.SetRawBytes("-1", []byte(part.Raw)); errAppend != nil {
					return payload
				}
				keptPartCount++
				continue
			}
			scrubbed, scrubbedAny := g.scrubText(part.Get("text").String())
			if !scrubbedAny {
				if errAppend := keptParts.SetRawBytes("-1", []byte(part.Raw)); errAppend != nil {
					return payload
				}
				keptPartCount++
				continue
			}
			messageChanged = true
			if scrubbed == "" {
				continue
			}
			partDoc := ojson.NewDoc([]byte(part.Raw))
			if errSet := partDoc.Set("text", scrubbed); errSet != nil {
				return payload
			}
			if errAppend := keptParts.SetRawBytes("-1", partDoc.Bytes()); errAppend != nil {
				return payload
			}
			keptPartCount++
		}
		if !messageChanged {
			if errAppend := keptMessages.SetRawBytes("-1", []byte(message.Raw)); errAppend != nil {
				return payload
			}
			continue
		}
		changed = true
		if keptPartCount == 0 {
			continue
		}
		messageDoc := ojson.NewDoc([]byte(message.Raw))
		if errSet := messageDoc.SetRawBytes("content", keptParts.Bytes()); errSet != nil {
			return payload
		}
		if errAppend := keptMessages.SetRawBytes("-1", messageDoc.Bytes()); errAppend != nil {
			return payload
		}
	}
	if !changed {
		return payload
	}
	if errSet := doc.SetRawBytes("messages", keptMessages.Bytes()); errSet != nil {
		return payload
	}
	return doc.Bytes()
}

// scrubText applies the ordered rule set to one system text block. A dropped
// block is returned as an empty string. The bool reports whether any rule matched.
func (g codeBuddyClaudeCodeGuard) scrubText(text string) (string, bool) {
	matched := false
	for _, rule := range g.rules {
		if !rule.pattern.MatchString(text) {
			continue
		}
		matched = true
		switch rule.action {
		case codeBuddyClaudeCodeDrop:
			return "", true
		case codeBuddyClaudeCodeReplace:
			text = rule.pattern.ReplaceAllString(text, rule.replacement)
		}
	}
	if !matched {
		return text, false
	}
	return text, true
}
