package openai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	codebuddyexecutor "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/tidwall/gjson"
)

const codebuddyFallbackModel = "glm5.3"

type codebuddyFallbackUpstream struct {
	mu       sync.Mutex
	requests [][]byte
	calls    int
	events   []string
}

func (u *codebuddyFallbackUpstream) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read codebuddy upstream body: %v", err)
			return
		}
		u.mu.Lock()
		call := u.calls
		u.calls++
		u.requests = append(u.requests, bytes.Clone(body))
		event := "success"
		if call < len(u.events) {
			event = u.events[call]
		}
		u.mu.Unlock()

		switch event {
		case "success":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(u.chunk(call, "ok", "stop")))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case "tool":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"id":"chatcmpl-tool","object":"chat.completion.chunk","model":"` + codebuddyFallbackModel + `","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Beijing\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			status, err := strconv.Atoi(event)
			if err != nil {
				t.Fatalf("invalid upstream status fixture %q: %v", event, err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"codebuddy upstream rejected the request"}}`))
		}
	}
}

func (u *codebuddyFallbackUpstream) chunk(call int, content, finish string) string {
	return `data: {"id":"chatcmpl-` + fmt.Sprint(call+1) + `","object":"chat.completion.chunk","model":"` + codebuddyFallbackModel + `","choices":[{"index":0,"delta":{"role":"assistant","content":` + quoteGoString(content) + `},"finish_reason":"` + finish + `"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}` + "\n\n"
}

func (u *codebuddyFallbackUpstream) bodies() [][]byte {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([][]byte, len(u.requests))
	for i := range u.requests {
		out[i] = bytes.Clone(u.requests[i])
	}
	return out
}

func quoteGoString(value string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		default:
			out.WriteRune(r)
		}
	}
	out.WriteByte('"')
	return out.String()
}

func newCodeBuddyFallbackWebsocket(t *testing.T, events ...string) (*codebuddyFallbackUpstream, *websocket.Conn) {
	t.Helper()
	upstream := &codebuddyFallbackUpstream{events: events}
	upstreamServer := httptest.NewServer(upstream.handler(t))
	t.Cleanup(upstreamServer.Close)

	executor := codebuddyexecutor.NewCodeBuddyExecutor(&config.Config{})
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)
	auth := &coreauth.Auth{
		ID:         "codebuddy-http-auth",
		Provider:   executor.Identifier(),
		Status:     coreauth.StatusActive,
		Attributes: map[string]string{"base_url": upstreamServer.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register codebuddy auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: codebuddyFallbackModel}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/v1/responses/ws", NewOpenAIResponsesAPIHandler(base).ResponsesWebsocket)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/responses/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial codebuddy fallback websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return upstream, conn
}

func readCodeBuddyWebsocketEvent(t *testing.T, conn *websocket.Conn, eventType string) []byte {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set websocket read deadline: %v", err)
	}
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read websocket %s event: %v", eventType, err)
		}
		if got := gjson.GetBytes(payload, "type").String(); got == eventType {
			return bytes.Clone(payload)
		}
	}
}

func readCodeBuddyWebsocketEventOrClose(t *testing.T, conn *websocket.Conn) ([]byte, error) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set websocket read deadline: %v", err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	return bytes.Clone(payload), nil
}

func writeCodeBuddyWebsocket(t *testing.T, conn *websocket.Conn, payload string) {
	t.Helper()
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set websocket write deadline: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
		t.Fatalf("write websocket request: %v", err)
	}
}

func assertCodeBuddyChatMessages(t *testing.T, body []byte, wantContent ...string) {
	t.Helper()
	if got := gjson.GetBytes(body, "stream").Bool(); !got {
		t.Fatalf("codebuddy request must force streaming: %s", body)
	}
	if gjson.GetBytes(body, "previous_response_id").Exists() {
		t.Fatalf("previous_response_id leaked to codebuddy chat request: %s", body)
	}
	messages := gjson.GetBytes(body, "messages").Array()
	if len(messages) != len(wantContent) {
		t.Fatalf("chat message count = %d, want %d: %s", len(messages), len(wantContent), body)
	}
	for i, want := range wantContent {
		if got := messages[i].Get("content.0.text").String(); got != want {
			t.Fatalf("chat message %d content = %q, want %q: %s", i, got, want, body)
		}
	}
}

func ginSetTestMode() {
	gin.SetMode(gin.TestMode)
}

func TestResponsesWebsocketCodeBuddyFallbackContinuesSession(t *testing.T) {
	ginSetTestMode()
	upstream, conn := newCodeBuddyFallbackWebsocket(t, "success", "success", "success")

	writeCodeBuddyWebsocket(t, conn, `{"type":"response.create","model":"`+codebuddyFallbackModel+`","input":[{"type":"message","id":"msg-1","role":"user","content":[{"type":"input_text","text":"first"}]}]}`)
	first := readCodeBuddyWebsocketEvent(t, conn, wsEventTypeCompleted)
	firstResponseID := gjson.GetBytes(first, "response.id").String()
	if firstResponseID == "" {
		t.Fatalf("first completed response has no id: %s", first)
	}

	writeCodeBuddyWebsocket(t, conn, `{"type":"response.append","input":[{"type":"message","id":"msg-2","role":"user","content":[{"type":"input_text","text":"second"}]}]}`)
	readCodeBuddyWebsocketEvent(t, conn, wsEventTypeCompleted)

	appendRequest := fmt.Sprintf(`{"type":"response.create","previous_response_id":%q,"input":[{"type":"message","id":"msg-3","role":"user","content":[{"type":"input_text","text":"third"}]}]}`, firstResponseID)
	writeCodeBuddyWebsocket(t, conn, appendRequest)
	readCodeBuddyWebsocketEvent(t, conn, wsEventTypeCompleted)

	bodies := upstream.bodies()
	if len(bodies) != 3 {
		t.Fatalf("codebuddy request count = %d, want 3", len(bodies))
	}
	assertCodeBuddyChatMessages(t, bodies[0], "first")
	assertCodeBuddyChatMessages(t, bodies[1], "first", "ok", "second")
	assertCodeBuddyChatMessages(t, bodies[2], "first", "ok", "second", "ok", "third")
}

func TestResponsesWebsocketCodeBuddyFallbackToolCallContinuation(t *testing.T) {
	ginSetTestMode()
	upstream, conn := newCodeBuddyFallbackWebsocket(t, "tool", "success")

	firstRequest := `{"type":"response.create","model":"` + codebuddyFallbackModel + `","input":[{"type":"message","id":"msg-1","role":"user","content":[{"type":"input_text","text":"weather"}]}],"tools":[{"type":"function","name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}]}`
	writeCodeBuddyWebsocket(t, conn, firstRequest)
	toolCompleted := readCodeBuddyWebsocketEvent(t, conn, wsEventTypeCompleted)
	toolResponseID := gjson.GetBytes(toolCompleted, "response.id").String()
	if toolResponseID == "" {
		t.Fatalf("tool completed response has no id: %s", toolCompleted)
	}

	toolOutput := fmt.Sprintf(`{"type":"response.create","previous_response_id":%q,"input":[{"type":"function_call_output","call_id":"call-1","output":"Sunny, 31C"}]}`, toolResponseID)
	writeCodeBuddyWebsocket(t, conn, toolOutput)
	readCodeBuddyWebsocketEvent(t, conn, wsEventTypeCompleted)

	bodies := upstream.bodies()
	if len(bodies) != 2 {
		t.Fatalf("codebuddy request count = %d, want 2", len(bodies))
	}
	if got := gjson.GetBytes(bodies[0], "tools.0.function.name").String(); got != "get_weather" {
		t.Fatalf("tool declaration = %q, want get_weather: %s", got, bodies[0])
	}
	if got := gjson.GetBytes(bodies[1], "messages.#(role==\"assistant\").tool_calls.0.function.name").String(); got != "get_weather" {
		t.Fatalf("assistant tool call missing from continuation request: %s", bodies[1])
	}
	if got := gjson.GetBytes(bodies[1], "messages.#(role==\"tool\").tool_call_id").String(); got != "call-1" {
		t.Fatalf("tool result call id = %q, want call-1: %s", got, bodies[1])
	}
	if got := gjson.GetBytes(bodies[1], "messages.#(role==\"tool\").content").String(); got != "Sunny, 31C" {
		t.Fatalf("tool result content = %q, want Sunny, 31C: %s", got, bodies[1])
	}
}

func TestResponsesWebsocketCodeBuddyFallbackAcceptsFullTranscriptAfterReconnect(t *testing.T) {
	ginSetTestMode()
	_, firstConn := newCodeBuddyFallbackWebsocket(t, "success", "success")
	writeCodeBuddyWebsocket(t, firstConn, `{"type":"response.create","model":"`+codebuddyFallbackModel+`","input":[{"type":"message","id":"msg-1","role":"user","content":[{"type":"input_text","text":"first"}]}]}`)
	readCodeBuddyWebsocketEvent(t, firstConn, wsEventTypeCompleted)
	if err := firstConn.Close(); err != nil {
		t.Fatalf("close first websocket: %v", err)
	}

	secondUpstream, secondConn := newCodeBuddyFallbackWebsocket(t, "success")
	recovery := `{"type":"response.create","model":"` + codebuddyFallbackModel + `","input":[` +
		`{"type":"message","id":"msg-1","role":"user","content":[{"type":"input_text","text":"first"}]},` +
		`{"type":"message","id":"out-1","role":"assistant","content":[{"type":"output_text","text":"ok"}]},` +
		`{"type":"message","id":"msg-2","role":"user","content":[{"type":"input_text","text":"second"}]}]}`
	writeCodeBuddyWebsocket(t, secondConn, recovery)
	readCodeBuddyWebsocketEvent(t, secondConn, wsEventTypeCompleted)

	bodies := secondUpstream.bodies()
	if len(bodies) != 1 {
		t.Fatalf("reconnected codebuddy request count = %d, want 1", len(bodies))
	}
	assertCodeBuddyChatMessages(t, bodies[0], "first", "ok", "second")
}

func TestResponsesWebsocketCodeBuddyFallbackExposesUpstreamErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			ginSetTestMode()
			_, conn := newCodeBuddyFallbackWebsocket(t, fmt.Sprint(status))
			writeCodeBuddyWebsocket(t, conn, `{"type":"response.create","model":"`+codebuddyFallbackModel+`","input":[{"type":"message","id":"msg-1","role":"user","content":[{"type":"input_text","text":"first"}]}]}`)
			errorPayload, errRead := readCodeBuddyWebsocketEventOrClose(t, conn)
			if status == http.StatusTooManyRequests {
				if errRead == nil {
					t.Fatalf("rate-limited credential unexpectedly produced frame: %s", errorPayload)
				}
				return
			}
			if errRead != nil {
				t.Fatalf("read websocket error event for status %d: %v", status, errRead)
			}
			if got := int(gjson.GetBytes(errorPayload, "status").Int()); got != status {
				t.Fatalf("downstream status = %d, want %d: %s", got, status, errorPayload)
			}
			if got := gjson.GetBytes(errorPayload, "error.message").String(); got == "" {
				t.Fatalf("downstream error has no message: %s", errorPayload)
			}
		})
	}
}
