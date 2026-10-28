package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestCodeBuddyClaudeThinkingScopeAndPayloadRules(t *testing.T) {
	for _, tc := range []struct {
		name, model, reasoning string
		format                 sdktranslator.Format
		payload                string
		cfg                    *config.Config
	}{
		{name: "deepseek", model: "deepseek-v4.1-flash", reasoning: "Read the file.", format: sdktranslator.FormatClaude},
		{name: "suffix", model: "deepseek-v4.1-flash(high)", reasoning: "Read the file.", format: sdktranslator.FormatClaude},
		{name: "other model unchanged", model: "glm-5.3", format: sdktranslator.FormatClaude},
		{name: "no invented thinking", model: "deepseek-v4.1-flash", format: sdktranslator.FormatClaude, payload: `{"model":"deepseek-v4.1-flash","max_tokens":64,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"read-1","name":"Read","input":{"path":"a.txt"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"read-1","content":"one"}]}]}`},
		{name: "codex source unchanged", model: "deepseek-v4.1-flash", format: sdktranslator.FormatOpenAIResponse, payload: `{"model":"deepseek-v4.1-flash","input":[{"role":"user","content":"hello"}]}`},
		{name: "override remains final", model: "deepseek-v4.1-flash", reasoning: "configured reasoning", format: sdktranslator.FormatClaude, cfg: &config.Config{Payload: config.PayloadConfig{Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: map[string]any{"messages.0.reasoning_content": "configured reasoning"}}}}}},
		{name: "filter remains final", model: "deepseek-v4.1-flash", format: sdktranslator.FormatClaude, cfg: &config.Config{Payload: config.PayloadConfig{Filter: []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: []string{"messages.0.reasoning_content"}}}}}},
	} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, streaming), func(t *testing.T) {
				bodies := make(chan []byte, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					bodies <- body
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}))
				defer server.Close()
				payload := tc.payload
				if payload == "" {
					payload = `{"model":"deepseek-v4.1-flash","max_tokens":64,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"Read the file."},{"type":"tool_use","id":"read-1","name":"Read","input":{"path":"a.txt"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"read-1","content":"one"}]}]}`
				}
				cfg := tc.cfg
				if cfg == nil {
					cfg = &config.Config{}
				}
				exec := NewCodeBuddyExecutor(cfg)
				auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}}
				req := cliproxyexecutor.Request{Model: tc.model, Payload: []byte(payload)}
				req.Payload, _ = sjson.SetBytes(req.Payload, "stream", streaming)
				opts := cliproxyexecutor.Options{SourceFormat: tc.format, OriginalRequest: req.Payload, Stream: streaming}
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
				reasoning := gjson.GetBytes(body, `messages.#(role=="assistant").reasoning_content`)
				if reasoning.String() != tc.reasoning || (tc.reasoning == "" && reasoning.Exists()) {
					t.Fatalf("reasoning_content = %s, want %q: %s", reasoning.Raw, tc.reasoning, body)
				}
				if bytes.Contains(body, []byte(`"signature"`)) {
					t.Fatalf("signature leaked upstream: %s", body)
				}
			})
		}
	}
}

func TestCodeBuddyClaudeThinkingToolRoundTrip(t *testing.T) {
	frames := [][]string{
		{
			`{"choices":[{"index":0,"delta":{"reasoning_content":"Read both inputs."}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"read-a","type":"function","function":{"name":"Read","arguments":"{\"path\":"}},{"index":1,"id":"read-b","type":"function","function":{"name":"Read","arguments":"{\"path\":"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"b.txt\"}"}},{"index":0,"function":{"arguments":"\"a.txt\"}"}}]},"finish_reason":"tool_calls"}]}`,
		},
		{
			`{"choices":[{"index":0,"delta":{"reasoning_content":"Check their sum."}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"run-test","type":"function","function":{"name":"Bash","arguments":"{\"command\":"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"echo 3\"}"}}]},"finish_reason":"tool_calls"}]}`,
		},
		{
			`{"choices":[{"index":0,"delta":{"reasoning_content":"Results agree.","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":20}}}`,
		},
	}
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", streaming), func(t *testing.T) {
			var turn atomic.Int32
			bodies := make(chan []byte, len(frames))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				bodies <- body
				index := int(turn.Add(1)) - 1
				if index >= len(frames) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, frame := range frames[index] {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
				}
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			defer server.Close()
			exec := NewCodeBuddyExecutor(&config.Config{})
			auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}}
			payload := []byte(`{"model":"deepseek-v4.1-flash","max_tokens":128,"messages":[{"role":"user","content":"Read a.txt and b.txt, then check their sum."}],"tools":[{"name":"Read","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}},{"name":"Bash","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}}]}`)
			for index := range frames {
				payload, _ = sjson.SetBytes(payload, "stream", streaming)
				req := cliproxyexecutor.Request{Model: "deepseek-v4.1-flash", Payload: payload}
				response := codeBuddyClaudeThinkingTestTurn(t, exec, auth, req, streaming)
				body := <-bodies
				assistants := gjson.GetBytes(body, `messages.#(role=="assistant")#`).Array()
				for historyIndex, assistant := range assistants {
					want := []string{"Read both inputs.", "Check their sum."}[historyIndex]
					if assistant.Get("reasoning_content").String() != want {
						t.Fatalf("turn %d lost assistant reasoning: %s", index, body)
					}
				}
				if len(assistants) != index {
					t.Fatalf("turn %d has %d assistant messages: %s", index, len(assistants), body)
				}
				if index > 0 {
					if gjson.GetBytes(body, "messages.2.tool_call_id").String() != "read-a" || gjson.GetBytes(body, "messages.3.tool_call_id").String() != "read-b" {
						t.Fatalf("parallel tool results lost alignment: %s", body)
					}
				}
				if index == 2 {
					if response.Get("stop_reason").String() != "end_turn" || response.Get(`content.#(type=="text").text`).String() != "done" || gjson.GetBytes(body, "messages.5.tool_call_id").String() != "run-test" {
						t.Fatalf("conversation did not finish after the serial tool result: %s; %s", response.Raw, body)
					}
					if response.Get("usage.cache_read_input_tokens").Int() != 20 {
						t.Fatalf("cache usage lost: %s", response.Raw)
					}
					continue
				}
				if response.Get("stop_reason").String() != "tool_use" {
					t.Fatalf("turn %d did not request tools: %s", index, response.Raw)
				}
				payload, _ = sjson.SetRawBytes(payload, "messages.-1", []byte(`{"role":"assistant","content":`+response.Get("content").Raw+`}`))
				var results []map[string]any
				for _, tool := range response.Get(`content.#(type=="tool_use")#`).Array() {
					wantInput := map[string]string{"read-a": "a.txt", "read-b": "b.txt", "run-test": "echo 3"}[tool.Get("id").String()]
					path := "input.path"
					if index == 1 {
						path = "input.command"
					}
					if wantInput == "" || tool.Get(path).String() != wantInput {
						t.Fatalf("fragmented arguments were corrupted: %s", tool.Raw)
					}
					results = append(results, map[string]any{"type": "tool_result", "tool_use_id": tool.Get("id").String(), "content": "3"})
				}
				wantTools := 2 - index
				if len(results) != wantTools {
					t.Fatalf("turn %d returned %d tools, want %d", index, len(results), wantTools)
				}
				resultJSON, _ := json.Marshal(map[string]any{"role": "user", "content": results})
				payload, _ = sjson.SetRawBytes(payload, "messages.-1", resultJSON)
			}
		})
	}
}

func codeBuddyClaudeThinkingTestTurn(t *testing.T, exec *CodeBuddyExecutor, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, streaming bool) gjson.Result {
	t.Helper()
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude, OriginalRequest: req.Payload, Stream: streaming}
	if !streaming {
		response, errExecute := exec.Execute(context.Background(), auth, req, opts)
		if errExecute != nil {
			t.Fatal(errExecute)
		}
		return gjson.ParseBytes(response.Payload)
	}
	result, errStream := exec.ExecuteStream(context.Background(), auth, req, opts)
	if errStream != nil {
		t.Fatal(errStream)
	}
	accumulator := newKimiThinkingReplayStreamAccumulator()
	response := []byte(`{}`)
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		accumulator.observe(chunk.Payload)
		for _, line := range strings.Split(string(chunk.Payload), "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			event := gjson.Parse(strings.TrimPrefix(line, "data: "))
			if event.Get("type").String() == "message_delta" {
				response, _ = sjson.SetBytes(response, "stop_reason", event.Get("delta.stop_reason").String())
				if usage := event.Get("usage"); usage.Exists() {
					response, _ = sjson.SetRawBytes(response, "usage", []byte(usage.Raw))
				}
			}
		}
	}
	content, complete := accumulator.content()
	if !complete {
		t.Fatal("Claude stream did not complete its content blocks")
	}
	response, _ = sjson.SetRawBytes(response, "content", content)
	return gjson.ParseBytes(response)
}

func TestCodeBuddyClaudeThinkingCountTokens(t *testing.T) {
	const original = `{"model":"deepseek-v4.1-flash","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason"},{"type":"text","text":"Ready."}]},{"role":"user","content":"continue"}]}`
	for _, model := range []string{"deepseek-v4.1-flash", "deepseek-v4.1-flash(high)"} {
		t.Run(model, func(t *testing.T) {
			cfg := &config.Config{Payload: config.PayloadConfig{Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "*", Exist: []string{"messages.0.reasoning_content"}}}, Params: map[string]any{"messages.1.content": "thinking was preserved"}}}}}
			req := cliproxyexecutor.Request{Model: model, Payload: []byte(original)}
			response, errCount := NewCodeBuddyExecutor(cfg).CountTokens(context.Background(), nil, req, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude, OriginalRequest: req.Payload})
			if errCount != nil {
				t.Fatal(errCount)
			}
			enc, errTokenizer := helps.TokenizerForModel("deepseek-v4.1-flash")
			if errTokenizer != nil {
				t.Fatal(errTokenizer)
			}
			want, errExpected := helps.CountOpenAIChatTokens(enc, []byte(`{"messages":[{"role":"assistant","content":[{"type":"text","text":"Ready."}],"reasoning_content":"reason"},{"role":"user","content":"thinking was preserved"}]}`))
			if errExpected != nil {
				t.Fatal(errExpected)
			}
			if got := gjson.GetBytes(response.Payload, "input_tokens").Int(); got != want {
				t.Fatalf("token-count payload did not preserve thinking before user rules: got %d, want %d", got, want)
			}
		})
	}
}
