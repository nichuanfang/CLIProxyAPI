// Package codebuddy implements CodeBuddy device authentication and token refresh.
package codebuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	log "github.com/sirupsen/logrus"
)

const (
	// DefaultBaseURL is the CodeBuddy API base URL.
	DefaultBaseURL = "https://copilot.tencent.com"
	// ChatPath is the OpenAI-compatible chat completions endpoint.
	ChatPath = "/v2/chat/completions"

	authStatePath        = "/v2/plugin/auth/state"
	authTokenPath        = "/v2/plugin/auth/token"
	authTokenRefreshPath = "/v2/plugin/auth/token/refresh"

	deviceAuthPendingCode     = 10008
	deviceAuthLoginInProgress = 11217

	defaultIDEVersion = "4.9.7"
	defaultDomain     = "www.codebuddy.cn"
)

// DeviceAuthSession contains a pending CodeBuddy browser authorization session.
type DeviceAuthSession struct {
	State   string
	AuthURL string
}

// Token contains the credential fields returned by CodeBuddy authentication.
type Token struct {
	AccessToken      string
	RefreshToken     string
	ExpiresIn        int
	RefreshExpiresIn int
}

// Client talks to the CodeBuddy plugin authentication endpoints.
type Client struct {
	baseURL string
	client  *http.Client
	cfg     *config.Config
}

// NewClient creates a CodeBuddy authentication client.
func NewClient(cfg *config.Config) *Client {
	return &Client{baseURL: DefaultBaseURL, cfg: cfg}
}

// NewClientWithBaseURL creates a CodeBuddy authentication client using an explicit base URL.
func NewClientWithBaseURL(cfg *config.Config, baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{baseURL: baseURL, cfg: cfg}
}

// SetHTTPClient overrides the HTTP client (used by tests and refresh flows).
func (c *Client) SetHTTPClient(client *http.Client) {
	if c != nil {
		c.client = client
	}
}

// BaseURL returns the configured CodeBuddy API base URL.
func (c *Client) BaseURL() string {
	if c == nil || strings.TrimSpace(c.baseURL) == "" {
		return DefaultBaseURL
	}
	return strings.TrimRight(strings.TrimSpace(c.baseURL), "/")
}

// StartDeviceAuth requests a CodeBuddy device login state and browser URL.
func (c *Client) StartDeviceAuth(ctx context.Context) (*DeviceAuthSession, error) {
	raw, err := c.doNoAuthJSON(ctx, http.MethodPost, authStatePath+"?platform=desktop", []byte("{}"))
	if err != nil {
		return nil, fmt.Errorf("codebuddy auth: start device auth failed: %w", err)
	}
	var env authEnvelope
	if errUnmarshal := json.Unmarshal(raw, &env); errUnmarshal != nil {
		return nil, fmt.Errorf("codebuddy auth: decode auth state failed: %w", errUnmarshal)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("codebuddy auth: auth state failed: code %d: %s", env.Code, env.message())
	}
	var data struct {
		State   string `json:"state"`
		AuthURL string `json:"authUrl"`
	}
	if env.Data == nil {
		return nil, fmt.Errorf("codebuddy auth: invalid auth state payload")
	}
	if errUnmarshal := json.Unmarshal(env.Data, &data); errUnmarshal != nil {
		return nil, fmt.Errorf("codebuddy auth: invalid auth state payload: %w", errUnmarshal)
	}
	data.State = strings.TrimSpace(data.State)
	data.AuthURL = strings.TrimSpace(data.AuthURL)
	if data.State == "" || data.AuthURL == "" {
		return nil, fmt.Errorf("codebuddy auth: auth state missing state or authUrl")
	}
	return &DeviceAuthSession{State: data.State, AuthURL: data.AuthURL}, nil
}

// PollDeviceAuth polls once for a completed browser login. A true pending result
// indicates that authorization has not finished yet.
func (c *Client) PollDeviceAuth(ctx context.Context, state string) (bool, *Token, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		return false, nil, fmt.Errorf("codebuddy auth: state is required")
	}
	path := authTokenPath + "?state=" + url.QueryEscape(state)
	raw, err := c.doNoAuthJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, nil, fmt.Errorf("codebuddy auth: poll device auth failed: %w", err)
	}
	var env authEnvelope
	if errUnmarshal := json.Unmarshal(raw, &env); errUnmarshal != nil {
		return false, nil, fmt.Errorf("codebuddy auth: decode auth token failed: %w", errUnmarshal)
	}
	if env.Code == deviceAuthPendingCode || env.Code == deviceAuthLoginInProgress {
		return true, nil, nil
	}
	if env.Code != 0 {
		return false, nil, fmt.Errorf("codebuddy auth: auth token failed: code %d: %s", env.Code, env.message())
	}
	var data struct {
		AccessToken      string `json:"accessToken"`
		RefreshToken     string `json:"refreshToken"`
		ExpiresIn        int    `json:"expiresIn"`
		RefreshExpiresIn int    `json:"refreshExpiresIn"`
	}
	if env.Data == nil {
		return false, nil, fmt.Errorf("codebuddy auth: invalid auth token payload")
	}
	if errUnmarshal := json.Unmarshal(env.Data, &data); errUnmarshal != nil {
		return false, nil, fmt.Errorf("codebuddy auth: invalid auth token payload: %w", errUnmarshal)
	}
	data.AccessToken = strings.TrimSpace(data.AccessToken)
	if data.AccessToken == "" {
		return false, nil, fmt.Errorf("codebuddy auth: auth token returned empty access token")
	}
	return false, &Token{
		AccessToken:      data.AccessToken,
		RefreshToken:     strings.TrimSpace(data.RefreshToken),
		ExpiresIn:        data.ExpiresIn,
		RefreshExpiresIn: data.RefreshExpiresIn,
	}, nil
}

// WaitForDeviceAuth polls CodeBuddy until login completes or the context is canceled.
func (c *Client) WaitForDeviceAuth(ctx context.Context, state string, interval time.Duration) (*Token, error) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		pending, token, err := c.PollDeviceAuth(ctx, state)
		if err != nil {
			return nil, err
		}
		if !pending {
			return token, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// RefreshToken exchanges a refresh token for a new CodeBuddy access token.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (*Token, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, fmt.Errorf("codebuddy auth: refresh token is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL()+authTokenRefreshPath, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("codebuddy auth: create refresh request failed: %w", err)
	}
	c.applyNoAuthHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Refresh-Source", "plugin")
	req.Header.Set("X-Refresh-Token", refreshToken)
	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	raw, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	var env authEnvelope
	if errUnmarshal := json.Unmarshal(raw, &env); errUnmarshal != nil {
		return nil, fmt.Errorf("codebuddy auth: decode refresh response failed: %w", errUnmarshal)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("codebuddy auth: refresh failed: code %d: %s", env.Code, env.message())
	}
	token, err := tokenFromData(env.Data, "refresh")
	if err != nil {
		return nil, err
	}
	return token, nil
}

// applyNoAuthHeaders adds the browser-like headers accepted by public auth endpoints.
func (c *Client) applyNoAuthHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("X-No-Authorization", "true")
	req.Header.Set("X-No-User-Id", "true")
	req.Header.Set("X-No-Enterprise-Id", "true")
	req.Header.Set("X-No-Department-Info", "true")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-Domain", defaultDomain)
	req.Header.Set("X-Product", "SaaS")
	req.Header.Set("X-IDE-Type", "CodeBuddyIDE")
	req.Header.Set("X-IDE-Version", defaultIDEVersion)
	req.Header.Set("User-Agent", "CodeBuddyIDE/"+defaultIDEVersion+" CodeBuddy/"+defaultIDEVersion)
}

func (c *Client) doNoAuthJSON(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL()+path, reader)
	if err != nil {
		return nil, err
	}
	c.applyNoAuthHeaders(req)
	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	return readBody(resp)
}

func (c *Client) doRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req = req.WithContext(ctx)
	httpClient := c.client
	if httpClient == nil {
		// Auth token acquisition is the only credential timeout allowed here.
		httpClient = &http.Client{Timeout: 30 * time.Second}
		proxyURL := ""
		if c.cfg != nil {
			proxyURL = strings.TrimSpace(c.cfg.ProxyURL)
		}
		if proxyURL != "" {
			var sdkCfg config.SDKConfig
			sdkCfg.ProxyURL = proxyURL
			httpClient = util.SetProxy(&sdkCfg, httpClient)
		}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codebuddy auth: request failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("codebuddy auth: http %d: %s", resp.StatusCode, clipBody(raw))
	}
	return resp, nil
}

func readBody(resp *http.Response) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("codebuddy auth: response is nil")
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("codebuddy auth: close response failed: %v", errClose)
		}
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("codebuddy auth: read response failed: %w", err)
	}
	return raw, nil
}

// ApplyChatHeaders sets the headers required by CodeBuddy IDE chat requests.
func ApplyChatHeaders(req *http.Request, accessToken string, stream bool) {
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("X-Agent-Intent", "craft")
	req.Header.Set("X-IDE-Type", "CodeBuddyIDE")
	req.Header.Set("X-IDE-Version", defaultIDEVersion)
	req.Header.Set("X-Product-Version", defaultIDEVersion)
	req.Header.Set("X-Env-ID", "production")
	req.Header.Set("X-Domain", defaultDomain)
	req.Header.Set("X-Product", "SaaS")
	req.Header.Set("User-Agent", "CodeBuddyIDE/"+defaultIDEVersion+" CodeBuddy/"+defaultIDEVersion)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
		return
	}
	req.Header.Set("Accept", "application/json")
}

type authEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Msg     string          `json:"msg"`
	Data    json.RawMessage `json:"data"`
}

func (e authEnvelope) message() string {
	if strings.TrimSpace(e.Message) != "" {
		return strings.TrimSpace(e.Message)
	}
	if strings.TrimSpace(e.Msg) != "" {
		return strings.TrimSpace(e.Msg)
	}
	return "unknown error"
}

func tokenFromData(data json.RawMessage, operation string) (*Token, error) {
	var parsed struct {
		AccessToken      string `json:"accessToken"`
		RefreshToken     string `json:"refreshToken"`
		ExpiresIn        int    `json:"expiresIn"`
		RefreshExpiresIn int    `json:"refreshExpiresIn"`
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("codebuddy auth: invalid %s payload", operation)
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("codebuddy auth: invalid %s payload: %w", operation, err)
	}
	parsed.AccessToken = strings.TrimSpace(parsed.AccessToken)
	if parsed.AccessToken == "" {
		return nil, fmt.Errorf("codebuddy auth: %s returned empty access token", operation)
	}
	return &Token{
		AccessToken:      parsed.AccessToken,
		RefreshToken:     strings.TrimSpace(parsed.RefreshToken),
		ExpiresIn:        parsed.ExpiresIn,
		RefreshExpiresIn: parsed.RefreshExpiresIn,
	}, nil
}

func clipBody(raw []byte) string {
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "empty response"
	}
	if len(value) > 300 {
		return value[:300]
	}
	return value
}

// FormatExpires returns an RFC3339 timestamp for a token expiry.
func FormatExpires(expiresAt time.Time) string {
	if expiresAt.IsZero() {
		return ""
	}
	return expiresAt.UTC().Format(time.RFC3339)
}

// TokenExpiry returns the absolute expiry based on the issued-at time.
func TokenExpiry(token *Token, issuedAt time.Time) time.Time {
	if token == nil || token.ExpiresIn <= 0 {
		return time.Time{}
	}
	if issuedAt.IsZero() {
		issuedAt = time.Now()
	}
	return issuedAt.Add(time.Duration(token.ExpiresIn) * time.Second)
}
