package management

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codebuddy"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// RequestCodeBuddyToken starts the CodeBuddy browser device login flow.
func (h *Handler) RequestCodeBuddyToken(c *gin.Context) {
	ctx := context.Background()
	ctx = PopulateAuthContext(ctx, c)

	fmt.Println("Initializing CodeBuddy authentication...")

	client := codebuddy.NewClient(h.cfg)
	session, errStart := client.StartDeviceAuth(ctx)
	if errStart != nil {
		log.Errorf("Failed to start CodeBuddy device flow: %v", errStart)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start device authorization flow"})
		return
	}

	state := fmt.Sprintf("cbb-%d", time.Now().UnixNano())
	providerName := "codebuddy"
	RegisterOAuthSession(state, providerName)

	go func() {
		pollCtx, cancelPoll := context.WithCancel(ctx)
		defer cancelPoll()
		go watchOAuthSessionCancel(pollCtx, cancelPoll, state, providerName)

		fmt.Println("Waiting for CodeBuddy authentication...")
		token, errWait := client.WaitForDeviceAuth(pollCtx, session.State, 2*time.Second)
		if errWait != nil {
			if !IsOAuthSessionPending(state, providerName) {
				return
			}
			SetOAuthSessionError(state, oauthSessionErrorWithCause("Authentication failed", errWait))
			fmt.Printf("CodeBuddy authentication failed: %v\n", errWait)
			return
		}
		if !IsOAuthSessionPending(state, providerName) {
			return
		}

		baseURL := client.BaseURL()
		expiredAt := codebuddy.TokenExpiry(token, time.Now())
		storage := &codebuddy.TokenStorage{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			Expired:      codebuddy.FormatExpires(expiredAt),
			BaseURL:      baseURL,
			Type:         providerName,
		}
		metadata := map[string]any{
			"type":          providerName,
			"access_token":  token.AccessToken,
			"refresh_token": token.RefreshToken,
			"timestamp":     time.Now().UnixMilli(),
			"base_url":      baseURL,
		}
		if expired := codebuddy.FormatExpires(expiredAt); expired != "" {
			metadata["expired"] = expired
		}
		if token.RefreshExpiresIn > 0 {
			metadata["refresh_expires_in"] = token.RefreshExpiresIn
		}

		fileName := fmt.Sprintf("codebuddy-%d.json", time.Now().UnixMilli())
		record := &coreauth.Auth{
			ID:       fileName,
			Provider: providerName,
			FileName: fileName,
			Label:    "CodeBuddy User",
			Storage:  storage,
			Metadata: metadata,
			Attributes: map[string]string{
				"base_url": baseURL,
			},
		}
		if errGuard := guardOAuthSessionPendingForSave(state, providerName); errGuard != nil {
			return
		}
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("Failed to save CodeBuddy token to file: %v", errSave)
			SetOAuthSessionError(state, "Failed to save token to file")
			return
		}
		CompleteOAuthSession(state)
		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		fmt.Println("You can now use CodeBuddy services through this CLI")
	}()

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"url":    session.AuthURL,
		"state":  state,
		"flow":   "device",
	})
}
