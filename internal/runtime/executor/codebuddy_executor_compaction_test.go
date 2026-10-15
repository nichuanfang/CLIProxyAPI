package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyExecutorCompactionNonStream(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"sum-1\",\"object\":\"chat.completion.chunk\",\"model\":\"glm-5.3\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Summary: user asked about weather tools.\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	payload := []byte(`{"model":"glm-5.3","input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi there"}]},
		{"type":"compaction_trigger"}
	]}`)
	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "glm-5.3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, Alt: "responses/compact"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := gjson.GetBytes(gotBody, "stream").Bool(); !got {
		t.Fatalf("upstream must be stream=true: %s", string(gotBody))
	}
	msgs := gjson.GetBytes(gotBody, "messages")
	if !msgs.IsArray() {
		t.Fatalf("messages missing: %s", string(gotBody))
	}
	foundSummaryPrompt := false
	for _, m := range msgs.Array() {
		if strings.Contains(m.Get("content").String(), "concise and comprehensive summary") {
			foundSummaryPrompt = true
		}
		if strings.Contains(m.Raw, "compaction_trigger") {
			t.Fatalf("compaction_trigger must be removed: %s", string(gotBody))
		}
	}
	if !foundSummaryPrompt {
		t.Fatalf("summary instruction missing: %s", string(gotBody))
	}

	if got := gjson.GetBytes(resp.Payload, "object").String(); got != "response.compaction" {
		t.Fatalf("object = %q, payload=%s", got, string(resp.Payload))
	}
	capsule := gjson.GetBytes(resp.Payload, "output.0.encrypted_content").String()
	if capsule == "" {
		t.Fatalf("capsule missing: %s", string(resp.Payload))
	}
	summary, errUnseal := helps.UnsealAntigravityCompaction(capsule)
	if errUnseal != nil {
		t.Fatalf("unseal: %v", errUnseal)
	}
	if !strings.Contains(summary, "weather tools") {
		t.Fatalf("summary = %q", summary)
	}
	if got := gjson.GetBytes(resp.Payload, "usage.input_tokens").Int(); got != 100 {
		t.Fatalf("input_tokens = %d", got)
	}
}

func TestCodeBuddyExecutorCompactionStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"A compact summary.\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	payload := []byte(`{"model":"glm-5.3","input":[{"type":"compaction_trigger"}],"stream":true}`)
	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "glm-5.3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, Alt: "responses/compact", Stream: true})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	var frames strings.Builder
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("chunk error = %v", chunk.Err)
		}
		frames.Write(chunk.Payload)
	}
	out := frames.String()
	for _, event := range []string{"response.created", "response.output_item.done", "response.completed"} {
		if !strings.Contains(out, "event: "+event) {
			t.Fatalf("missing SSE event %s in:\n%s", event, out)
		}
	}
	if !strings.Contains(out, "\"type\":\"compaction\"") {
		t.Fatalf("compaction item missing in:\n%s", out)
	}
}

func TestCodeBuddyExecutorCompactionCapsuleExpansion(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	capsule, errSeal := helps.SealAntigravityCompaction("Previous context: discussed weather APIs.", "glm-5.3")
	if errSeal != nil {
		t.Fatalf("seal: %v", errSeal)
	}

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	payload := []byte(`{"model":"glm-5.3","input":[
		{"type":"compaction","encrypted_content":"` + capsule + `"},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
	]}`)
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "glm-5.3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	bodyStr := string(gotBody)
	if !strings.Contains(bodyStr, "discussed weather APIs") {
		t.Fatalf("capsule summary not expanded into upstream body: %s", bodyStr)
	}
	if strings.Contains(bodyStr, "encrypted_content") {
		t.Fatalf("raw capsule must not reach upstream: %s", bodyStr)
	}
}

func TestCodeBuddyExecutorCompactionToolChoiceNone(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"summary\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewCodeBuddyExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "token"},
	}
	payload := []byte(`{"model":"glm-5.3","tools":[{"type":"function","name":"get_weather","description":"w","parameters":{"type":"object","properties":{}}}],"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"weather?"}]},
		{"type":"compaction_trigger"}
	]}`)
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "glm-5.3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, Alt: "responses/compact"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := gjson.GetBytes(gotBody, "tool_choice").String(); got != "none" {
		t.Fatalf("tool_choice = %q, body=%s", got, string(gotBody))
	}
	if !gjson.GetBytes(gotBody, "tools").Exists() {
		t.Fatalf("tools must be preserved for structured history: %s", string(gotBody))
	}
}
