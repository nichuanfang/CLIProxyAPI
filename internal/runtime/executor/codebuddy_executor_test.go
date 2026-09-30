package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"glm5.3","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
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
	if _, err := executor.CountTokens(context.Background(), auth, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}); err == nil {
		t.Fatal("CountTokens() expected unsupported error")
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-tool","object":"chat.completion","model":"glm5.3","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Beijing\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14}}`))
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-after-tool","object":"chat.completion","model":"glm5.3","choices":[{"index":0,"message":{"role":"assistant","content":"Sunny, 31C."},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":4,"total_tokens":15}}`))
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
