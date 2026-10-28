package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	codebuddyauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codebuddy"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var _ cliproxyauth.ProviderExecutor = (*CodeBuddyExecutor)(nil)

// CodeBuddyExecutor executes chat requests against the CodeBuddy OpenAI-compatible API.
type CodeBuddyExecutor struct {
	cfg *config.Config
}

// NewCodeBuddyExecutor constructs a new CodeBuddy executor.
func NewCodeBuddyExecutor(cfg *config.Config) *CodeBuddyExecutor {
	return &CodeBuddyExecutor{cfg: cfg}
}

// Identifier returns the provider identifier "codebuddy".
func (e *CodeBuddyExecutor) Identifier() string { return "codebuddy" }

// PrepareRequest injects CodeBuddy credentials into a native HTTP request.
func (e *CodeBuddyExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	token := codeBuddyAccessToken(auth)
	if token == "" {
		token = codeBuddyAPIKey(auth)
	}
	codebuddyauth.ApplyChatHeaders(req, token, false)
	return nil
}

// HttpRequest injects CodeBuddy credentials into a native request and executes it.
func (e *CodeBuddyExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("codebuddy executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// Execute performs a non-streaming chat completion.
// CodeBuddy upstream rejects non-stream requests, so the executor always
// requests a stream upstream and aggregates SSE frames into a single response.
func (e *CodeBuddyExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	if errExpand := expandClaudeResponsesCompaction(&req, &opts); errExpand != nil {
		return resp, errExpand
	}
	if codeBuddyCompactionRequested(req, opts) {
		return e.executeCodeBuddyCompaction(ctx, auth, req, opts)
	}
	ctx = helps.EnsureSessionContext(ctx, opts, req.Payload)
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)

	baseURL, token, errCreds := e.resolveCredentials(auth)
	if errCreds != nil {
		err = errCreds
		return
	}

	from := opts.SourceFormat
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := sdktranslator.FormatOpenAI
	originalPayload := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayload = opts.OriginalRequest
	}
	// Upstream only supports streaming; always translate as a stream request.
	originalTranslated, translated, updatesChanged, err := helps.TranslateRequestPairWithAPIKeyModelCompatibilityAndUpdateIntent(
		ctx, opts.Headers, e.cfg, from, to, baseModel, originalPayload, req.Payload, true, false,
	)
	if err != nil {
		return resp, err
	}
	translated, err = helps.ApplyRequestThinking(translated, req, opts, from.String(), to.String(), e.Identifier(), updatesChanged)
	if err != nil {
		return resp, err
	}
	requestedModel := helps.PayloadRequestedModel(opts, req.Model)
	requestPath := helps.PayloadRequestPath(opts)
	translated = helps.StripCodeBuddyClaudeCodeIdentity(translated)
	translated = helps.NormalizeOpenAIToolResultsTextOnly(translated)
	translated = helps.NormalizeOpenAIMaxTokens(translated, false)
	translated, err = e.applyPromptCacheKey(ctx, from, baseModel, req, opts, translated)
	if err != nil {
		return resp, err
	}
	reporter.SetTranslatedReasoningEffort(translated, to.String())
	translated = helps.SetBoolIfDifferent(translated, "stream", true)
	translated = helps.SetBoolIfDifferent(translated, "stream_options.include_usage", true)
	if codeBuddyCompactionSummaryFromContext(ctx) {
		translated = finalizeCodeBuddyCompactionSummaryBody(translated)
	}
	translated = helps.ApplyPayloadConfigWithRequest(e.cfg, baseModel, to.String(), from.String(), "", translated, originalTranslated, requestedModel, requestPath, opts.Headers)

	httpReq, errNew := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+codebuddyauth.ChatPath, bytes.NewReader(translated))
	if errNew != nil {
		err = fmt.Errorf("codebuddy executor: create request failed: %w", errNew)
		return resp, err
	}
	codebuddyauth.ApplyChatHeaders(httpReq, token, true)
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(httpReq, attrs, opts.Headers)
	helps.RecordAPIRequest(ctx, e.cfg, helps.UpstreamRequestLog{
		URL:       httpReq.URL.String(),
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      translated,
		Provider:  e.Identifier(),
		AuthID:    authID(auth),
		AuthLabel: authLabel(auth),
		AuthType:  "oauth",
		AuthValue: token,
	})

	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpClient = reporter.TrackHTTPClient(httpClient)
	httpResp, errDo := httpClient.Do(httpReq)
	if errDo != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, errDo)
		err = errDo
		return resp, err
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("codebuddy executor: close response body error: %v", errClose)
		}
	}()
	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		helps.AppendAPIResponseChunk(ctx, e.cfg, body)
		helps.LogWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, helps.SummarizeErrorBody(httpResp.Header.Get("Content-Type"), body))
		err = newOpenAICompatStatusError(httpResp.StatusCode, httpResp.Header, body)
		return resp, err
	}

	// Aggregate upstream SSE frames into a single chat completion response.
	body, usageDetail, hasUsage, errAgg := aggregateCodeBuddyStream(httpResp.Body)
	if errAgg != nil {
		if ctx.Err() != nil {
			return resp, ctx.Err()
		}
		helps.RecordAPIResponseError(ctx, e.cfg, errAgg)
		if hasUsage {
			reporter.PublishFailureWithDetail(ctx, usageDetail, errAgg)
		}
		err = errAgg
		return resp, err
	}
	helps.AppendAPIResponseChunk(ctx, e.cfg, body)
	reporter.ObserveResponseModel(body)
	if hasUsage {
		reporter.Publish(ctx, usageDetail)
	} else {
		reporter.Publish(ctx, helps.ParseOpenAIUsage(body))
	}
	reporter.EnsurePublished(ctx)
	var param any
	out := sdktranslator.TranslateNonStream(ctx, to, responseFormat, req.Model, helps.ApplyPatchOriginalRequest(req, opts), translated, body, &param)
	if helps.ApplyPatchTranslationError(param) != nil || len(out) == 0 {
		err = statusErr{code: http.StatusBadGateway, msg: helps.ApplyPatchUpstreamErrorMessage}
		return resp, err
	}
	if responseFormat == sdktranslator.FormatOpenAIResponse {
		out = helps.EnsureResponsesUsageDetails(out)
	}
	resp = cliproxyexecutor.Response{Payload: out, Headers: httpResp.Header.Clone()}
	return resp, nil
}

// aggregateCodeBuddyStream consumes an OpenAI-compatible SSE stream and folds
// the chunks into a single non-stream chat completion payload. Reasoning
// deltas (reasoning_content) are preserved on the first choice message.
func aggregateCodeBuddyStream(r io.Reader) ([]byte, usage.Detail, bool, error) {
	var zeroUsageDetail usage.Detail
	reader := helps.NewCodeBuddySSEReader(r)

	var (
		chatID      string
		created     int64
		model       string
		content     strings.Builder
		reasoning   strings.Builder
		finishCause string
		usageRaw    []byte
		usage       usage.Detail
		hasUsage    bool
		sawChoice   bool
		sawDone     bool
		toolCalls   = map[int]map[string]any{}
		toolArgBuf  = map[int]*strings.Builder{}
	)

	for {
		frame, errRead := reader.ReadFrame()
		if errRead == io.EOF {
			break
		}
		if errRead != nil {
			return nil, usage, hasUsage, statusErr{code: http.StatusBadGateway, msg: "codebuddy executor: read upstream stream failed: " + errRead.Error(), streamTruncated: true}
		}
		data := frame.Data
		if len(data) == 0 && !openAICompatErrorEvent(frame.Event) {
			continue
		}
		if streamErr, isError := openAICompatStreamDataError(data, frame.Event); isError {
			return nil, usage, hasUsage, streamErr
		}
		if openAICompatErrorEvent(frame.Event) {
			return nil, usage, hasUsage, statusErr{code: http.StatusBadGateway, msg: "upstream error event returned invalid data"}
		}
		if frame.BareJSON || (!bytes.Equal(data, []byte("[DONE]")) && !json.Valid(data)) {
			return nil, usage, hasUsage, statusErr{code: http.StatusBadGateway, msg: "upstream stream ended with incomplete SSE data frame", streamTruncated: true}
		}
		if bytes.Equal(data, []byte("[DONE]")) {
			sawDone = true
			break
		}
		if id := gjson.GetBytes(data, "id"); id.Exists() && chatID == "" {
			chatID = id.String()
		}
		if c := gjson.GetBytes(data, "created"); c.Exists() && created == 0 {
			created = c.Int()
		}
		if m := gjson.GetBytes(data, "model"); m.Exists() && model == "" {
			model = m.String()
		}
		if u := gjson.GetBytes(data, "usage"); u.Exists() && u.IsObject() {
			usageRaw = append([]byte(nil), data...)
			usage = helps.ParseOpenAIUsage(data)
			hasUsage = true
		}
		choices := gjson.GetBytes(data, "choices")
		if !choices.Exists() || !choices.IsArray() {
			continue
		}
		for _, choice := range choices.Array() {
			sawChoice = true
			delta := choice.Get("delta")
			if rc := delta.Get("reasoning_content"); rc.Exists() {
				reasoning.WriteString(rc.String())
			}
			if c := delta.Get("content"); c.Exists() {
				content.WriteString(c.String())
			}
			for _, tc := range delta.Get("tool_calls").Array() {
				idx := int(tc.Get("index").Int())
				entry, ok := toolCalls[idx]
				if !ok {
					entry = map[string]any{"type": "function"}
					toolCalls[idx] = entry
					toolArgBuf[idx] = &strings.Builder{}
				}
				if id := tc.Get("id"); id.Exists() && id.String() != "" {
					entry["id"] = id.String()
				}
				fn := tc.Get("function")
				if name := fn.Get("name"); name.Exists() && name.String() != "" {
					entry["function"] = map[string]any{"name": name.String()}
				}
				if args := fn.Get("arguments"); args.Exists() {
					toolArgBuf[idx].WriteString(args.String())
				}
			}
			if fr := choice.Get("finish_reason"); fr.Exists() && fr.String() != "" {
				finishCause = fr.String()
			}
		}
	}
	if !sawChoice || (!sawDone && finishCause == "") {
		return nil, usage, hasUsage, statusErr{code: http.StatusBadGateway, msg: "upstream stream closed before completion", streamTruncated: true}
	}

	message := map[string]any{"role": "assistant", "content": content.String()}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolCalls) > 0 {
		ordered := make([]any, 0, len(toolCalls))
		indices := make([]int, 0, len(toolCalls))
		for index := range toolCalls {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		for _, i := range indices {
			entry := toolCalls[i]
			if fn, ok := entry["function"].(map[string]any); ok {
				fn["arguments"] = toolArgBuf[i].String()
			}
			ordered = append(ordered, entry)
		}
		message["tool_calls"] = ordered
	}
	if finishCause == "" {
		finishCause = "stop"
	}

	result := map[string]any{
		"object":  "chat.completion",
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finishCause}},
	}
	if chatID != "" {
		result["id"] = chatID
	}
	if created != 0 {
		result["created"] = created
	}
	if model != "" {
		result["model"] = model
	}
	if usageRaw != nil {
		usageNode := gjson.GetBytes(usageRaw, "usage")
		usageJSON := []byte(usageNode.Raw)
		// Mirror Responses-style aliases so downstream consumers (compaction,
		// usage accounting) can read either OpenAI Chat or Responses field names.
		if pt := usageNode.Get("prompt_tokens"); pt.Exists() {
			usageJSON, _ = sjson.SetBytes(usageJSON, "input_tokens", pt.Int())
		}
		if ct := usageNode.Get("completion_tokens"); ct.Exists() {
			usageJSON, _ = sjson.SetBytes(usageJSON, "output_tokens", ct.Int())
		}
		result["usage"] = json.RawMessage(usageJSON)
	}

	out, errMarshal := json.Marshal(result)
	if errMarshal != nil {
		return nil, zeroUsageDetail, false, fmt.Errorf("codebuddy executor: marshal aggregated response failed: %w", errMarshal)
	}
	return out, usage, hasUsage, nil
}

// ExecuteStream performs a streaming chat completion and translates OpenAI SSE chunks.
func (e *CodeBuddyExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	if errExpand := expandClaudeResponsesCompaction(&req, &opts); errExpand != nil {
		return nil, errExpand
	}
	if codeBuddyCompactionRequested(req, opts) {
		return e.executeCodeBuddyCompactionStream(ctx, auth, req, opts)
	}
	ctx = helps.EnsureSessionContext(ctx, opts, req.Payload)
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)

	baseURL, token, errCreds := e.resolveCredentials(auth)
	if errCreds != nil {
		return nil, errCreds
	}

	from := opts.SourceFormat
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := sdktranslator.FormatOpenAI
	originalPayload := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayload = opts.OriginalRequest
	}
	originalTranslated, translated, updatesChanged, err := helps.TranslateRequestPairWithAPIKeyModelCompatibilityAndUpdateIntent(
		ctx, opts.Headers, e.cfg, from, to, baseModel, originalPayload, req.Payload, true, false,
	)
	if err != nil {
		return nil, err
	}
	translated, err = helps.ApplyRequestThinking(translated, req, opts, from.String(), to.String(), e.Identifier(), updatesChanged)
	if err != nil {
		return nil, err
	}
	requestedModel := helps.PayloadRequestedModel(opts, req.Model)
	requestPath := helps.PayloadRequestPath(opts)
	translated = helps.StripCodeBuddyClaudeCodeIdentity(translated)
	translated = helps.NormalizeOpenAIToolResultsTextOnly(translated)
	translated = helps.NormalizeOpenAIMaxTokens(translated, false)
	translated, err = e.applyPromptCacheKey(ctx, from, baseModel, req, opts, translated)
	if err != nil {
		return nil, err
	}
	translated = helps.SetBoolIfDifferent(translated, "stream_options.include_usage", true)
	reporter.SetTranslatedReasoningEffort(translated, to.String())
	translated = helps.ApplyPayloadConfigWithRequest(e.cfg, baseModel, to.String(), from.String(), "", translated, originalTranslated, requestedModel, requestPath, opts.Headers)

	httpReq, errNew := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+codebuddyauth.ChatPath, bytes.NewReader(translated))
	if errNew != nil {
		return nil, fmt.Errorf("codebuddy executor: create request failed: %w", errNew)
	}
	codebuddyauth.ApplyChatHeaders(httpReq, token, true)
	var streamAttrs map[string]string
	if auth != nil {
		streamAttrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(httpReq, streamAttrs, opts.Headers)
	httpReq.Header.Set("Cache-Control", "no-cache")
	helps.RecordAPIRequest(ctx, e.cfg, helps.UpstreamRequestLog{
		URL:       httpReq.URL.String(),
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      translated,
		Provider:  e.Identifier(),
		AuthID:    authID(auth),
		AuthLabel: authLabel(auth),
		AuthType:  "oauth",
		AuthValue: token,
	})

	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	// Early role/empty frames are only first-packet fallbacks, not effective tokens.
	httpClient = reporter.TrackHTTPClientRoundTripOnly(httpClient)
	httpResp, errDo := httpClient.Do(httpReq)
	if errDo != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, errDo)
		return nil, errDo
	}
	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		helps.AppendAPIResponseChunk(ctx, e.cfg, body)
		helps.LogWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, helps.SummarizeErrorBody(httpResp.Header.Get("Content-Type"), body))
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("codebuddy executor: close response body error: %v", errClose)
		}
		return nil, newOpenAICompatStatusError(httpResp.StatusCode, httpResp.Header, body)
	}

	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("codebuddy executor: close response body error: %v", errClose)
			}
		}()
		reader := helps.NewCodeBuddySSEReader(httpResp.Body)
		claudeInputTokens := helps.NewClaudeInputTokenState(from, to, responseFormat, originalPayload)
		var param any
		helps.InitializeApplyPatchStream(ctx, to, responseFormat, req.Model, helps.ApplyPatchOriginalRequest(req, opts), translated, &param)
		var streamUsage helps.StreamUsageBuffer
		var seenDone bool
		var streamFailed bool
		var streamAborted bool
		var seenChoice bool
		var seenFinish bool
		defer streamUsage.Publish(ctx, reporter)

		publishStreamError := func(streamErr statusErr, containsPayload bool) {
			loggedErr := streamErr
			if containsPayload {
				loggedErr.msg = "upstream stream returned an error payload"
			}
			helps.RecordAPIResponseError(ctx, e.cfg, loggedErr)
			streamUsage.PublishFailure(ctx, reporter, loggedErr)
			select {
			case out <- cliproxyexecutor.StreamChunk{Err: streamErr}:
			case <-ctx.Done():
			}
			streamFailed = true
		}

		processFrame := func(frame helps.CodeBuddySSEFrame) bool {
			eventName := frame.Event
			dataPayload := frame.Data
			if len(dataPayload) == 0 {
				if openAICompatErrorEvent(eventName) {
					publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream error event ended without data"}, false)
					return true
				}
				return false
			}
			isDone := bytes.Equal(dataPayload, []byte("[DONE]"))
			if openAICompatErrorEvent(eventName) && !json.Valid(dataPayload) {
				publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream error event returned invalid data"}, false)
				return true
			}
			if !isDone && !json.Valid(dataPayload) {
				publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream stream ended with incomplete SSE data frame", streamTruncated: true}, false)
				return true
			}
			if !isDone {
				if streamErr, isError := openAICompatStreamDataError(dataPayload, eventName); isError {
					publishStreamError(streamErr, true)
					return true
				}
				if frame.BareJSON {
					publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream returned an unframed response", streamTruncated: true}, false)
					return true
				}
				if len(gjson.GetBytes(dataPayload, "choices").Array()) > 0 {
					seenChoice = true
				}
				seenFinish = seenFinish || openAICompatChunkHasFinishReason(dataPayload)
			} else if frame.BareJSON || !seenChoice {
				publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream stream ended without response choices", streamTruncated: true}, false)
				return true
			}
			// Observe complete upstream frames before translation or downstream backpressure.
			reporter.ObserveResponseModel(dataPayload)
			streamUsage.ObserveOpenAIStream(dataPayload)
			helps.ObserveChatTokenEvent(reporter, dataPayload)
			streamLine := append([]byte("data: "), dataPayload...)
			chunks := helps.TranslateStreamWithClaudeInputTokens(ctx, to, responseFormat, req.Model, helps.ApplyPatchOriginalRequest(req, opts), translated, streamLine, &param, claudeInputTokens)
			helps.RecordApplyPatchStreamFailure(ctx, param, reporter, statusErr{code: http.StatusBadGateway, msg: helps.ApplyPatchUpstreamErrorMessage})
			for i := range chunks {
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: chunks[i]}:
				case <-ctx.Done():
					streamAborted = true
					return true
				}
			}
			if helps.ApplyPatchTranslationError(param) != nil {
				publishStreamError(statusErr{code: http.StatusBadGateway, msg: helps.ApplyPatchUpstreamErrorMessage}, false)
				return true
			}
			if isDone {
				seenDone = true
				return true
			}
			return false
		}

		var errScan error
		for {
			frame, errRead := reader.ReadFrame()
			if errRead == io.EOF {
				break
			}
			if errRead != nil {
				errScan = errRead
				break
			}
			helps.AppendAPIResponseChunk(ctx, e.cfg, frame.Data)
			if processFrame(frame) {
				break
			}
		}
		if ctx.Err() != nil {
			streamUsage.PublishFailure(ctx, reporter, ctx.Err())
			return
		}
		if !streamFailed && !streamAborted && helps.EndApplyPatchStream(ctx, param, reporter, out, statusErr{code: http.StatusBadGateway, msg: helps.ApplyPatchUpstreamErrorMessage, streamTruncated: true}) {
			return
		}
		if !streamFailed && !streamAborted && !seenDone && errScan == nil && responseFormat == sdktranslator.FormatOpenAIResponse && ctx.Err() == nil && helps.CanFinalizeResponseStream(param) {
			finalChunks := helps.TranslateStreamWithClaudeInputTokens(ctx, to, responseFormat, req.Model, helps.ApplyPatchOriginalRequest(req, opts), translated, []byte("data: [DONE]"), &param, claudeInputTokens)
			for _, finalChunk := range finalChunks {
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: finalChunk}:
				case <-ctx.Done():
					streamAborted = true
				}
				if streamAborted {
					break
				}
			}
			if len(finalChunks) > 0 && !streamAborted {
				seenDone = true
			}
		}
		if streamFailed || streamAborted {
			return
		}
		if errScan != nil {
			publishStreamError(statusErr{code: http.StatusBadGateway, msg: "codebuddy executor: read upstream stream failed: " + errScan.Error(), streamTruncated: true}, false)
			return
		}
		if !seenDone {
			if responseFormat == sdktranslator.FormatOpenAIResponse || !seenFinish || !seenChoice {
				publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream stream closed before completion", streamTruncated: true}, false)
				return
			}
			chunks := helps.TranslateStreamWithClaudeInputTokens(ctx, to, responseFormat, req.Model, helps.ApplyPatchOriginalRequest(req, opts), translated, []byte("data: [DONE]"), &param, claudeInputTokens)
			helps.RecordApplyPatchStreamFailure(ctx, param, reporter, statusErr{code: http.StatusBadGateway, msg: helps.ApplyPatchUpstreamErrorMessage})
			for i := range chunks {
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: chunks[i]}:
				case <-ctx.Done():
					return
				}
			}
		}
		streamUsage.Publish(ctx, reporter)
		reporter.EnsurePublished(ctx)
	}()
	return &cliproxyexecutor.StreamResult{Headers: httpResp.Header.Clone(), Chunks: out}, nil
}

// CountTokens counts tokens locally using the OpenAI-compatible tokenizer;
// CodeBuddy has no dedicated token counting endpoint.
func (e *CodeBuddyExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	from := opts.SourceFormat
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := sdktranslator.FormatOpenAI
	_, translated, updatesChanged, err := helps.TranslateRequestPairWithAPIKeyModelCompatibilityAndUpdateIntent(
		ctx, opts.Headers, e.cfg, from, to, baseModel, req.Payload, req.Payload, false, false,
	)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	originalTranslated := translated
	if len(opts.OriginalRequest) > 0 {
		originalTranslated, _, _, err = helps.TranslateRequestPairWithAPIKeyModelCompatibilityAndUpdateIntent(
			ctx, opts.Headers, e.cfg, from, to, baseModel, opts.OriginalRequest, opts.OriginalRequest, false, false,
		)
		if err != nil {
			return cliproxyexecutor.Response{}, err
		}
	}

	translated, err = helps.ApplyRequestThinking(translated, req, opts, from.String(), to.String(), e.Identifier(), updatesChanged)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}

	translated = helps.StripCodeBuddyClaudeCodeIdentity(translated)
	translated = helps.NewPayloadFinalizer(e.cfg, e.Identifier(), baseModel, to.String(), "", originalTranslated, req, opts)(translated)
	enc, err := helps.TokenizerForModel(baseModel)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("codebuddy executor: tokenizer init failed: %w", err)
	}

	count, err := helps.CountOpenAIChatTokens(enc, translated)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("codebuddy executor: token counting failed: %w", err)
	}

	usageJSON := helps.BuildOpenAIUsageJSON(count)
	translatedUsage := sdktranslator.TranslateTokenCount(ctx, to, responseFormat, count, usageJSON)
	return cliproxyexecutor.Response{Payload: translatedUsage}, nil
}

// SupportsApplyPatch reports the actual executor contract, independent of its provider name.
func (e *CodeBuddyExecutor) SupportsApplyPatch() bool { return e != nil }

// applyPromptCacheKey injects a prompt_cache_key for CodeBuddy upstream caching.
// CodeBuddy natively supports prompt cache statistics in usage, so caching is
// always enabled without an OpenAICompatibility gate.
func (e *CodeBuddyExecutor) applyPromptCacheKey(ctx context.Context, from sdktranslator.Format, baseModel string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, translated []byte) ([]byte, error) {
	for _, payload := range [][]byte{req.Payload, opts.OriginalRequest, translated} {
		if promptCacheKey := strings.TrimSpace(gjson.GetBytes(payload, "prompt_cache_key").String()); promptCacheKey != "" {
			return helps.SetStringIfDifferent(translated, "prompt_cache_key", promptCacheKey), nil
		}
	}

	modelName := strings.TrimSpace(gjson.GetBytes(translated, "model").String())
	if modelName == "" {
		modelName = baseModel
	}
	if sourceFormatEqual(from, sdktranslator.FormatClaude) {
		cached, ok, errCache := helps.ClaudeCodePromptCache(ctx, modelName, req.Payload, opts.Headers)
		if errCache != nil {
			return translated, errCache
		}
		if ok {
			return helps.SetStringIfDifferent(translated, "prompt_cache_key", cached.ID), nil
		}
	}

	sessionID := helps.ProviderSessionUUID(e.Identifier(), opts.Metadata, req.Metadata)
	if sessionID == "" {
		return translated, nil
	}
	identity := strings.Join([]string{
		"cli-proxy-api:codebuddy:prompt-cache",
		strings.ToLower(modelName),
		strings.ToLower(strings.TrimSpace(from.String())),
		sessionID,
	}, "\x00")
	promptCacheKey := uuid.NewSHA1(uuid.NameSpaceOID, []byte(identity)).String()
	return helps.SetStringIfDifferent(translated, "prompt_cache_key", promptCacheKey), nil
}

// Refresh rotates CodeBuddy access credentials and updates the runtime auth metadata.
func (e *CodeBuddyExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	log.Debugf("codebuddy executor: refresh called")
	if refreshed, handled, err := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		return refreshed, err
	}
	if auth == nil {
		return nil, fmt.Errorf("codebuddy executor: auth is nil")
	}
	refreshToken := codeBuddyRefreshToken(auth)
	if refreshToken == "" {
		return auth, nil
	}
	baseURL := codeBuddyBaseURL(auth)
	client := codebuddyauth.NewClientWithBaseURL(e.cfg, baseURL)
	if httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0); httpClient != nil {
		client.SetHTTPClient(httpClient)
	}
	token, err := client.RefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["access_token"] = token.AccessToken
	if token.RefreshToken != "" {
		auth.Metadata["refresh_token"] = token.RefreshToken
	}
	if token.ExpiresIn > 0 {
		auth.Metadata["expires_in"] = token.ExpiresIn
	}
	if interval := codebuddyauth.RefreshInterval(token); interval > 0 {
		auth.Metadata["refresh_interval_seconds"] = int(interval / time.Second)
	}
	if expiredAt := codebuddyauth.TokenExpiry(token, time.Now()); !expiredAt.IsZero() {
		expired := codebuddyauth.FormatExpires(expiredAt)
		auth.Metadata["expired"] = expired
	}
	if _, ok := auth.Metadata["base_url"]; !ok {
		auth.Metadata["base_url"] = baseURL
	}
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	if auth.Attributes["base_url"] == "" {
		auth.Attributes["base_url"] = baseURL
	}
	if storage, ok := auth.Storage.(*codebuddyauth.TokenStorage); ok && storage != nil {
		newStorage := *storage
		newStorage.AccessToken = token.AccessToken
		if token.RefreshToken != "" {
			newStorage.RefreshToken = token.RefreshToken
		}
		if expiredAt := codebuddyauth.TokenExpiry(token, time.Now()); !expiredAt.IsZero() {
			newStorage.Expired = codebuddyauth.FormatExpires(expiredAt)
		}
		if newStorage.BaseURL == "" {
			newStorage.BaseURL = baseURL
		}
		auth.Storage = &newStorage
	}
	auth.Metadata["last_refresh"] = time.Now().Format(time.RFC3339)
	return auth, nil
}

func (e *CodeBuddyExecutor) resolveCredentials(auth *cliproxyauth.Auth) (string, string, error) {
	baseURL := codeBuddyBaseURL(auth)
	token := codeBuddyAccessToken(auth)
	if token == "" {
		token = codeBuddyAPIKey(auth)
	}
	if baseURL == "" {
		return "", "", statusErr{code: http.StatusUnauthorized, msg: "missing codebuddy baseURL"}
	}
	if token == "" {
		return "", "", statusErr{code: http.StatusUnauthorized, msg: "missing codebuddy access token"}
	}
	return baseURL, token, nil
}

func codeBuddyBaseURL(auth *cliproxyauth.Auth) string {
	if auth != nil && auth.Attributes != nil {
		if baseURL := strings.TrimSpace(auth.Attributes["base_url"]); baseURL != "" {
			return strings.TrimRight(baseURL, "/")
		}
	}
	return codebuddyauth.DefaultBaseURL
}

func codeBuddyAccessToken(auth *cliproxyauth.Auth) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	token, _ := auth.Metadata["access_token"].(string)
	return strings.TrimSpace(token)
}

func codeBuddyRefreshToken(auth *cliproxyauth.Auth) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	token, _ := auth.Metadata["refresh_token"].(string)
	return strings.TrimSpace(token)
}

func codeBuddyAPIKey(auth *cliproxyauth.Auth) string {
	if auth == nil || auth.Attributes == nil {
		return ""
	}
	return strings.TrimSpace(auth.Attributes["api_key"])
}

func authID(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	return auth.ID
}

func authLabel(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	return auth.Label
}
