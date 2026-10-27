package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyCompactionIntegrity(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, finish := range []string{"stop", "length", "content_filter", ""} {
			t.Run(fmt.Sprintf("stream=%v/finish=%s", streaming, finish), func(t *testing.T) {
				var upstreamBody []byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					upstreamBody, _ = io.ReadAll(r.Body)
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Summary of prior work.\"},\"finish_reason\":%q}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120,\"prompt_tokens_details\":{\"cached_tokens\":60},\"completion_tokens_details\":{\"reasoning_tokens\":7}}}\n\n", finish)
					if finish != "" {
						_, _ = io.WriteString(w, "data: [DONE]\n\n")
					}
				}))
				defer server.Close()
				executor := NewCodeBuddyExecutor(&config.Config{})
				auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}}
				payload := []byte(`{"model":"glm-5.3","input":"summarize","reasoning":{"effort":"low"}}`)
				req := cliproxyexecutor.Request{Model: "glm-5.3(high)", Payload: payload}
				opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, OriginalRequest: payload, Alt: "responses/compact", Stream: streaming}
				var body []byte
				var errResult error
				if streaming {
					var result *cliproxyexecutor.StreamResult
					result, errResult = executor.ExecuteStream(context.Background(), auth, req, opts)
					if result != nil {
						for chunk := range result.Chunks {
							if chunk.Err != nil {
								errResult = chunk.Err
							}
							frame, errFrame := helps.NewCodeBuddySSEReader(strings.NewReader(string(chunk.Payload))).ReadFrame()
							if errFrame != nil {
								t.Fatal(errFrame)
							}
							if gjson.GetBytes(frame.Data, "type").String() == "response.completed" {
								body = []byte(gjson.GetBytes(frame.Data, "response").Raw)
							}
						}
					}
				} else {
					var result cliproxyexecutor.Response
					result, errResult = executor.Execute(context.Background(), auth, req, opts)
					body = result.Payload
				}
				if finish != "stop" {
					if errResult == nil || len(body) != 0 {
						t.Fatalf("unfinished summary sealed: error=%v, body=%s", errResult, body)
					}
					return
				}
				if errResult != nil {
					t.Fatal(errResult)
				}
				if got := gjson.GetBytes(upstreamBody, "reasoning_effort").String(); got != "high" {
					t.Errorf("summary lost model suffix: %s", upstreamBody)
				}
				if !gjson.GetBytes(upstreamBody, "stream_options.include_usage").Bool() {
					t.Errorf("summary did not request usage: %s", upstreamBody)
				}
				for path, want := range map[string]int64{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120, "input_tokens_details.cached_tokens": 60, "output_tokens_details.reasoning_tokens": 7} {
					if got := gjson.GetBytes(body, "usage."+path).Int(); got != want {
						t.Errorf("usage.%s = %d, want %d; body=%s", path, got, want, body)
					}
				}
				text, errUnseal := helps.UnsealAntigravityCompaction(gjson.GetBytes(body, "output.0.encrypted_content").String())
				if errUnseal != nil || !strings.Contains(text, "prior work") {
					t.Errorf("invalid capsule: text=%q, error=%v", text, errUnseal)
				}
			})
		}
	}
}

func TestCodeBuddyPayloadRulesAreFinal(t *testing.T) {
	models := []config.PayloadModelRule{{Name: "*"}}
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{
			{Models: []config.PayloadModelRule{{Name: "*", Exist: []string{"barrier_once"}}}, Params: map[string]any{"applied_twice": true}},
			{Models: models, Params: map[string]any{"barrier_once": true, "stream": false, "tool_choice": "required"}},
		},
		Filter: []config.PayloadFilterRule{{Models: models, Params: []string{"stream_options", "reasoning_effort", "prompt_cache_key"}}},
	}}
	for _, compact := range []bool{false, true} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("compact=%v/stream=%v", compact, streaming), func(t *testing.T) {
				var upstreamBody []byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					upstreamBody, _ = io.ReadAll(r.Body)
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"summary\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}))
				defer server.Close()
				executor := NewCodeBuddyExecutor(cfg)
				auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}}
				payload := []byte(`{"model":"glm-5.3","input":"hello","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"prompt_cache_key":"test-cache"}`)
				req := cliproxyexecutor.Request{Model: "glm-5.3(high)", Payload: payload}
				opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, OriginalRequest: payload, Stream: streaming}
				if compact {
					opts.Alt = "responses/compact"
				}
				if streaming {
					result, errStream := executor.ExecuteStream(context.Background(), auth, req, opts)
					if errStream != nil {
						t.Fatal(errStream)
					}
					for chunk := range result.Chunks {
						if chunk.Err != nil {
							t.Fatal(chunk.Err)
						}
					}
				} else if _, errExecute := executor.Execute(context.Background(), auth, req, opts); errExecute != nil {
					t.Fatal(errExecute)
				}
				if !gjson.GetBytes(upstreamBody, "barrier_once").Bool() || gjson.GetBytes(upstreamBody, "applied_twice").Exists() || gjson.GetBytes(upstreamBody, "stream").Bool() || gjson.GetBytes(upstreamBody, "tool_choice").String() != "required" {
					t.Errorf("payload rules overwritten or evaluated twice: %s", upstreamBody)
				}
				for _, path := range []string{"stream_options", "reasoning_effort", "prompt_cache_key"} {
					if gjson.GetBytes(upstreamBody, path).Exists() {
						t.Errorf("filtered field %s restored: %s", path, upstreamBody)
					}
				}
			})
		}
	}
}

func TestCodeBuddyCountTokensUsesFinalPayloadOnce(t *testing.T) {
	baseline := []byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"hello"}]}`)
	withTools := []byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","description":"A tool which must be filtered before token counting","parameters":{"type":"object"}}}]}`)
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI}
	want, errBaseline := NewCodeBuddyExecutor(&config.Config{}).CountTokens(context.Background(), nil, cliproxyexecutor.Request{Model: "glm-5.3", Payload: baseline}, opts)
	if errBaseline != nil {
		t.Fatal(errBaseline)
	}
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{
			{Models: []config.PayloadModelRule{{Name: "*", Exist: []string{"barrier_once"}}}, Params: map[string]any{"messages.0.content": strings.Repeat("should not be counted twice ", 100)}},
			{Models: []config.PayloadModelRule{{Name: "*"}}, Params: map[string]any{"barrier_once": true}},
		},
		Filter: []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: []string{"tools"}}},
	}}
	got, errCount := NewCodeBuddyExecutor(cfg).CountTokens(context.Background(), nil, cliproxyexecutor.Request{Model: "glm-5.3", Payload: withTools}, opts)
	if errCount != nil || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("final token count = %s, error=%v, want %s", got.Payload, errCount, want.Payload)
	}
}
