package helps

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/tidwall/gjson"
)

func TestEnsureCodeBuddyReasoningEffort(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		model   string
		want    string
	}{
		{
			name:    "adds default for model requiring effort",
			payload: `{"model":"glm-5.1","messages":[]}`,
			model:   "glm-5.1",
			want:    "medium",
		},
		{
			name:    "adds default for turbo model requiring effort",
			payload: `{"model":"glm-5.0-turbo"}`,
			model:   "glm-5.0-turbo",
			want:    "medium",
		},
		{
			name:    "replaces explicit none",
			payload: `{"model":"glm-5.1","reasoning_effort":"none"}`,
			model:   "glm-5.1",
			want:    "medium",
		},
		{
			name:    "keeps explicit level",
			payload: `{"model":"glm-5.1","reasoning_effort":"max"}`,
			model:   "glm-5.1",
			want:    "max",
		},
		{
			name:    "leaves other models untouched",
			payload: `{"model":"glm-5.3"}`,
			model:   "glm-5.3",
			want:    "",
		},
		{
			name:    "leaves invalid payload untouched",
			payload: `not json`,
			model:   "glm-5.1",
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EnsureCodeBuddyReasoningEffort([]byte(tc.payload), tc.model)
			effort := gjson.GetBytes(got, "reasoning_effort").String()
			if effort != tc.want {
				t.Fatalf("reasoning_effort = %q, want %q (payload=%s)", effort, tc.want, got)
			}
		})
	}
}

// TestEnsureCodeBuddyReasoningEffortFollowsCatalog guards against the required
// model list drifting from the catalog: a CodeBuddy model that cannot disable
// thinking needs the default even when it was added by a remote refresh.
func TestEnsureCodeBuddyReasoningEffortFollowsCatalog(t *testing.T) {
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(t.Name(), "codebuddy", []*registry.ModelInfo{
		{
			ID:   "future-coded-model",
			Type: "codebuddy",
			Thinking: &registry.ThinkingSupport{
				Levels: []string{"low", "medium", "high"},
			},
		},
		{
			ID:   "future-optional-model",
			Type: "codebuddy",
			Thinking: &registry.ThinkingSupport{
				Levels:      []string{"low", "medium", "high", "none"},
				ZeroAllowed: true,
			},
		},
	})
	t.Cleanup(func() { modelRegistry.UnregisterClient(t.Name()) })

	for _, tc := range []struct {
		model string
		want  string
	}{
		{model: "future-coded-model", want: codeBuddyDefaultReasoningEffort},
		{model: "future-optional-model", want: ""},
		{model: "unregistered-model", want: ""},
	} {
		t.Run(tc.model, func(t *testing.T) {
			got := EnsureCodeBuddyReasoningEffort([]byte(`{"model":"`+tc.model+`"}`), tc.model)
			if effort := gjson.GetBytes(got, "reasoning_effort").String(); effort != tc.want {
				t.Fatalf("reasoning_effort = %q, want %q (payload=%s)", effort, tc.want, got)
			}
		})
	}
}
