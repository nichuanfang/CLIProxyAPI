package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	codebuddyauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codebuddy"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyExecutorExecute(t *testing.T) {
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"glm5.3\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "glm5.3",
		Payload: []byte(`{"model":"glm5.3","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if gotPath != codebuddyauth.ChatPath {
		t.Fatalf("path = %s, want %s", gotPath, codebuddyauth.ChatPath)
	}
	if got := gjson.GetBytes(gotBody, "model").String(); got != "glm5.3" {
		t.Fatalf("upstream model = %s, body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "messages.0.content").String(); got != "hi" {
		t.Fatalf("payload not translated: %s", string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "stream").Bool(); !got {
		t.Fatalf("upstream request must force stream=true: %s", string(gotBody))
	}
	if got := gjson.GetBytes(resp.Payload, "choices.0.message.content").String(); got != "hello" {
		t.Fatalf("response content = %s, payload=%s", got, string(resp.Payload))
	}
}

func TestCodeBuddyExecutorStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("Accept = %s", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-2\",\"object\":\"chat.completion.chunk\",\"model\":\"glm5.3\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "glm5.3",
		Payload: []byte(`{"model":"glm5.3","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: true})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	var payload []byte
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
		payload = append(payload, chunk.Payload...)
	}
	if !bytes.Contains(payload, []byte("hi")) {
		t.Fatalf("stream payload = %s", string(payload))
	}
}

func TestCodeBuddyExecutorHeadersAndCountTokens(t *testing.T) {
	var authHeader, ideHeader, intentHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		ideHeader = r.Header.Get("X-IDE-Type")
		intentHeader = r.Header.Get("X-Agent-Intent")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "secret"},
	}
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-v4.1-flash",
		Payload: []byte(`{"model":"deepseek-v4.1-flash","messages":[]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if authHeader != "Bearer secret" || ideHeader != "CodeBuddyIDE" || intentHeader != "craft" {
		t.Fatalf("headers = %q %q %q", authHeader, ideHeader, intentHeader)
	}
	countResp, err := executor.CountTokens(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "glm5.3",
		Payload: []byte(`{"model":"glm5.3","messages":[{"role":"user","content":"hello world"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	if err != nil {
		t.Fatalf("CountTokens() error = %v", err)
	}
	if got := gjson.GetBytes(countResp.Payload, "usage.prompt_tokens").Int(); got <= 0 {
		t.Fatalf("CountTokens() prompt_tokens = %d, payload=%s", got, string(countResp.Payload))
	}
}

func TestCodeBuddyExecutorRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/plugin/auth/token/refresh" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-Refresh-Token") == "" {
			t.Fatal("refresh endpoint must receive X-Refresh-Token")
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"accessToken":"new","refreshToken":"new-refresh","expiresIn":7200}}`))
	}))
	defer server.Close()

	// The auth client appends the refresh path to the credential base URL.
	clientBase := server.URL
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": clientBase},
		Metadata: map[string]any{
			"access_token":  "old",
			"refresh_token": "old-refresh",
		},
		Storage: &codebuddyauth.TokenStorage{AccessToken: "old", RefreshToken: "old-refresh", BaseURL: clientBase, Type: "codebuddy"},
	}
	executor := NewCodeBuddyExecutor(&config.Config{})
	refreshed, err := executor.Refresh(context.Background(), auth)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshed.Metadata["access_token"] != "new" || refreshed.Metadata["refresh_token"] != "new-refresh" {
		t.Fatalf("metadata = %+v", refreshed.Metadata)
	}
	if refreshed.Metadata["last_refresh"] == "" {
		t.Fatal("last_refresh not set")
	}
	if got := refreshed.Metadata["expires_in"]; got != 7200 {
		t.Fatalf("expires_in = %v, want 7200", got)
	}
	if got := refreshed.Metadata["refresh_interval_seconds"]; got != 5760 {
		t.Fatalf("refresh_interval_seconds = %v, want 5760", got)
	}
	storage, ok := refreshed.Storage.(*codebuddyauth.TokenStorage)
	if !ok || storage.AccessToken != "new" || storage.RefreshToken != "new-refresh" {
		t.Fatalf("storage = %#v", refreshed.Storage)
	}
}

func TestCodeBuddyExecutorToolTranslation(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-tool\",\"object\":\"chat.completion.chunk\",\"model\":\"glm5.3\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"Beijing\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":5,\"total_tokens\":14}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	payload := []byte(`{"model":"glm5.3","max_tokens":64,"system":"Use the tool","tools":[{"name":"get_weather","description":"Get weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}],"messages":[{"role":"user","content":[{"type":"text","text":"Weather in Beijing?"}]}]}`)
	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "glm5.3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := gjson.GetBytes(gotBody, "tools.#").Int(); got != 1 {
		t.Fatalf("upstream tools = %d, body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "tools.0.function.name").String(); got != "get_weather" {
		t.Fatalf("tool name = %q, body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "messages.1.content.0.text").String(); got != "Weather in Beijing?" {
		t.Fatalf("user content mismatch: %s", string(gotBody))
	}
	if got := gjson.GetBytes(resp.Payload, "content.#").Int(); got != 1 {
		t.Fatalf("claude content blocks = %d, payload=%s", got, string(resp.Payload))
	}
	if got := gjson.GetBytes(resp.Payload, "content.0.type").String(); got != "tool_use" {
		t.Fatalf("claude block type = %q, payload=%s", got, string(resp.Payload))
	}
	if got := gjson.GetBytes(resp.Payload, "content.0.name").String(); got != "get_weather" {
		t.Fatalf("claude tool name = %q, payload=%s", got, string(resp.Payload))
	}
	if got := gjson.GetBytes(resp.Payload, "content.0.input.city").String(); got != "Beijing" {
		t.Fatalf("claude tool input = %s, payload=%s", gjson.GetBytes(resp.Payload, "content.0.input").Raw, string(resp.Payload))
	}
	if got := gjson.GetBytes(resp.Payload, "stop_reason").String(); got != "tool_use" {
		t.Fatalf("stop_reason = %q, payload=%s", got, string(resp.Payload))
	}
}

func TestCodeBuddyExecutorToolResultTranslation(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-after-tool\",\"object\":\"chat.completion.chunk\",\"model\":\"glm5.3\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Sunny, 31C.\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":4,\"total_tokens\":15}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	payload := []byte(`{"model":"glm5.3","max_tokens":64,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"get_weather","input":{"city":"Beijing"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"Sunny, 31C"}]}]}]}`)
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "glm5.3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := gjson.GetBytes(gotBody, "messages.#").Int(); got != 2 {
		t.Fatalf("message count = %d, body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "messages.0.role").String(); got != "assistant" {
		t.Fatalf("assistant role = %q, body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "messages.0.tool_calls.0.id").String(); got != "call_1" {
		t.Fatalf("assistant tool call id = %q, body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "messages.0.tool_calls.0.function.name").String(); got != "get_weather" {
		t.Fatalf("assistant tool call name = %q, body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "messages.0.tool_calls.0.function.arguments").String(); got != `{"city":"Beijing"}` {
		t.Fatalf("assistant tool arguments = %q, body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "messages.1.role").String(); got != "tool" {
		t.Fatalf("result role = %q, body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "messages.1.tool_call_id").String(); got != "call_1" {
		t.Fatalf("result tool_call_id = %q, body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "messages.1.content").String(); got != "Sunny, 31C" {
		t.Fatalf("result content = %q, body=%s", got, string(gotBody))
	}
}

func TestCodeBuddyExecutorToolCallStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-stream-tool\",\"object\":\"chat.completion.chunk\",\"model\":\"glm5.3\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_stream\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"Beijing\\\"}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	payload := []byte(`{"model":"glm5.3","messages":[{"role":"user","content":"Weather?"}],"stream":true,"tools":[{"name":"get_weather","description":"Get weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}]}`)
	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "glm5.3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude, Stream: true, OriginalRequest: payload})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	var payloadOut []byte
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
		payloadOut = append(payloadOut, chunk.Payload...)
	}
	if !bytes.Contains(payloadOut, []byte(`"type":"tool_use"`)) && !bytes.Contains(payloadOut, []byte(`"type": "tool_use"`)) {
		t.Fatalf("stream payload missing tool_use: %s", string(payloadOut))
	}
	if !bytes.Contains(payloadOut, []byte("get_weather")) {
		t.Fatalf("stream payload missing tool name: %s", string(payloadOut))
	}
	if !bytes.Contains(payloadOut, []byte("Beijing")) {
		t.Fatalf("stream payload missing tool input: %s", string(payloadOut))
	}
}

func TestCodeBuddyExecutorExecuteAggregatesReasoningAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-r\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4.1-flash\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"think\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-r\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4.1-flash\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7,\"completion_tokens_details\":{\"reasoning_tokens\":1}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-v4.1-flash",
		Payload: []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := gjson.GetBytes(resp.Payload, "choices.0.message.content").String(); got != "answer" {
		t.Fatalf("content = %q, payload=%s", got, string(resp.Payload))
	}
	if got := gjson.GetBytes(resp.Payload, "choices.0.message.reasoning_content").String(); got != "think" {
		t.Fatalf("reasoning_content = %q, payload=%s", got, string(resp.Payload))
	}
	if got := gjson.GetBytes(resp.Payload, "usage.total_tokens").Int(); got != 7 {
		t.Fatalf("usage.total_tokens = %d, payload=%s", got, string(resp.Payload))
	}
}

func TestCodeBuddyExecutorExecuteInjectsPromptCacheKey(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:    "glm5.3",
		Payload:  []byte(`{"model":"glm5.3","messages":[{"role":"user","content":"hi"}],"metadata":{"session_id":"sess-1"}}`),
		Metadata: map[string]any{"session_id": "sess-1"},
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Metadata: map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: "sess-1"}})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := gjson.GetBytes(gotBody, "prompt_cache_key").String(); got == "" {
		t.Fatalf("prompt_cache_key not injected: %s", string(gotBody))
	}
}

func TestCodeBuddyExecutorThinkingLevelsInjected(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-v4.1-flash(medium)",
		Payload: []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := gjson.GetBytes(gotBody, "reasoning_effort").String(); got != "medium" {
		t.Fatalf("reasoning_effort = %q, body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "model").String(); got != "deepseek-v4.1-flash" {
		t.Fatalf("model suffix not stripped: %q", got)
	}
}

func TestCodeBuddyExecutorResponsesFallbackToolContract(t *testing.T) {
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, bytes.Clone(body))
		w.Header().Set("Content-Type", "text/event-stream")
		if len(bodies) == 1 {
			_, _ = w.Write([]byte(`data: {"id":"chatcmpl-tool","object":"chat.completion.chunk","model":"glm5.3","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Beijing\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n"))
		} else {
			_, _ = w.Write([]byte(`data: {"id":"chatcmpl-final","object":"chat.completion.chunk","model":"glm5.3","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}` + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	responsesPayload := []byte(`{"model":"glm5.3","stream":true,"input":[{"type":"message","id":"msg-1","role":"user","content":[{"type":"input_text","text":"weather"}]}],"tools":[{"type":"function","name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}]}`)
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, Stream: true, OriginalRequest: responsesPayload}
	first, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{Model: "glm5.3", Payload: responsesPayload}, opts)
	if err != nil {
		t.Fatalf("first ExecuteStream() error = %v", err)
	}
	var firstOut []byte
	for chunk := range first.Chunks {
		if chunk.Err != nil {
			t.Fatalf("first stream chunk error = %v", chunk.Err)
		}
		firstOut = append(firstOut, chunk.Payload...)
	}
	if !bytes.Contains(firstOut, []byte(`"type":"function_call"`)) {
		t.Fatalf("first stream payload missing function_call: %s", firstOut)
	}

	toolPayload := []byte(`{"model":"glm5.3","stream":true,"input":[{"type":"message","id":"msg-1","role":"user","content":[{"type":"input_text","text":"weather"}]},{"type":"function_call","id":"fc-1","call_id":"call-1","name":"get_weather","arguments":"{\"city\":\"Beijing\"}"},{"type":"function_call_output","id":"out-1","call_id":"call-1","output":"Sunny, 31C"}],"tools":[{"type":"function","name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}]}`)
	second, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{Model: "glm5.3", Payload: toolPayload}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, Stream: true, OriginalRequest: toolPayload})
	if err != nil {
		t.Fatalf("second ExecuteStream() error = %v", err)
	}
	for chunk := range second.Chunks {
		if chunk.Err != nil {
			t.Fatalf("second stream chunk error = %v", chunk.Err)
		}
	}
	if len(bodies) != 2 {
		t.Fatalf("upstream request count = %d, want 2", len(bodies))
	}
	if got := gjson.GetBytes(bodies[0], "tools.0.function.name").String(); got != "get_weather" {
		t.Fatalf("tool declaration = %q, body=%s", got, bodies[0])
	}
	if got := gjson.GetBytes(bodies[1], "messages.#(role==\"assistant\").tool_calls.0.function.name").String(); got != "get_weather" {
		t.Fatalf("assistant tool call missing: %s", bodies[1])
	}
	if got := gjson.GetBytes(bodies[1], "messages.#(role==\"tool\").tool_call_id").String(); got != "call-1" {
		t.Fatalf("tool call id = %q, want call-1: %s", got, bodies[1])
	}
	if got := gjson.GetBytes(bodies[1], "messages.#(role==\"tool\").content").String(); got != "Sunny, 31C" {
		t.Fatalf("tool output = %q, body=%s", got, bodies[1])
	}
}

func TestCodeBuddyExecutorStreamExposesUpstreamStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid image","type":"invalid_request_error","code":"bad_image"}}`))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	result, streamStartErr := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "glm5.3",
		Payload: []byte(`{"model":"glm5.3","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: true})
	if streamStartErr == nil {
		for chunk := range result.Chunks {
			if chunk.Err != nil {
				streamStartErr = chunk.Err
				break
			}
		}
	}
	if streamStartErr == nil {
		t.Fatal("ExecuteStream() = nil error, want upstream status error")
	}
	streamErr := streamStartErr
	statusErr, ok := streamErr.(statusErr)
	if !ok {
		t.Fatalf("stream error type = %T, want statusErr: %v", streamErr, streamErr)
	}
	if statusErr.code != http.StatusBadRequest {
		t.Fatalf("stream error status = %d, want %d", statusErr.code, http.StatusBadRequest)
	}
	if !strings.Contains(statusErr.msg, "invalid image") || !strings.Contains(statusErr.msg, "bad_image") {
		t.Fatalf("stream error lost upstream detail: %q", statusErr.msg)
	}
}
