package api

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/creditstats"
)

// codexCreditsSnapshot is a package-level hook so tests can stub the source.
var codexCreditsSnapshot = creditstats.Current

// codexCreditsMiddleware publishes the accumulated CodeBuddy credit balance
// through the x-codex-* response header family that the stock Codex client
// reads to render its /status panel. Only the credits headers are sent; the
// window-percentage headers are intentionally left unset because the proxy
// has no truthful window data to report.
func codexCreditsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		snapshot := codexCreditsSnapshot()
		if snapshot.Enabled && snapshot.TotalCredit > 0 {
			header := c.Writer.Header()
			header.Set("x-codex-credits-has-credits", "true")
			header.Set("x-codex-credits-unlimited", "false")
			header.Set("x-codex-credits-balance", strconv.FormatFloat(snapshot.TotalCredit, 'f', -1, 64))
		}
		c.Next()
	}
}
