package management

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

// GetUsageLimits trả về rate limit usage ở format đơn giản nhất.
// Usage tính theo % (0-100), status là "allowed"/"rejected".
//
// GET /v0/management/usage/limits
func (h *Handler) GetUsageLimits(c *gin.Context) {
	store := usage.GetRateLimitStore()
	latest := store.Latest()

	if latest == nil {
		c.JSON(http.StatusOK, gin.H{
			"5h_usage":  0,
			"5h_status": "unknown",
			"5h_reset":  "",
			"7d_usage":  0,
			"7d_status": "unknown",
			"7d_reset":  "",
		})
		return
	}

	reset5h := ""
	if !latest.Reset5h.IsZero() {
		reset5h = latest.Reset5h.Format(time.RFC3339)
	}
	reset7d := ""
	if !latest.Reset7d.IsZero() {
		reset7d = latest.Reset7d.Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, gin.H{
		"5h_usage":  round2(latest.Utilization5h * 100),
		"5h_status": latest.Status5h,
		"5h_reset":  reset5h,
		"7d_usage":  round2(latest.Utilization7d * 100),
		"7d_status": latest.Status7d,
		"7d_reset":  reset7d,
	})
}

// round2 làm tròn float đến 2 chữ số thập phân.
func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
