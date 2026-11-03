package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

// TestCodeBuddySendsConversationHeaders guards the upstream channel-verification
// contract: CodeBuddy expects the X-Conversation-* header family on every chat
// request, and rejects requests without it using code 11128.
func TestCodeBuddySendsConversationHeaders(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run("stream="+boolLabel(streaming), func(t *testing.T) {
			headers := make(chan http.Header, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				headers <- r.Header.Clone()
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()

			executor := NewCodeBuddyExecutor(&config.Config{})
			auth := &cliproxyauth.Auth{
				Attributes: map[string]string{"base_url": server.URL},
				Metadata:   map[string]any{"access_token": "test-token"},
			}
			payload := []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}]}`)
			req := cliproxyexecutor.Request{Model: "deepseek-v4.1-flash", Payload: payload}
			opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, OriginalRequest: payload, Stream: streaming}

			const sessionID = "f740af01-5560-4173-a7b6-7153a92854d5"
			ctx := util.WithSessionID(context.Background(), sessionID)
			if streaming {
				result, errStream := executor.ExecuteStream(ctx, auth, req, opts)
				if errStream != nil {
					t.Fatal(errStream)
				}
				for chunk := range result.Chunks {
					if chunk.Err != nil {
						t.Fatal(chunk.Err)
					}
				}
			} else if _, errExecute := executor.Execute(ctx, auth, req, opts); errExecute != nil {
				t.Fatal(errExecute)
			}

			got := <-headers
			// Upstream expects a bare UUID; proxy-side namespace prefixes must
			// never reach it.
			convID := got.Get("X-Conversation-ID")
			if parsed, errParse := uuid.Parse(convID); errParse != nil || parsed.String() != convID {
				t.Fatalf("X-Conversation-ID = %q, want a bare canonical UUID", convID)
			}
			messageID := got.Get("X-Conversation-Message-ID")
			if len(messageID) != 32 || strings.Contains(messageID, "-") {
				t.Fatalf("X-Conversation-Message-ID = %q, want 32-char hyphenless hex", messageID)
			}
			if reqID := got.Get("X-Conversation-Request-ID"); reqID != messageID {
				t.Fatalf("X-Conversation-Request-ID = %q, want %q", reqID, messageID)
			}
			if reqID := got.Get("X-Request-ID"); reqID != messageID {
				t.Fatalf("X-Request-ID = %q, want %q", reqID, messageID)
			}
			// The impersonated IDE version must not regress to the stale 4.9.7.
			if version := got.Get("X-IDE-Version"); version != "4.12.2" {
				t.Fatalf("X-IDE-Version = %q, want 4.12.2", version)
			}
		})
	}
}

// TestCodeBuddyDerivesConversationIDWithoutExplicitSession documents the
// fallback path: when the client sends no session signal, the derived message
// hash still supplies a stable conversation identity instead of an empty one.
func TestCodeBuddyDerivesConversationIDWithoutExplicitSession(t *testing.T) {
	headers := make(chan http.Header, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "test-token"},
	}
	payload := []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}]}`)
	req := cliproxyexecutor.Request{Model: "deepseek-v4.1-flash", Payload: payload}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, OriginalRequest: payload}

	for i := 0; i < 2; i++ {
		if _, errExecute := executor.Execute(context.Background(), auth, req, opts); errExecute != nil {
			t.Fatal(errExecute)
		}
	}
	first, second := <-headers, <-headers
	if first.Get("X-Conversation-ID") == "" {
		t.Fatal("X-Conversation-ID must be derived when the client sends no session signal")
	}
	if first.Get("X-Conversation-ID") != second.Get("X-Conversation-ID") {
		t.Fatalf("derived conversation id must stay stable across turns: %q vs %q",
			first.Get("X-Conversation-ID"), second.Get("X-Conversation-ID"))
	}
	if first.Get("X-Conversation-Message-ID") == "" {
		t.Fatal("per-request X-Conversation-Message-ID must always be present")
	}
}

// TestCodeBuddyConversationIDStripsProxyPrefixes guards against leaking the
// proxy's internal session namespace ("claude:", "session:", ...) to upstream,
// which would itself be a distinctive non-official-client fingerprint.
func TestCodeBuddyConversationIDStripsProxyPrefixes(t *testing.T) {
	cases := []struct{ name, input string }{
		{"plain uuid", "f740af01-5560-4173-a7b6-7153a92854d5"},
		{"claude namespace", "claude:f740af01-5560-4173-a7b6-7153a92854d5"},
		{"session namespace", "session:session-998"},
		{"derived message hash", "msg:2f68951593d97234"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := codeBuddyConversationID(util.WithSessionID(context.Background(), tc.input))
			parsed, errParse := uuid.Parse(got)
			if errParse != nil {
				t.Fatalf("conversation id %q is not a UUID: %v", got, errParse)
			}
			if parsed.String() != got {
				t.Fatalf("conversation id %q is not in canonical UUID form", got)
			}
			if strings.Contains(got, ":") {
				t.Fatalf("conversation id %q still carries a proxy namespace prefix", got)
			}
			// Stability: the same session must always map to the same upstream id.
			if again := codeBuddyConversationID(util.WithSessionID(context.Background(), tc.input)); again != got {
				t.Fatalf("conversation id not deterministic: %q vs %q", got, again)
			}
		})
	}

	// Distinct sessions must not collide onto one upstream conversation.
	a := codeBuddyConversationID(util.WithSessionID(context.Background(), "claude:aaa"))
	b := codeBuddyConversationID(util.WithSessionID(context.Background(), "claude:bbb"))
	if a == b {
		t.Fatalf("distinct sessions collapsed to one conversation id %q", a)
	}
}

func boolLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
