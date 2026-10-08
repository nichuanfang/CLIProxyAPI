package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	codebuddyauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codebuddy"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	log "github.com/sirupsen/logrus"
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
func (e *CodeBuddyExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
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
	_, translated, updatesChanged, err := helps.TranslateRequestPairWithAPIKeyModelCompatibilityAndUpdateIntent(
		ctx, opts.Headers, e.cfg, from, to, baseModel, originalPayload, req.Payload, opts.Stream, false,
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
	translated = helps.ApplyPayloadConfigWithRequest(e.cfg, baseModel, to.String(), from.String(), "", translated, translated, requestedModel, requestPath, opts.Headers)
	translated = helps.NormalizeOpenAIMaxTokens(translated, false)
	reporter.SetTranslatedReasoningEffort(translated, to.String())

	httpReq, errNew := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+codebuddyauth.ChatPath, bytes.NewReader(translated))
	if errNew != nil {
		err = fmt.Errorf("codebuddy executor: create request failed: %w", errNew)
		return resp, err
	}
	codebuddyauth.ApplyChatHeaders(httpReq, token, false)
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
	body, errRead := io.ReadAll(httpResp.Body)
	if errRead != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, errRead)
		err = errRead
		return resp, err
	}
	helps.AppendAPIResponseChunk(ctx, e.cfg, body)
	reporter.ObserveResponseModel(body)
	reporter.Publish(ctx, helps.ParseOpenAIUsage(body))
	reporter.EnsurePublished(ctx)
	var param any
	out := sdktranslator.TranslateNonStream(ctx, to, responseFormat, req.Model, opts.OriginalRequest, translated, body, &param)
	if responseFormat == sdktranslator.FormatOpenAIResponse {
		out = helps.EnsureResponsesUsageDetails(out)
	}
	resp = cliproxyexecutor.Response{Payload: out, Headers: httpResp.Header.Clone()}
	return resp, nil
}

// ExecuteStream performs a streaming chat completion and translates OpenAI SSE chunks.
func (e *CodeBuddyExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
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
	_, translated, updatesChanged, err := helps.TranslateRequestPairWithAPIKeyModelCompatibilityAndUpdateIntent(
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
	translated = helps.ApplyPayloadConfigWithRequest(e.cfg, baseModel, to.String(), from.String(), "", translated, translated, requestedModel, requestPath, opts.Headers)
	translated = helps.NormalizeOpenAIMaxTokens(translated, false)
	translated = helps.SetBoolIfDifferent(translated, "stream_options.include_usage", true)
	reporter.SetTranslatedReasoningEffort(translated, to.String())

	httpReq, errNew := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+codebuddyauth.ChatPath, bytes.NewReader(translated))
	if errNew != nil {
		return nil, fmt.Errorf("codebuddy executor: create request failed: %w", errNew)
	}
	codebuddyauth.ApplyChatHeaders(httpReq, token, true)
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
		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 52_428_800) // 50MB
		claudeInputTokens := helps.NewClaudeInputTokenState(from, to, responseFormat, originalPayload)
		var param any
		var streamUsage helps.StreamUsageBuffer
		var seenDone bool
		var streamFailed bool
		var streamAborted bool
		var upstreamEvent string
		var frameData [][]byte
		defer streamUsage.Publish(ctx, reporter)

		publishStreamError := func(streamErr statusErr, containsPayload bool) {
			loggedErr := streamErr
			if containsPayload {
				loggedErr = statusErr{code: streamErr.code, msg: "upstream stream returned an error payload"}
			}
			helps.RecordAPIResponseError(ctx, e.cfg, loggedErr)
			reporter.PublishFailure(ctx, loggedErr)
			select {
			case out <- cliproxyexecutor.StreamChunk{Err: streamErr}:
			case <-ctx.Done():
			}
			streamFailed = true
		}

		processFrame := func() bool {
			eventName := upstreamEvent
			upstreamEvent = ""
			dataLines := frameData
			frameData = nil
			if len(dataLines) == 0 {
				if openAICompatErrorEvent(eventName) {
					publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream error event ended without data"}, false)
					return true
				}
				return false
			}
			if len(dataLines) > 1 {
				for _, dataLine := range dataLines {
					if bytes.Equal(bytes.TrimSpace(dataLine), []byte("[DONE]")) {
						publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream stream ended with incomplete data before [DONE]"}, false)
						return true
					}
				}
			}
			dataPayload := bytes.TrimSpace(bytes.Join(dataLines, []byte("\n")))
			isDone := bytes.Equal(dataPayload, []byte("[DONE]"))
			log.Debugf("codebuddy executor: frame data=%q", string(dataPayload))
			if !isDone && !json.Valid(dataPayload) {
				publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream stream ended with incomplete SSE data frame"}, false)
				return true
			}
			if !isDone {
				if streamErr, isError := openAICompatStreamDataError(dataPayload, eventName); isError {
					publishStreamError(streamErr, true)
					return true
				}
			}
			streamLine := append([]byte("data: "), dataPayload...)
			chunks := helps.TranslateStreamWithClaudeInputTokens(ctx, to, responseFormat, req.Model, opts.OriginalRequest, translated, streamLine, &param, claudeInputTokens)
			for i := range chunks {
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: chunks[i]}:
				case <-ctx.Done():
					streamAborted = true
					return true
				}
			}
			if isDone {
				seenDone = true
				return true
			}
			return false
		}

	scanLoop:
		for scanner.Scan() {
			line := scanner.Bytes()
			helps.AppendAPIResponseChunk(ctx, e.cfg, line)
			reporter.ObserveResponseModel(line)
			streamUsage.ObserveOpenAIStream(line)
			trimmedLine := bytes.TrimSpace(line)
			if len(trimmedLine) == 0 {
				if processFrame() {
					break scanLoop
				}
				continue
			}
			if bytes.HasPrefix(trimmedLine, []byte("data:")) {
				frameData = append(frameData, bytes.Clone(bytes.TrimSpace(trimmedLine[len("data:"):])))
				continue
			}
			if bytes.HasPrefix(trimmedLine, []byte("event:")) {
				upstreamEvent = strings.TrimSpace(string(trimmedLine[len("event:"):]))
				continue
			}
			if bytes.HasPrefix(trimmedLine, []byte(":")) || bytes.HasPrefix(trimmedLine, []byte("id:")) || bytes.HasPrefix(trimmedLine, []byte("retry:")) {
				continue
			}
			if bytes.HasPrefix(trimmedLine, []byte("{")) || bytes.HasPrefix(trimmedLine, []byte("[")) {
				publishStreamError(statusErr{code: http.StatusBadGateway, msg: string(trimmedLine)}, true)
				break
			}
		}
		errScan := scanner.Err()
		if errScan == nil && !seenDone && !streamFailed && !streamAborted && len(frameData) > 0 {
			_ = processFrame()
		}
		if streamFailed || streamAborted {
			return
		}
		if errScan != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, errScan)
			reporter.PublishFailure(ctx, errScan)
			select {
			case out <- cliproxyexecutor.StreamChunk{Err: errScan}:
			case <-ctx.Done():
			}
			return
		}
		if !seenDone {
			if responseFormat == sdktranslator.FormatOpenAIResponse {
				streamErr := statusErr{code: http.StatusBadGateway, msg: "upstream stream closed before [DONE]"}
				helps.RecordAPIResponseError(ctx, e.cfg, streamErr)
				reporter.PublishFailure(ctx, streamErr)
				select {
				case out <- cliproxyexecutor.StreamChunk{Err: streamErr}:
				case <-ctx.Done():
				}
				return
			}
			chunks := helps.TranslateStreamWithClaudeInputTokens(ctx, to, responseFormat, req.Model, opts.OriginalRequest, translated, []byte("data: [DONE]"), &param, claudeInputTokens)
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

// CountTokens is intentionally unsupported; CodeBuddy has no dedicated token counting endpoint.
func (e *CodeBuddyExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, fmt.Errorf("codebuddy executor: token counting is not supported")
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
