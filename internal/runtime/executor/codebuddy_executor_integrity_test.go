package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyStreamIntegrity(t *testing.T) {
	const partial = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n"
	const finish = "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"
	const done = "data: [DONE]\n\n"
	for _, streaming := range []bool{false, true} {
		for _, tc := range []struct {
			name      string
			body      string
			wantError bool
			truncated bool
			status    int
			wantUsage bool
		}{
			{name: "normal", body: partial + finish + done},
			{name: "done without finish reason", body: partial + done},
			{name: "finish reason without done", body: partial + finish},
			{name: "no final blank line", body: partial + "data: [DONE]"},
			{name: "multiline CRLF with comments", body: ": keepalive\r\nevent: message\r\ndata: {\"choices\":\r\ndata: [{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\r\n\r\n" + done},
			{name: "multiline late usage", body: partial + finish + "data: {\"choices\":[],\"usage\":\ndata: {\"prompt_tokens\":10,\"completion_tokens\":4,\"total_tokens\":14,\"prompt_tokens_details\":{\"cached_tokens\":6},\"completion_tokens_details\":{\"reasoning_tokens\":2}}}\n\n" + done, wantUsage: true},
			{name: "error data", body: "data: {\"error\":{\"message\":\"rejected\",\"status\":400}}\n\n" + done, wantError: true, status: 400},
			{name: "error event", body: "event: error\ndata: {\"message\":\"rejected\",\"status\":429}\n\n", wantError: true, status: 429},
			{name: "empty error event", body: "event: error\n\n", wantError: true, status: 502},
			{name: "invalid error event", body: "event: error\ndata: [DONE]\n\n", wantError: true, status: 502},
			{name: "late error after finish", body: partial + finish + "data: {\"error\":{\"message\":\"rejected\",\"status\":429}}\n\n", wantError: true, status: 429},
			{name: "bare error JSON", body: `{"error":{"message":"rejected","status":400}}`, wantError: true, status: 400},
			{name: "truncated text", body: partial, wantError: true, truncated: true, status: 502},
			{name: "malformed frame", body: partial + "data: {\"choices\":[\n\n", wantError: true, truncated: true, status: 502},
			{name: "empty stream", wantError: true, truncated: true, status: 502},
			{name: "done only", body: done, wantError: true, truncated: true, status: 502},
			{name: "unframed done", body: partial + "[DONE]\n", wantError: true, truncated: true, status: 502},
			{name: "usage only", body: "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3}}\n\n" + done, wantError: true, truncated: true, status: 502},
			{name: "unfinished frame followed by done", body: "data: {\"choices\":[\ndata: [DONE]\n\n", wantError: true, truncated: true, status: 502},
		} {
			t.Run(fmt.Sprintf("stream=%v/%s", streaming, tc.name), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, tc.body)
				}))
				defer server.Close()
				executor := NewCodeBuddyExecutor(&config.Config{})
				auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}}
				payload := []byte(`{"model":"glm-5.3","input":"hello","stream":true}`)
				req := cliproxyexecutor.Request{Model: "glm-5.3", Payload: payload}
				opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, OriginalRequest: payload, Stream: streaming}
				var body []byte
				var errResult error
				if streaming {
					var result *cliproxyexecutor.StreamResult
					result, errResult = executor.ExecuteStream(context.Background(), auth, req, opts)
					if result != nil {
						for chunk := range result.Chunks {
							body = append(body, chunk.Payload...)
							if chunk.Err != nil {
								errResult = chunk.Err
								break
							}
						}
					}
				} else {
					var result cliproxyexecutor.Response
					result, errResult = executor.Execute(context.Background(), auth, req, opts)
					body = result.Payload
				}
				if (errResult != nil) != tc.wantError {
					t.Fatalf("error = %v, wantError = %v; body=%s", errResult, tc.wantError, body)
				}
				if tc.wantError {
					var status interface{ StatusCode() int }
					if !errors.As(errResult, &status) || status.StatusCode() != tc.status {
						t.Errorf("error status = %v, want %d", errResult, tc.status)
					}
					var truncated cliproxyexecutor.StreamTruncatedError
					if got := errors.As(errResult, &truncated) && truncated.IsStreamTruncated(); got != tc.truncated {
						t.Errorf("truncated = %v, want %v: %v", got, tc.truncated, errResult)
					}
					if gjson.GetBytes(body, "status").String() == "completed" || bytes.Contains(body, []byte(`"type":"response.completed"`)) {
						t.Errorf("failed response was completed: %s", body)
					}
				} else if !bytes.Contains(body, []byte("hello")) || !bytes.Contains(body, []byte(`"status":"completed"`)) {
					t.Errorf("missing successful response: %s", body)
				}
				if tc.wantUsage {
					response := gjson.ParseBytes(body)
					if streaming {
						for _, frame := range applyPatchTestPayloads(body) {
							if gjson.GetBytes(frame, "type").String() == "response.completed" {
								response = gjson.GetBytes(frame, "response")
							}
						}
					}
					for path, want := range map[string]int64{"input_tokens": 10, "output_tokens": 4, "total_tokens": 14, "input_tokens_details.cached_tokens": 6, "output_tokens_details.reasoning_tokens": 2} {
						if got := response.Get("usage." + path).Int(); got != want {
							t.Errorf("usage.%s = %d, want %d; body=%s", path, got, want, body)
						}
					}
				}
			})
		}
	}
}

func TestCodeBuddyAggregateSparseInterleavedTools(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":9,\"id\":\"call-b\",\"function\":{\"name\":\"second\",\"arguments\":\"{\\\"b\\\":\"}},{\"index\":3,\"id\":\"call-a\",\"function\":{\"name\":\"first\",\"arguments\":\"{\\\"a\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":3,\"function\":{\"arguments\":\"1}\"}},{\"index\":9,\"function\":{\"arguments\":\"2}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"
	body, _, _, errAggregate := aggregateCodeBuddyStream(strings.NewReader(stream))
	if errAggregate != nil {
		t.Fatal(errAggregate)
	}
	tools := gjson.GetBytes(body, "choices.0.message.tool_calls").Array()
	if len(tools) != 2 || tools[0].Get("id").String() != "call-a" || tools[1].Get("id").String() != "call-b" || tools[0].Get("function.arguments").String() != `{"a":1}` || tools[1].Get("function.arguments").String() != `{"b":2}` {
		t.Fatalf("tool calls lost or interleaved: %s", body)
	}
}

func TestCodeBuddyApplyPatchIntegrity(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, complete := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/complete=%v", streaming, complete), func(t *testing.T) {
				first, last := applyPatchTestFrames("chat", "apply_patch")
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, first)
					if complete {
						_, _ = io.WriteString(w, last)
					}
				}))
				defer server.Close()
				executor := NewCodeBuddyExecutor(&config.Config{})
				auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}}
				payload := applyPatchTestRequest()
				req := cliproxyexecutor.Request{Model: "glm-5.3", Payload: payload}
				opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, OriginalRequest: payload, Stream: streaming}
				var body []byte
				var errResult error
				if streaming {
					var result *cliproxyexecutor.StreamResult
					result, errResult = executor.ExecuteStream(context.Background(), auth, req, opts)
					if result != nil {
						for chunk := range result.Chunks {
							body = append(body, chunk.Payload...)
							if chunk.Err != nil {
								errResult = chunk.Err
							}
						}
					}
				} else {
					var result cliproxyexecutor.Response
					result, errResult = executor.Execute(context.Background(), auth, req, opts)
					body = result.Payload
				}
				if !complete {
					if errResult == nil || bytes.Contains(body, []byte(`"status":"completed"`)) || bytes.Contains(body, []byte(`"type":"response.custom_tool_call_input.done"`)) {
						t.Fatalf("unfinished patch was accepted: error=%v, body=%s", errResult, body)
					}
					return
				}
				if errResult != nil {
					t.Fatal(errResult)
				}
				var output gjson.Result
				for _, frame := range applyPatchTestPayloads(body) {
					if item := gjson.GetBytes(frame, "response.output.0"); item.Exists() {
						output = item
					} else if item := gjson.GetBytes(frame, "output.0"); item.Exists() {
						output = item
					}
				}
				if output.Get("type").String() != "custom_tool_call" || output.Get("name").String() != "apply_patch" || output.Get("input").String() != applyPatchTestInput {
					t.Fatalf("patch bridge changed: %s", body)
				}
			})
		}
	}
}
