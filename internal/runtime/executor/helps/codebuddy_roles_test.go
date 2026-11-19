package helps

import (
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestNormalizeCodeBuddyDeveloperRole(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		wantRoles   []string
		wantChanged bool
	}{
		{
			name:      "developer folded into system",
			payload:   `{"model":"m","messages":[{"role":"developer","content":"a"},{"role":"user","content":"b"}]}`,
			wantRoles: []string{"system", "user"},
		},
		{
			name:        "no developer leaves payload untouched",
			payload:     `{"model":"m","messages":[{"role":"system","content":"a"},{"role":"user","content":"b"}]}`,
			wantRoles:   []string{"system", "user"},
			wantChanged: false,
		},
		{
			name:      "multiple developer messages and other roles preserved",
			payload:   `{"model":"m","messages":[{"role":"developer","content":[{"type":"text","text":"a"}]},{"role":"user","content":"b"},{"role":"assistant","content":"c"},{"role":"developer","content":"d"},{"role":"tool","tool_call_id":"1","content":"e"}]}`,
			wantRoles: []string{"system", "user", "assistant", "system", "tool"},
		},
		{
			name:      "content is not modified",
			payload:   `{"model":"m","messages":[{"role":"developer","content":[{"type":"text","text":"keep me"}]}]}`,
			wantRoles: []string{"system"},
		},
		{
			name:      "missing messages array is a no-op",
			payload:   `{"model":"m"}`,
			wantRoles: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeCodeBuddyDeveloperRole([]byte(tt.payload))
			messages := ojson.GetBytes(got, "messages")
			if tt.wantRoles == nil {
				if messages.Exists() {
					t.Fatalf("messages should stay absent: %s", got)
				}
				return
			}
			gotRoles := make([]string, 0, len(messages.Array()))
			for _, message := range messages.Array() {
				gotRoles = append(gotRoles, message.Get("role").String())
			}
			if len(gotRoles) != len(tt.wantRoles) {
				t.Fatalf("roles = %v, want %v", gotRoles, tt.wantRoles)
			}
			for i := range tt.wantRoles {
				if gotRoles[i] != tt.wantRoles[i] {
					t.Fatalf("roles = %v, want %v", gotRoles, tt.wantRoles)
				}
			}
			if tt.name == "content is not modified" {
				if text := ojson.GetBytes(got, "messages.0.content.0.text").String(); text != "keep me" {
					t.Fatalf("content changed: %s", got)
				}
			}
		})
	}
}

func TestNormalizeCodeBuddyDeveloperRolePreservesRawWhenUnchanged(t *testing.T) {
	payload := []byte(`{"model":"m",  "messages":[{"role":"system","content":"a"}]}`)
	got := NormalizeCodeBuddyDeveloperRole(payload)
	if string(got) != string(payload) {
		t.Fatalf("unchanged payload was reserialized: %q", got)
	}
}
