package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestCodeBuddyTruncationDoesNotReplayOrCoolCredentials(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, status := range []int{http.StatusOK, http.StatusBadGateway} {
			t.Run(fmt.Sprintf("stream=%v/status=%d", streaming, status), func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(status)
					if status == http.StatusOK {
						_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
					} else {
						_, _ = io.WriteString(w, `{"error":{"message":"upstream rejected request"}}`)
					}
				}))
				defer server.Close()
				manager := cliproxyauth.NewManager(nil, &cliproxyauth.RoundRobinSelector{}, nil)
				cfg := &config.Config{}
				manager.SetConfig(cfg)
				manager.SetRetryConfig(0, 0, 0)
				manager.RegisterExecutor(NewCodeBuddyExecutor(cfg))
				const model = "codebuddy-truncation-test"
				for i := 0; i < 2; i++ {
					id := fmt.Sprintf("%s/%d", t.Name(), i)
					registry.GetGlobalRegistry().RegisterClient(id, "codebuddy", []*registry.ModelInfo{{ID: model}})
					t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
					_, errRegister := manager.Register(context.Background(), &cliproxyauth.Auth{ID: id, Provider: "codebuddy", Status: cliproxyauth.StatusActive, Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}})
					if errRegister != nil {
						t.Fatal(errRegister)
					}
				}
				req := cliproxyexecutor.Request{Model: model, Payload: []byte(`{"model":"codebuddy-truncation-test","input":"hello"}`)}
				opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, Stream: streaming}
				var errResult error
				if streaming {
					var result *cliproxyexecutor.StreamResult
					result, errResult = manager.ExecuteStream(context.Background(), []string{"codebuddy"}, req, opts)
					if result != nil {
						for chunk := range result.Chunks {
							if chunk.Err != nil {
								errResult = chunk.Err
							}
						}
					}
				} else {
					_, errResult = manager.Execute(context.Background(), []string{"codebuddy"}, req, opts)
				}
				if errResult == nil {
					t.Fatal("expected terminal error")
				}
				wantCalls, wantCooled := int32(1), 0
				if status != http.StatusOK {
					wantCalls, wantCooled = 2, 2
				}
				cooled := 0
				for _, auth := range manager.List() {
					if state := auth.ModelStates[model]; state != nil && state.Unavailable && state.NextRetryAfter.After(time.Now()) {
						cooled++
					}
				}
				if calls.Load() != wantCalls || cooled != wantCooled {
					t.Fatalf("calls=%d, cooled=%d, want calls=%d, cooled=%d", calls.Load(), cooled, wantCalls, wantCooled)
				}
			})
		}
	}
}

func TestCodeBuddyCancellationClosesUpstream(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", streaming), func(t *testing.T) {
			started, stopped := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
				w.(http.Flusher).Flush()
				close(started)
				<-r.Context().Done()
				close(stopped)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			executor := NewCodeBuddyExecutor(&config.Config{})
			auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{"access_token": "test-token"}}
			req := cliproxyexecutor.Request{Model: "glm-5.3", Payload: []byte(`{"model":"glm-5.3","input":"hello"}`)}
			opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, Stream: streaming}
			type result struct {
				payload []byte
				err     error
			}
			done := make(chan result, 1)
			go func() {
				var observed result
				if streaming {
					stream, errStream := executor.ExecuteStream(ctx, auth, req, opts)
					observed.err = errStream
					if stream != nil {
						for chunk := range stream.Chunks {
							observed.payload = append(observed.payload, chunk.Payload...)
							if chunk.Err != nil {
								observed.err = chunk.Err
							}
						}
					}
				} else {
					response, errExecute := executor.Execute(ctx, auth, req, opts)
					observed = result{payload: response.Payload, err: errExecute}
				}
				done <- observed
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("upstream did not start")
			}
			cancel()
			select {
			case observed := <-done:
				if !streaming && !errors.Is(observed.err, context.Canceled) {
					t.Errorf("error = %v, want cancellation", observed.err)
				}
				if bytes.Contains(observed.payload, []byte(`"status":"completed"`)) {
					t.Errorf("canceled response completed: %s", observed.payload)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("executor did not stop on cancellation")
			}
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("upstream connection remained open")
			}
		})
	}
}
