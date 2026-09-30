package auth

import (
	"testing"
	"time"
)

func TestCodeBuddyAuthenticatorProviderAndRefreshLead(t *testing.T) {
	authenticator := NewCodeBuddyAuthenticator()
	if authenticator.Provider() != "codebuddy" {
		t.Fatalf("Provider() = %q", authenticator.Provider())
	}
	lead := authenticator.RefreshLead()
	if lead == nil || *lead != 30*24*time.Hour {
		t.Fatalf("RefreshLead() = %v, want 720h", lead)
	}
}
