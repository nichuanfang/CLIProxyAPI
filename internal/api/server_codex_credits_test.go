package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/creditstats"
)

func TestCodexCreditsMiddlewareEmitsHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	old := codexCreditsSnapshot
	codexCreditsSnapshot = func() creditstats.Snapshot {
		return creditstats.Snapshot{Enabled: true, TotalCredit: 12.34}
	}
	t.Cleanup(func() { codexCreditsSnapshot = old })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	codexCreditsMiddleware()(c)

	if got := recorder.Header().Get("x-codex-credits-has-credits"); got != "true" {
		t.Fatalf("has-credits = %q, want true", got)
	}
	if got := recorder.Header().Get("x-codex-credits-unlimited"); got != "false" {
		t.Fatalf("unlimited = %q, want false", got)
	}
	// Local consumption is not a wallet balance and must not masquerade as one.
	if got := recorder.Header().Get("x-codex-credits-balance"); got != "" {
		t.Fatalf("balance = %q, want empty", got)
	}
}

func TestCodexCreditsMiddlewareSilentWhenEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	old := codexCreditsSnapshot
	codexCreditsSnapshot = func() creditstats.Snapshot {
		return creditstats.Snapshot{Enabled: true, TotalCredit: 0}
	}
	t.Cleanup(func() { codexCreditsSnapshot = old })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	codexCreditsMiddleware()(c)

	if got := recorder.Header().Get("x-codex-credits-has-credits"); got != "" {
		t.Fatalf("has-credits = %q, want empty", got)
	}
	if got := recorder.Header().Get("x-codex-credits-balance"); got != "" {
		t.Fatalf("balance = %q, want empty", got)
	}
	if got := recorder.Header().Get("x-codex-credits-has-credits"); got != "" {
		t.Fatalf("has-credits = %q, want empty", got)
	}
	if got := recorder.Header().Get("x-codex-credits-unlimited"); got != "" {
		t.Fatalf("unlimited = %q, want empty", got)
	}

	// Disabled snapshot must also stay silent.
	codexCreditsSnapshot = func() creditstats.Snapshot {
		return creditstats.Snapshot{Enabled: false, TotalCredit: 99}
	}
	recorder2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(recorder2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	codexCreditsMiddleware()(c2)
	if got := recorder2.Header().Get("x-codex-credits-balance"); got != "" {
		t.Fatalf("disabled balance = %q, want empty", got)
	}
}
