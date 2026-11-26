package helps

import (
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestCodeBuddyClaudeCodeGuardRules(t *testing.T) {
	const identity = "You are Claude Code, Anthropic's official CLI for Claude."
	tests := []struct {
		name        string
		payload     string
		wantSystem  string
		wantChanged bool
	}{
		{
			name:        "claude code identity block is dropped",
			payload:     `{"messages":[{"role":"system","content":[{"type":"text","text":"` + identity + `"},{"type":"text","text":"Follow project rules."}]},{"role":"user","content":"hi"}]}`,
			wantSystem:  "Follow project rules.",
			wantChanged: true,
		},
		{
			name:        "billing header block is dropped",
			payload:     `{"messages":[{"role":"system","content":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.321;"},{"type":"text","text":"Follow project rules."}]},{"role":"user","content":"hi"}]}`,
			wantSystem:  "Follow project rules.",
			wantChanged: true,
		},
		{
			name:        "git main branch fingerprint is rewritten",
			payload:     `{"messages":[{"role":"system","content":[{"type":"text","text":"Main branch (you will usually use this for PRs): master\n\nFollow project rules."}]},{"role":"user","content":"hi"}]}`,
			wantSystem:  "Main branch: master\n\nFollow project rules.",
			wantChanged: true,
		},
		{
			name:        "git fingerprint matching is case insensitive",
			payload:     `{"messages":[{"role":"system","content":[{"type":"text","text":"MAIN BRANCH (YOU WILL USUALLY USE THIS FOR PRS): main"}]},{"role":"user","content":"hi"}]}`,
			wantSystem:  "Main branch: main",
			wantChanged: true,
		},
		{
			name:        "embedded identity in project instructions is preserved",
			payload:     `{"messages":[{"role":"system","content":"Keep this example: ` + identity + `"},{"role":"user","content":"hi"}]}`,
			wantSystem:  "Keep this example: " + identity,
			wantChanged: false,
		},
		{
			name:        "user content is not touched",
			payload:     `{"messages":[{"role":"system","content":"Follow project rules."},{"role":"user","content":"Main branch (you will usually use this for PRs): master"}]}`,
			wantSystem:  "Follow project rules.",
			wantChanged: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeCodeBuddyClaudeCodePayload([]byte(tt.payload))
			changed := string(got) != tt.payload
			if changed != tt.wantChanged {
				t.Fatalf("changed = %t, want %t: %s", changed, tt.wantChanged, got)
			}
			system := ojson.GetBytes(got, `messages.#(role=="system")`)
			content := system.Get("content")
			if content.IsArray() {
				content = content.Get("0.text")
			}
			if content.String() != tt.wantSystem {
				t.Fatalf("system = %q, want %q: %s", content.String(), tt.wantSystem, got)
			}
			user := ojson.GetBytes(got, `messages.#(role=="user")`)
			if tt.name == "user content is not touched" && !strings.Contains(user.Get("content").String(), "you will usually use this for PRs") {
				t.Fatalf("user content was modified: %s", got)
			}
		})
	}
}

func TestCodeBuddyClaudeCodeGuardRuleTable(t *testing.T) {
	seen := make(map[string]struct{}, len(codeBuddyClaudeCodeRules))
	for _, rule := range codeBuddyClaudeCodeRules {
		if strings.TrimSpace(rule.name) == "" {
			t.Fatal("rule name must not be empty")
		}
		if _, ok := seen[rule.name]; ok {
			t.Fatalf("duplicate rule name %q", rule.name)
		}
		seen[rule.name] = struct{}{}
		if rule.pattern == nil {
			t.Fatalf("rule %q has a nil pattern", rule.name)
		}
		switch rule.action {
		case codeBuddyClaudeCodeReplace:
			if rule.replacement == "" {
				t.Fatalf("replace rule %q has an empty replacement", rule.name)
			}
		case codeBuddyClaudeCodeDrop:
			if rule.replacement != "" {
				t.Fatalf("drop rule %q has a replacement", rule.name)
			}
		default:
			t.Fatalf("rule %q has an unknown action %d", rule.name, rule.action)
		}
	}
}

func TestCodeBuddyClaudeCodeGuardPreservesRawWhenUnchanged(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"system","content":"Follow project rules."},{"role":"user","content":"hi"}]}`)
	got := SanitizeCodeBuddyClaudeCodePayload(payload)
	if string(got) != string(payload) {
		t.Fatalf("unchanged payload was reserialized: %q", got)
	}
}
