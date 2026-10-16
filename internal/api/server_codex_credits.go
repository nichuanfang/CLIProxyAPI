package api

import (
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/creditstats"
)

// codexCreditsSnapshot is a package-level hook so tests can stub the source.
var codexCreditsSnapshot = creditstats.Current

// codexCreditsMiddleware signals that CodeBuddy credit consumption has been
// observed through the x-codex-* response header family. The stock Codex
// client reads these headers for its /status panel. Accumulated local spend is
// not a wallet balance, so no balance header is reported until a real CodeBuddy
// balance source is available. Window-percentage headers are also left unset
// because the proxy has no truthful window data to report.
func codexCreditsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		snapshot := codexCreditsSnapshot()
		if snapshot.Enabled && snapshot.TotalCredit > 0 {
			header := c.Writer.Header()
			header.Set("x-codex-credits-has-credits", "true")
			header.Set("x-codex-credits-unlimited", "false")
		}
		c.Next()
	}
}
