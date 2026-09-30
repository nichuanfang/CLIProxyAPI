package auth

import (
	"context"
	"fmt"
	"time"

	codebuddyauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codebuddy"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// codeBuddyRefreshLead refreshes access tokens well before their JWT expiry.
var codeBuddyRefreshLead = 30 * 24 * time.Hour

// CodeBuddyAuthenticator implements CodeBuddy device authentication.
type CodeBuddyAuthenticator struct{}

// NewCodeBuddyAuthenticator constructs a new CodeBuddy authenticator.
func NewCodeBuddyAuthenticator() *CodeBuddyAuthenticator {
	return &CodeBuddyAuthenticator{}
}

// Provider returns the unique provider identifier.
func (a *CodeBuddyAuthenticator) Provider() string {
	return "codebuddy"
}

// RefreshLead returns the lead time before JWT expiry for automatic refresh.
func (a *CodeBuddyAuthenticator) RefreshLead() *time.Duration {
	lead := codeBuddyRefreshLead
	return &lead
}

// Login starts and waits for the CodeBuddy browser device login.
func (a *CodeBuddyAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cliproxy auth: configuration is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &LoginOptions{}
	}

	client := codebuddyauth.NewClient(cfg)
	session, errStart := client.StartDeviceAuth(ctx)
	if errStart != nil {
		return nil, errStart
	}

	fmt.Println("Starting CodeBuddy authentication...")
	fmt.Printf("\nTo authenticate, please visit:\n%s\n\n", session.AuthURL)
	if !opts.NoBrowser && browser.IsAvailable() {
		if errOpen := browser.OpenURL(session.AuthURL); errOpen != nil {
			log.Warnf("Failed to open browser automatically: %v", errOpen)
		} else {
			fmt.Println("Browser opened automatically.")
		}
	}
	fmt.Println("Waiting for authorization...")

	token, errWait := client.WaitForDeviceAuth(ctx, session.State, 2*time.Second)
	if errWait != nil {
		return nil, fmt.Errorf("codebuddy authentication failed: %w", errWait)
	}

	baseURL := client.BaseURL()
	expiredAt := codebuddyauth.TokenExpiry(token, time.Now())
	storage := &codebuddyauth.TokenStorage{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expired:      codebuddyauth.FormatExpires(expiredAt),
		BaseURL:      baseURL,
		Type:         "codebuddy",
	}
	metadata := map[string]any{
		"type":          "codebuddy",
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"timestamp":     time.Now().UnixMilli(),
		"base_url":      baseURL,
	}
	if expired := codebuddyauth.FormatExpires(expiredAt); expired != "" {
		metadata["expired"] = expired
	}
	if token.RefreshExpiresIn > 0 {
		metadata["refresh_expires_in"] = token.RefreshExpiresIn
	}

	fileName := fmt.Sprintf("codebuddy-%d.json", time.Now().UnixMilli())
	fmt.Println("\nCodeBuddy authentication successful!")
	return &coreauth.Auth{
		ID:       fileName,
		Provider: "codebuddy",
		FileName: fileName,
		Label:    "CodeBuddy User",
		Storage:  storage,
		Metadata: metadata,
		Attributes: map[string]string{
			"base_url": baseURL,
		},
	}, nil
}
