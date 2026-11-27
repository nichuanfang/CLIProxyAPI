package util

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	log "github.com/sirupsen/logrus"
)

func TestSetLogLevel(t *testing.T) {
	originalLevel := log.GetLevel()
	t.Cleanup(func() {
		log.SetLevel(originalLevel)
	})

	tests := []struct {
		name     string
		cfg      config.Config
		expected log.Level
	}{
		{name: "explicit warn", cfg: config.Config{LogLevel: "warn"}, expected: log.WarnLevel},
		{name: "explicit level wins over debug", cfg: config.Config{Debug: true, LogLevel: "error"}, expected: log.ErrorLevel},
		{name: "legacy debug", cfg: config.Config{Debug: true}, expected: log.DebugLevel},
		{name: "legacy default", cfg: config.Config{}, expected: log.InfoLevel},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log.SetLevel(log.ErrorLevel)
			SetLogLevel(&tc.cfg)
			if got := log.GetLevel(); got != tc.expected {
				t.Fatalf("log level = %s, want %s", got, tc.expected)
			}
		})
	}
}
