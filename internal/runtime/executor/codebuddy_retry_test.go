package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestCodeBuddyExecutorRetryAfter(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, tc := range []struct {
			name       string
			status     int
			sse        bool
			retryAfter string
			want       time.Duration
		}{
			{name: "rate limit", status: 429, want: 10 * time.Second},
			{name: "service error", status: 500, want: 10 * time.Second},
			{name: "header takes precedence", status: 429, retryAfter: "120", want: 120 * time.Second},
			{name: "SSE error", status: 429, sse: true, want: 10 * time.Second},
			{name: "SSE service error", status: 500, sse: true, want: 10 * time.Second},
		} {
			t.Run(fmt.Sprintf("stream=%v/%s", streaming, tc.name), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body := fmt.Sprintf(`{"code":14003,"msg":"model busy","status":%d,"retry_policy":{"retry":true,"retry_after":10}}`, tc.status)
					if tc.sse {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", body)
						return
					}
					if tc.retryAfter != "" {
						w.Header().Set("Retry-After", tc.retryAfter)
					}
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, body)
				}))
				defer server.Close()
				auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}}
				payload := []byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}]}`)
				req := cliproxyexecutor.Request{Model: "glm-5.3", Payload: payload}
				opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: streaming}
				exec := NewCodeBuddyExecutor(&config.Config{})
				var errResult error
				if streaming {
					result, errStream := exec.ExecuteStream(context.Background(), auth, req, opts)
					errResult = errStream
					if result != nil {
						for chunk := range result.Chunks {
							if chunk.Err != nil {
								errResult = chunk.Err
							}
						}
					}
				} else {
					_, errResult = exec.Execute(context.Background(), auth, req, opts)
				}
				var upstreamErr statusErr
				if !errors.As(errResult, &upstreamErr) || upstreamErr.StatusCode() != tc.status || upstreamErr.RetryAfter() == nil || *upstreamErr.RetryAfter() != tc.want {
					t.Fatalf("error = %#v, want status %d and retry delay %v", errResult, tc.status, tc.want)
				}
			})
		}
	}
}

func TestCodeBuddyRetryHintReachesCooldown(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", streaming), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"code":10000,"msg":"invoke model error","retry_policy":{"retry":true,"retry_after":10}}`)
			}))
			defer server.Close()
			const model = "codebuddy-retry-hint-test"
			auth := &cliproxyauth.Auth{ID: t.Name(), Provider: "codebuddy", Status: cliproxyauth.StatusActive, Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}}
			registry.GetGlobalRegistry().RegisterClient(auth.ID, "codebuddy", []*registry.ModelInfo{{ID: model}})
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
			cfg := &config.Config{}
			manager := cliproxyauth.NewManager(nil, &cliproxyauth.RoundRobinSelector{}, nil)
			manager.SetConfig(cfg)
			manager.SetRetryConfig(0, 0, 0)
			manager.RegisterExecutor(NewCodeBuddyExecutor(cfg))
			if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
				t.Fatal(errRegister)
			}
			req := cliproxyexecutor.Request{Model: model, Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}
			opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: streaming}
			started := time.Now()
			var errResult error
			if streaming {
				_, errResult = manager.ExecuteStream(context.Background(), []string{"codebuddy"}, req, opts)
			} else {
				_, errResult = manager.Execute(context.Background(), []string{"codebuddy"}, req, opts)
			}
			finished := time.Now()
			if errResult == nil {
				t.Fatal("expected upstream rejection")
			}
			updated, ok := manager.GetByID(auth.ID)
			if !ok || updated.ModelStates[model] == nil {
				t.Fatal("missing model cooldown state")
			}
			next := updated.ModelStates[model].NextRetryAfter
			if next.Before(started.Add(10*time.Second)) || next.After(finished.Add(10*time.Second)) {
				t.Fatalf("cooldown = %v, want upstream hint of 10s after failure", next)
			}
		})
	}
}
