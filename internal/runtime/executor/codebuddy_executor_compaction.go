package executor

import (
	"context"
	"fmt"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type codeBuddyCompactionSummaryContextKey struct{}

func withCodeBuddyCompactionSummary(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, codeBuddyCompactionSummaryContextKey{}, true)
}

func codeBuddyCompactionSummaryFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	flag, ok := ctx.Value(codeBuddyCompactionSummaryContextKey{}).(bool)
	return ok && flag
}

// finalizeCodeBuddyCompactionSummaryBody stops the summary turn from calling
// tools: tool_choice none keeps structured tool history valid upstream.
func finalizeCodeBuddyCompactionSummaryBody(body []byte) []byte {
	tools := gjson.GetBytes(body, "tools")
	if tools.IsArray() && len(tools.Array()) > 0 {
		out, _ := sjson.SetBytes(body, "tool_choice", "none")
		return out
	}
	return body
}

// codeBuddyCompactionRequested reports whether the request is a Codex
// responses/compact call or carries an inline compaction trigger.
func codeBuddyCompactionRequested(req cliproxyexecutor.Request, opts cliproxyexecutor.Options) bool {
	return opts.Alt == "responses/compact" ||
		helps.HasResponsesCompactionTrigger(req.Payload) ||
		helps.HasResponsesCompactionTrigger(opts.OriginalRequest)
}

// codeBuddyCompactionSourcePayload picks the payload that carries the trigger.
func codeBuddyCompactionSourcePayload(req cliproxyexecutor.Request, opts cliproxyexecutor.Options) []byte {
	payload := req.Payload
	if !helps.HasResponsesCompactionTrigger(payload) && helps.HasResponsesCompactionTrigger(opts.OriginalRequest) {
		payload = opts.OriginalRequest
	}
	if len(payload) == 0 && len(opts.OriginalRequest) > 0 {
		payload = opts.OriginalRequest
	}
	return payload
}

// executeCodeBuddyCompaction performs proxy-side conversation compaction for
// CodeBuddy: the upstream has no compact endpoint, so the summary is produced
// by an ordinary streaming chat completion against the same model and sealed
// into the shared CPA compaction capsule format.
func (e *CodeBuddyExecutor) executeCodeBuddyCompaction(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	summaryPayload := prepareClaudeCompactionSummaryPayload(codeBuddyCompactionSourcePayload(req, opts), baseModel)
	summaryPayload, _ = sjson.DeleteBytes(summaryPayload, "stream")
	// The Responses->Chat translator treats the original request as authoritative
	// for update-intent normalization; both payload and original must carry the
	// summary request or the raw compaction payload is forwarded verbatim.
	summaryReq := cliproxyexecutor.Request{
		Model:    baseModel,
		Payload:  summaryPayload,
		Metadata: req.Metadata,
	}
	summaryOpts := opts
	summaryOpts.Alt = ""
	summaryOpts.Stream = false
	summaryOpts.OriginalRequest = summaryPayload
	summaryOpts.SourceFormat = sdktranslator.FormatOpenAIResponse
	summaryOpts.ResponseFormat = sdktranslator.FormatOpenAIResponse

	summaryResp, errSummary := e.Execute(withCodeBuddyCompactionSummary(ctx), auth, summaryReq, summaryOpts)
	if errSummary != nil {
		return resp, errSummary
	}
	summaryText, errExtract := helps.ExtractAntigravitySummaryText(summaryResp.Payload)
	if errExtract != nil {
		return resp, fmt.Errorf("extract summary: %w", errExtract)
	}
	capsule, errSeal := helps.SealAntigravityCompaction(summaryText, baseModel)
	if errSeal != nil {
		return resp, fmt.Errorf("seal compaction capsule: %w", errSeal)
	}

	usageNode := gjson.GetBytes(summaryResp.Payload, "usage")
	inputTokens := int(usageNode.Get("input_tokens").Int())
	if inputTokens == 0 {
		inputTokens = int(usageNode.Get("prompt_tokens").Int())
	}
	outputTokens := int(usageNode.Get("output_tokens").Int())
	if outputTokens == 0 {
		outputTokens = int(usageNode.Get("completion_tokens").Int())
	}
	totalTokens := int(usageNode.Get("total_tokens").Int())
	cachedTokens := int(usageNode.Get("prompt_cache_hit_tokens").Int())
	if cachedTokens == 0 {
		cachedTokens = int(usageNode.Get("cache_read_input_tokens").Int())
	}
	if totalTokens == 0 && inputTokens == 0 {
		parsed := helps.ParseOpenAIUsage(summaryResp.Payload)
		inputTokens = int(parsed.InputTokens)
		outputTokens = int(parsed.OutputTokens)
		totalTokens = int(parsed.TotalTokens)
	}

	respPayload := helps.BuildAntigravityCompactionResponse(baseModel, capsule, inputTokens, outputTokens, totalTokens)
	if cachedTokens > 0 {
		respPayload, _ = sjson.SetBytes(respPayload, "usage.input_tokens_details.cached_tokens", cachedTokens)
	}
	return cliproxyexecutor.Response{
		Payload: respPayload,
		Headers: summaryResp.Headers,
	}, nil
}

// executeCodeBuddyCompactionStream serves streaming compaction requests by
// wrapping the non-stream result into the standard Responses SSE event chain.
func (e *CodeBuddyExecutor) executeCodeBuddyCompactionStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	summaryResp, errSummary := e.executeCodeBuddyCompaction(ctx, auth, req, opts)
	if errSummary != nil {
		return nil, errSummary
	}
	item := gjson.GetBytes(summaryResp.Payload, "output.0")
	capsule := item.Get("encrypted_content").String()
	if item.Get("type").String() != "compaction" || capsule == "" {
		return nil, fmt.Errorf("extract summary: compaction item missing")
	}
	inputTokens := int(gjson.GetBytes(summaryResp.Payload, "usage.input_tokens").Int())
	outputTokens := int(gjson.GetBytes(summaryResp.Payload, "usage.output_tokens").Int())
	totalTokens := int(gjson.GetBytes(summaryResp.Payload, "usage.total_tokens").Int())
	chunks := helps.BuildAntigravityCompactionStreamChunks(baseModel, capsule, inputTokens, outputTokens, totalTokens)
	out := make(chan cliproxyexecutor.StreamChunk, len(chunks))
	for _, chunk := range chunks {
		out <- cliproxyexecutor.StreamChunk{Payload: chunk}
	}
	close(out)

	headers := summaryResp.Headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Content-Type", "text/event-stream")
	return &cliproxyexecutor.StreamResult{Headers: headers, Chunks: out}, nil
}
