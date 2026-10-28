package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyExecutorClaudeCodeIdentity(t *testing.T) {
	const identity = "You are Claude Code, Anthropic's official CLI for Claude."
	const userText = "Explain Claude Code without changing this request."
	tests := []struct {
		name       string
		format     sdktranslator.Format
		payload    string
		override   bool
		wantSystem string
	}{
		{
			name:       "claude blocks preserve project instructions",
			format:     sdktranslator.FormatClaude,
			payload:    `{"model":"deepseek-v4.1-flash","max_tokens":32,"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.296;"},{"type":"text","text":"` + identity + `"},{"type":"text","text":"Follow project rules."}],"messages":[{"role":"user","content":"` + userText + `"}],"tools":[{"name":"Read","description":"` + identity + `","input_schema":{"type":"object"}}]}`,
			wantSystem: "Follow project rules.",
		},
		{
			name:    "claude identity only removes empty system message",
			format:  sdktranslator.FormatClaude,
			payload: `{"model":"deepseek-v4.1-flash","max_tokens":32,"system":"` + identity + `","messages":[{"role":"user","content":"` + userText + `"}],"tools":[{"name":"Read","description":"` + identity + `","input_schema":{"type":"object"}}]}`,
		},
		{
			name:       "openai string preserves other system messages",
			format:     sdktranslator.FormatOpenAI,
			payload:    `{"model":"deepseek-v4.1-flash","messages":[{"role":"system","content":"` + identity + `"},{"role":"system","content":"Follow project rules."},{"role":"user","content":"` + userText + `"}],"tools":[{"type":"function","function":{"name":"Read","description":"` + identity + `","parameters":{"type":"object"}}}]}`,
			wantSystem: "Follow project rules.",
		},
		{
			name:       "quoted identity in project instructions is preserved",
			format:     sdktranslator.FormatOpenAI,
			payload:    `{"model":"deepseek-v4.1-flash","messages":[{"role":"system","content":"Keep this example: ` + identity + `"},{"role":"user","content":"` + userText + `"}],"tools":[{"type":"function","function":{"name":"Read","description":"` + identity + `","parameters":{"type":"object"}}}]}`,
			wantSystem: "Keep this example: " + identity,
		},
		{
			name:       "payload override is the final semantic barrier",
			format:     sdktranslator.FormatOpenAI,
			payload:    `{"model":"deepseek-v4.1-flash","messages":[{"role":"system","content":[{"type":"text","text":"` + identity + `"},{"type":"text","text":"Follow project rules."}]},{"role":"user","content":"` + userText + `"}],"tools":[{"type":"function","function":{"name":"Read","description":"` + identity + `","parameters":{"type":"object"}}}]}`,
			override:   true,
			wantSystem: identity,
		},
	}
	for _, tt := range tests {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tt.name, streaming), func(t *testing.T) {
				bodies := make(chan []byte, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					bodies <- body
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}))
				defer server.Close()
				cfg := &config.Config{}
				if tt.override {
					cfg.Payload.Override = []config.PayloadRule{{
						Models: []config.PayloadModelRule{{Name: "*"}},
						Params: map[string]any{"messages.0.content": identity},
					}}
				}
				executor := NewCodeBuddyExecutor(cfg)
				auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}}
				req := cliproxyexecutor.Request{Model: "deepseek-v4.1-flash", Payload: []byte(tt.payload)}
				opts := cliproxyexecutor.Options{SourceFormat: tt.format, OriginalRequest: req.Payload, Stream: streaming}
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
				body := <-bodies
				system := gjson.GetBytes(body, `messages.#(role=="system")`)
				if tt.wantSystem == "" {
					if system.Exists() {
						t.Fatalf("identity-only system message retained: %s", body)
					}
				} else {
					content := system.Get("content")
					if content.IsArray() {
						if len(content.Array()) != 1 {
							t.Fatalf("unexpected system blocks: %s", body)
						}
						content = content.Get("0.text")
					}
					if content.String() != tt.wantSystem {
						t.Fatalf("system content = %q, want %q: %s", content.String(), tt.wantSystem, body)
					}
				}
				user := gjson.GetBytes(body, `messages.#(role=="user").content`)
				if user.IsArray() {
					user = user.Get("0.text")
				}
				if user.String() != userText || gjson.GetBytes(body, "tools.0.function.description").String() != identity {
					t.Fatalf("user content or tool description changed: %s", body)
				}
			})
		}
	}
}

func TestCodeBuddyCountTokensClaudeCodeIdentity(t *testing.T) {
	executor := NewCodeBuddyExecutor(&config.Config{})
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI}
	baseline := cliproxyexecutor.Request{
		Model:   "deepseek-v4.1-flash",
		Payload: []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"system","content":[{"type":"text","text":"Follow project rules."}]},{"role":"user","content":"hello"}]}`),
	}
	withIdentity := baseline
	withIdentity.Payload = []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"system","content":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."},{"type":"text","text":"Follow project rules."}]},{"role":"user","content":"hello"}]}`)
	want, errBaseline := executor.CountTokens(context.Background(), nil, baseline, opts)
	if errBaseline != nil {
		t.Fatal(errBaseline)
	}
	got, errCount := executor.CountTokens(context.Background(), nil, withIdentity, opts)
	if errCount != nil {
		t.Fatal(errCount)
	}
	if string(got.Payload) != string(want.Payload) {
		t.Fatalf("token count includes removed identity: got %s, want %s", got.Payload, want.Payload)
	}
}
