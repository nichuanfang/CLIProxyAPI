package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyResponsesReasoningEffort(t *testing.T) {
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(t.Name(), "codebuddy", registry.GetCodeBuddyModels())
	t.Cleanup(func() { modelRegistry.UnregisterClient(t.Name()) })

	for _, modelCase := range []struct {
		model, alternateEffort string
		output                 int
	}{
		{"deepseek-v4.1-flash", "xhigh", 393216},
		{"glm-5.3", "max", 131072},
		{"glm-5.3-flash", "xhigh", 131072},
	} {
		model := modelCase.model
		for _, streaming := range []bool{false, true} {
			for _, tc := range []struct {
				name, effort, suffix, want string
				payload                    config.PayloadConfig
			}{
				{name: "low", effort: "low", want: "low"},
				{name: "medium compatibility", effort: "medium", want: "medium"},
				{name: "high", effort: "high", want: "high"},
				{name: "alternate supported level", effort: modelCase.alternateEffort, want: modelCase.alternateEffort},
				{name: "max", effort: "max", want: "max"},
				{name: "disabled", effort: "none", want: "none"},
				{name: "suffix takes precedence", effort: "high", suffix: "(" + modelCase.alternateEffort + ")", want: modelCase.alternateEffort},
				{
					name: "payload override remains final", effort: modelCase.alternateEffort, want: "low",
					payload: config.PayloadConfig{Override: []config.PayloadRule{{
						Models: []config.PayloadModelRule{{Name: model}},
						Params: map[string]any{"reasoning_effort": "low"},
					}}},
				},
				{
					name: "payload filter remains final", effort: modelCase.alternateEffort,
					payload: config.PayloadConfig{Filter: []config.PayloadFilterRule{{
						Models: []config.PayloadModelRule{{Name: model}},
						Params: []string{"reasoning_effort"},
					}}},
				},
			} {
				t.Run(fmt.Sprintf("%s/stream=%t/%s", model, streaming, tc.name), func(t *testing.T) {
					bodies := make(chan []byte, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, errRead := io.ReadAll(r.Body)
						if errRead != nil {
							t.Error(errRead)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						bodies <- body
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"id\":\"chat-effort\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					}))
					defer server.Close()
					exec := NewCodeBuddyExecutor(&config.Config{Payload: tc.payload})
					auth := &cliproxyauth.Auth{
						Attributes: map[string]string{"base_url": server.URL},
						Metadata:   map[string]any{"access_token": "test-token"},
					}
					payload := []byte(fmt.Sprintf(`{"model":%q,"input":"Reply OK.","reasoning":{"effort":%q},"max_output_tokens":%d}`, model, tc.effort, modelCase.output))
					req := cliproxyexecutor.Request{Model: model + tc.suffix, Payload: payload}
					opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, OriginalRequest: payload, Stream: streaming}
					if streaming {
						result, errStream := exec.ExecuteStream(context.Background(), auth, req, opts)
						if errStream != nil {
							t.Fatal(errStream)
						}
						for chunk := range result.Chunks {
							if chunk.Err != nil {
								t.Fatal(chunk.Err)
							}
						}
					} else if _, errExecute := exec.Execute(context.Background(), auth, req, opts); errExecute != nil {
						t.Fatal(errExecute)
					}
					body := <-bodies
					effort := gjson.GetBytes(body, "reasoning_effort")
					if effort.String() != tc.want || (tc.want == "" && effort.Exists()) {
						t.Fatalf("upstream reasoning_effort = %s, want %q: %s", effort.Raw, tc.want, body)
					}
					if got := gjson.GetBytes(body, "model").String(); got != model {
						t.Fatalf("upstream model = %q, want %q", got, model)
					}
					if got := gjson.GetBytes(body, "max_tokens").Int(); got != int64(modelCase.output) {
						t.Fatalf("upstream max_tokens = %d, want %d: %s", got, modelCase.output, body)
					}
					if gjson.GetBytes(body, "reasoning").Exists() || gjson.GetBytes(body, "max_output_tokens").Exists() {
						t.Fatalf("Responses-only parameters leaked upstream: %s", body)
					}
				})
			}
		}
	}
}
