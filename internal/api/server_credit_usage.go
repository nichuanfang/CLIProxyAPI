package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/creditstats"
)

// creditUsageHandler exposes the accumulated upstream credit consumption.
// It shares the standard api-key auth of the /v1 group.
func creditUsageHandler(c *gin.Context) {
	c.IndentedJSON(http.StatusOK, creditstats.Current())
}
