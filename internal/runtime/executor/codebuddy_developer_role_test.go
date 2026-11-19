package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

// CodeBuddy rejects any request carrying a developer-role message with code
// 11128, so the executor must send system instead.
func TestCodeBuddyExecutorNormalizesDeveloperRole(t *testing.T) {
	tests := []struct {
		name    string
		format  sdktranslator.Format
		payload string
	}{
		{
			name:    "openai developer messages",
			format:  sdktranslator.FormatOpenAI,
			payload: `{"model":"deepseek-v4.1-flash","messages":[{"role":"developer","content":"You are Codex, a coding agent."},{"role":"user","content":"hi"}]}`,
		},
		{
			name:    "claude system instructions",
			format:  sdktranslator.FormatClaude,
			payload: `{"model":"deepseek-v4.1-flash","max_tokens":32,"system":"Follow project rules.","messages":[{"role":"user","content":"hi"}]}`,
		},
	}
	for _, tt := range tests {
		for _, streaming := range []bool{false, true} {
			t.Run(tt.name+"/stream="+map[bool]string{true: "true", false: "false"}[streaming], func(t *testing.T) {
				bodies := make(chan []byte, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					bodies <- body
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}))
				defer server.Close()

				executor := NewCodeBuddyExecutor(&config.Config{})
				auth := &cliproxyauth.Auth{
					Attributes: map[string]string{"base_url": server.URL},
					Metadata:   map[string]any{"access_token": "test-token"},
				}
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
				if developer := ojson.GetBytes(body, `messages.#(role=="developer")`); developer.Exists() {
					t.Fatalf("developer role leaked upstream: %s", body)
				}
				if system := ojson.GetBytes(body, `messages.#(role=="system")`); !system.Exists() {
					t.Fatalf("system instructions were dropped: %s", body)
				}
			})
		}
	}
}

func TestCodeBuddyCountTokensNormalizesDeveloperRole(t *testing.T) {
	executor := NewCodeBuddyExecutor(&config.Config{})
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI}
	baseline := cliproxyexecutor.Request{
		Model:   "deepseek-v4.1-flash",
		Payload: []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"system","content":"Follow project rules."},{"role":"user","content":"hello"}]}`),
	}
	withDeveloper := baseline
	withDeveloper.Payload = []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"developer","content":"Follow project rules."},{"role":"user","content":"hello"}]}`)

	want, errBaseline := executor.CountTokens(context.Background(), nil, baseline, opts)
	if errBaseline != nil {
		t.Fatal(errBaseline)
	}
	got, errCount := executor.CountTokens(context.Background(), nil, withDeveloper, opts)
	if errCount != nil {
		t.Fatal(errCount)
	}
	if string(got.Payload) != string(want.Payload) {
		t.Fatalf("token count differs for developer role: got %s, want %s", got.Payload, want.Payload)
	}
}
