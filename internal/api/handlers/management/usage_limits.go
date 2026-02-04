package management

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

// GetUsageLimits trả về rate limit usage cho 2 time windows: 5h và 1 tuần.
// Dữ liệu được capture từ Claude API response headers (anthropic-ratelimit-*).
//
// GET /v0/management/usage/limits
func (h *Handler) GetUsageLimits(c *gin.Context) {
	store := usage.GetRateLimitStore()
	c.JSON(http.StatusOK, gin.H{
		"last_5h":   store.QueryByWindow(5 * time.Hour),
		"last_week": store.QueryByWindow(7 * 24 * time.Hour),
	})
}
