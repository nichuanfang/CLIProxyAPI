package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

// TestCodeBuddyUsageRecordsEffectiveReasoningEffort guards that both transports
// capture usage metadata after defaults and payload rules produced the body that
// is actually sent upstream. glm-5.1 requires an explicit effort, so a record
// without one would describe a request the executor never makes.
func TestCodeBuddyUsageRecordsEffectiveReasoningEffort(t *testing.T) {
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(t.Name(), "codebuddy", registry.GetCodeBuddyModels())
	t.Cleanup(func() { modelRegistry.UnregisterClient(t.Name()) })

	for _, tc := range []struct {
		name   string
		config config.PayloadConfig
		want   string
	}{
		{name: "default effort", want: "medium"},
		{
			name: "payload override stays final",
			config: config.PayloadConfig{Override: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{Name: "glm-5.1"}},
				Params: map[string]any{"reasoning_effort": "low"},
			}}},
			want: "low",
		},
	} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, streaming), func(t *testing.T) {
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
					_, _ = io.WriteString(w, "data: {\"id\":\"chat-usage\",\"model\":\"glm-5.1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\ndata: [DONE]\n\n")
				}))
				t.Cleanup(server.Close)

				alias := t.Name()
				capture := &codexResponseModelUsageCapture{alias: alias, records: make(chan coreusage.Record, 4)}
				coreusage.RegisterNamedPlugin(t.Name(), capture)
				t.Cleanup(func() {
					coreusage.RegisterNamedPlugin(t.Name(), codexResponseModelNoopUsagePlugin{})
				})

				payload := []byte(`{"model":"glm-5.1","input":"Reply OK."}`)
				req := cliproxyexecutor.Request{Model: "glm-5.1", Payload: payload}
				opts := cliproxyexecutor.Options{
					SourceFormat:    sdktranslator.FormatOpenAIResponse,
					OriginalRequest: payload,
					Stream:          streaming,
				}
				ctx := coreusage.WithRequestedModelAlias(context.Background(), alias)
				exec := NewCodeBuddyExecutor(&config.Config{Payload: tc.config})
				auth := &cliproxyauth.Auth{
					Attributes: map[string]string{"base_url": server.URL},
					Metadata:   map[string]any{"access_token": "test-token"},
				}

				if streaming {
					result, errStream := exec.ExecuteStream(ctx, auth, req, opts)
					if errStream != nil {
						t.Fatalf("ExecuteStream: %v", errStream)
					}
					for chunk := range result.Chunks {
						if chunk.Err != nil {
							t.Fatalf("stream chunk: %v", chunk.Err)
						}
					}
				} else if _, errExecute := exec.Execute(ctx, auth, req, opts); errExecute != nil {
					t.Fatalf("Execute: %v", errExecute)
				}

				if got := gjson.GetBytes(<-bodies, "reasoning_effort").String(); got != tc.want {
					t.Fatalf("upstream reasoning_effort = %q, want %q", got, tc.want)
				}
				record := capture.await(t)
				if record.Failed {
					t.Fatalf("usage record marked failed: %+v", record.Fail)
				}
				if record.ReasoningEffort != tc.want {
					t.Fatalf("usage ReasoningEffort = %q, want %q", record.ReasoningEffort, tc.want)
				}
			})
		}
	}
}

func TestCodeBuddyUsageRecordsConversionFailure(t *testing.T) {
	for _, hasUsage := range []bool{false, true} {
		t.Run(fmt.Sprintf("usage=%v", hasUsage), func(t *testing.T) {
			first, _ := applyPatchTestFrames("chat", "apply_patch")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, first+"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
				if hasUsage {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n")
				}
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			defer server.Close()
			alias := t.Name()
			capture := &codexResponseModelUsageCapture{alias: alias, records: make(chan coreusage.Record, 4)}
			coreusage.RegisterNamedPlugin(t.Name(), capture)
			t.Cleanup(func() { coreusage.RegisterNamedPlugin(t.Name(), codexResponseModelNoopUsagePlugin{}) })
			ctx := coreusage.WithRequestedModelAlias(context.Background(), alias)
			auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}}
			payload := applyPatchTestRequest()
			_, errExecute := NewCodeBuddyExecutor(&config.Config{}).Execute(ctx, auth,
				cliproxyexecutor.Request{Model: "glm-5.3", Payload: payload},
				cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, OriginalRequest: payload})
			var upstreamErr statusErr
			if !errors.As(errExecute, &upstreamErr) || upstreamErr.StatusCode() != http.StatusBadGateway {
				t.Fatalf("conversion error = %v, want 502", errExecute)
			}
			record := capture.await(t)
			if !record.Failed {
				t.Fatal("conversion failure was recorded as success")
			}
			if hasUsage && (record.Detail.InputTokens != 5 || record.Detail.OutputTokens != 2 || record.Detail.TotalTokens != 7) {
				t.Fatalf("failed attempt lost upstream usage: %+v", record.Detail)
			}
		})
	}
}
