package codebuddy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenStorageSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codebuddy.json")
	storage := &TokenStorage{
		AccessToken:  "access",
		RefreshToken: "refresh",
		Expired:      FormatExpires(time.Now().Add(24 * time.Hour)),
		BaseURL:      "https://upstream.test",
		Type:         "codebuddy",
	}
	storage.SetMetadata(map[string]any{"timestamp": 1})

	if err := storage.SaveTokenToFile(path); err != nil {
		t.Fatalf("SaveTokenToFile() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if saved["type"] != "codebuddy" || saved["access_token"] != "access" || saved["refresh_token"] != "refresh" {
		t.Fatalf("saved = %v", saved)
	}
	if saved["base_url"] != "https://upstream.test" {
		t.Fatalf("base_url = %v", saved["base_url"])
	}
	if saved["timestamp"] != float64(1) {
		t.Fatal("metadata should be flattened into saved file")
	}
	if storage.IsExpired() {
		t.Fatal("future token reported expired")
	}
}
