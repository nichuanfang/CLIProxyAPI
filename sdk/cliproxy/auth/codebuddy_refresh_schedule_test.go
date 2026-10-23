package auth

import (
	"testing"
	"time"
)

func TestCodeBuddyRefreshScheduleUsesCredentialLifetime(t *testing.T) {
	tests := []struct {
		name     string
		expiryIn time.Duration
	}{
		{name: "28-day issuance", expiryIn: 28 * 24 * time.Hour},
		{name: "55-day issuance", expiryIn: 55 * 24 * time.Hour},
		{name: "short issuance", expiryIn: 10 * time.Minute},
	}
	now := time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)
	mgr := &Manager{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expiry := now.Add(test.expiryIn)
			interval := test.expiryIn * 4 / 5
			auth := &Auth{
				ID:              "codebuddy-auth",
				Provider:        "codebuddy",
				LastRefreshedAt: now,
				Metadata: map[string]any{
					"access_token":             "token",
					"expired":                  expiry.Format(time.RFC3339),
					"expires_in":               int(test.expiryIn / time.Second),
					"refresh_interval_seconds": int(interval / time.Second),
				},
			}
			if mgr.shouldRefresh(auth, now) {
				t.Fatalf("freshly refreshed credential is immediately due")
			}
			due := now.Add(interval)
			if !mgr.shouldRefresh(auth, due) {
				t.Fatalf("credential not due at interval boundary %s", due)
			}
			atExpiryBuffer := expiry.Add(-test.expiryIn / 5)
			if !mgr.shouldRefresh(auth, atExpiryBuffer) {
				t.Fatalf("credential not due before expiry at %s", atExpiryBuffer)
			}
		})
	}
}
