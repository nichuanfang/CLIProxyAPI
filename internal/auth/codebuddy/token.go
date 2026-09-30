package codebuddy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/misc"
	log "github.com/sirupsen/logrus"
)

// TokenStorage is the file representation of a CodeBuddy credential.
type TokenStorage struct {
	AccessToken  string         `json:"access_token"`
	RefreshToken string         `json:"refresh_token"`
	Expired      string         `json:"expired,omitempty"`
	BaseURL      string         `json:"base_url,omitempty"`
	Type         string         `json:"type"`
	Metadata     map[string]any `json:"-"`
}

// SetMetadata injects runtime metadata before serialization.
func (ts *TokenStorage) SetMetadata(meta map[string]any) {
	if ts != nil {
		ts.Metadata = meta
	}
}

// SaveTokenToFile writes the credential using the shared auth-file format.
func (ts *TokenStorage) SaveTokenToFile(authFilePath string) error {
	if ts == nil {
		return fmt.Errorf("codebuddy token storage is nil")
	}
	misc.LogSavingCredentials(authFilePath)
	if ts.Type == "" {
		ts.Type = "codebuddy"
	}
	if ts.BaseURL == "" {
		ts.BaseURL = DefaultBaseURL
	}
	if err := os.MkdirAll(filepath.Dir(authFilePath), 0o700); err != nil {
		return fmt.Errorf("codebuddy token storage: create directory failed: %w", err)
	}
	data, errMerge := misc.MergeMetadata(ts, ts.Metadata)
	if errMerge != nil {
		return fmt.Errorf("codebuddy token storage: merge metadata failed: %w", errMerge)
	}
	file, err := os.Create(authFilePath)
	if err != nil {
		return fmt.Errorf("codebuddy token storage: create token file failed: %w", err)
	}
	defer func() {
		if errClose := file.Close(); errClose != nil {
			log.Errorf("codebuddy token storage: close token file failed: %v", errClose)
		}
	}()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if errEncode := encoder.Encode(data); errEncode != nil {
		return fmt.Errorf("codebuddy token storage: write token failed: %w", errEncode)
	}
	return nil
}

// IsExpired reports whether the stored expiry timestamp is in the past.
func (ts *TokenStorage) IsExpired() bool {
	if ts == nil || ts.Expired == "" {
		return false
	}
	expired, errParse := time.Parse(time.RFC3339, ts.Expired)
	if errParse != nil {
		return false
	}
	return time.Now().After(expired)
}
