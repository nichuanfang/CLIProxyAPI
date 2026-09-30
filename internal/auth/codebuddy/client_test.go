package codebuddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStartAndPollDeviceAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == authStatePath:
			if r.Method != http.MethodPost {
				t.Fatalf("state method = %s, want POST", r.Method)
			}
			if r.Header.Get("Authorization") != "" {
				t.Fatal("state request should not contain Authorization")
			}
			if r.Header.Get("X-IDE-Type") != "CodeBuddyIDE" {
				t.Fatalf("X-IDE-Type = %s", r.Header.Get("X-IDE-Type"))
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"state":"abc","authUrl":"https://example.test/login"}}`))
		case r.URL.Path == authTokenPath:
			if r.URL.Query().Get("state") != "abc" {
				t.Fatalf("state = %s", r.URL.Query().Get("state"))
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"accessToken":"access","refreshToken":"refresh","expiresIn":3600,"refreshExpiresIn":86400}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(nil)
	client.baseURL = server.URL

	session, err := client.StartDeviceAuth(context.Background())
	if err != nil {
		t.Fatalf("StartDeviceAuth() error = %v", err)
	}
	if session.State != "abc" || session.AuthURL != "https://example.test/login" {
		t.Fatalf("session = %+v", session)
	}

	pending, token, err := client.PollDeviceAuth(context.Background(), session.State)
	if err != nil {
		t.Fatalf("PollDeviceAuth() error = %v", err)
	}
	if pending {
		t.Fatal("PollDeviceAuth() pending = true")
	}
	if token == nil || token.AccessToken != "access" || token.RefreshToken != "refresh" {
		t.Fatalf("token = %+v", token)
	}
}

func TestPollDeviceAuthPendingAndErrors(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		wantPnd bool
		wantErr bool
	}{
		{"pending token", 10008, true, false},
		{"login in progress", 11217, true, false},
		{"denied", 40003, false, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"code":` + intString(test.code) + `,"message":"status"}`))
			}))
			defer server.Close()
			client := NewClient(nil)
			client.baseURL = server.URL
			pending, token, err := client.PollDeviceAuth(context.Background(), "state")
			if test.wantErr != (err != nil) {
				t.Fatalf("PollDeviceAuth() error = %v, wantErr %v", err, test.wantErr)
			}
			if pending != test.wantPnd {
				t.Fatalf("pending = %v, want %v", pending, test.wantPnd)
			}
			if token != nil {
				t.Fatalf("token = %+v, want nil", token)
			}
		})
	}
}

func TestRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != authTokenRefreshPath {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("X-Refresh-Token"); got != "old-refresh" {
			t.Fatalf("X-Refresh-Token = %s", got)
		}
		if got := r.Header.Get("X-Auth-Refresh-Source"); got != "plugin" {
			t.Fatalf("X-Auth-Refresh-Source = %s", got)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"accessToken":"new-access","refreshToken":"new-refresh","expiresIn":1800}}`))
	}))
	defer server.Close()

	client := NewClient(nil)
	client.baseURL = server.URL
	token, err := client.RefreshToken(context.Background(), "old-refresh")
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if token.AccessToken != "new-access" || token.RefreshToken != "new-refresh" || token.ExpiresIn != 1800 {
		t.Fatalf("token = %+v", token)
	}
}

func TestRefreshTokenErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantSubstr string
	}{
		{"business error", 200, `{"code":40101,"message":"refresh expired"}`, "code 40101"},
		{"http error", 500, `server failed`, "http 500"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client := NewClient(nil)
			client.baseURL = server.URL
			_, err := client.RefreshToken(context.Background(), "refresh")
			if err == nil || !strings.Contains(err.Error(), test.wantSubstr) {
				t.Fatalf("RefreshToken() error = %v, want substring %q", err, test.wantSubstr)
			}
		})
	}
}

func TestApplyChatHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://example.test", strings.NewReader("{}"))
	ApplyChatHeaders(req, "token", true)

	want := map[string]string{
		"Authorization":     "Bearer token",
		"X-Agent-Intent":    "craft",
		"X-IDE-Type":        "CodeBuddyIDE",
		"X-IDE-Version":     "4.9.7",
		"X-Product-Version": "4.9.7",
		"X-Env-ID":          "production",
		"X-Domain":          "www.codebuddy.cn",
		"X-Product":         "SaaS",
		"User-Agent":        "CodeBuddyIDE/4.9.7 CodeBuddy/4.9.7",
		"Content-Type":      "application/json;charset=UTF-8",
		"Accept":            "text/event-stream",
	}
	for key, value := range want {
		if got := req.Header.Get(key); got != value {
			t.Fatalf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestTokenExpiry(t *testing.T) {
	issued := time.Date(2026, time.September, 30, 10, 0, 0, 0, time.UTC)
	got := TokenExpiry(&Token{ExpiresIn: 60}, issued)
	if !got.Equal(issued.Add(time.Minute)) {
		t.Fatalf("TokenExpiry() = %s", got)
	}
	if !TokenExpiry(nil, issued).IsZero() {
		t.Fatal("TokenExpiry(nil) should be zero")
	}
}

func intString(value int) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
