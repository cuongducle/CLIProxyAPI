package executor

import (
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	log "github.com/sirupsen/logrus"
)

// captureClaudeRateLimit parse rate limit headers từ Claude API response
// và lưu vào RateLimitStore. Hỗ trợ cả Unified (OAuth) và Standard (API key) format.
func captureClaudeRateLimit(headers http.Header, source, model string) {
	if headers == nil {
		return
	}

	// Kiểm tra nhanh xem có bất kỳ ratelimit header nào không
	hasRateLimit := false
	for key := range headers {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "ratelimit") {
			hasRateLimit = true
			break
		}
	}
	if !hasRateLimit {
		return
	}

	record := usage.ParseRateLimitHeaders(headers)
	if record.IsEmpty() {
		log.Debugf("ratelimit: headers found but parsed empty for model=%s source=%s", model, source)
		return
	}

	if record.Type == "unified" {
		log.Infof("ratelimit: [unified] model=%s source=%s 5h=%.2f%% (%s) 7d=%.2f%% (%s) overage=%s",
			model, source,
			record.Utilization5h*100, record.Status5h,
			record.Utilization7d*100, record.Status7d,
			record.OverageStatus)
	} else {
		log.Infof("ratelimit: [standard] model=%s source=%s requests=%d/%d tokens=%d/%d",
			model, source,
			record.RequestsRemaining, record.RequestsLimit,
			record.TokensRemaining, record.TokensLimit)
	}

	record.Source = source
	record.Model = model
	usage.GetRateLimitStore().Record(record)
}
