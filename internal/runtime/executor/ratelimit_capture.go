package executor

import (
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

// captureClaudeRateLimit parse rate limit headers từ Claude API response
// và lưu vào RateLimitStore. Nếu không có headers -> skip.
func captureClaudeRateLimit(headers http.Header, source, model string) {
	if headers == nil {
		return
	}
	record := usage.ParseRateLimitHeaders(headers)
	if record.IsEmpty() {
		return
	}
	record.Source = source
	record.Model = model
	usage.GetRateLimitStore().Record(record)
}
