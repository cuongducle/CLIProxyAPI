package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// rateLimitFilePath chứa đường dẫn file lưu rate limit statistics.
var rateLimitFilePath atomic.Value

// rlAutoSaveCancel dùng để cancel auto-save goroutine cho rate limit
var rlAutoSaveCancel context.CancelFunc
var rlAutoSaveMu sync.Mutex

// SetRateLimitFilePath đặt đường dẫn file lưu rate limit statistics.
func SetRateLimitFilePath(path string) {
	rateLimitFilePath.Store(path)
}

// GetRateLimitFilePath trả về đường dẫn file lưu rate limit statistics.
func GetRateLimitFilePath() string {
	if v := rateLimitFilePath.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// RateLimitRecord lưu 1 snapshot rate limit từ Claude API response headers.
type RateLimitRecord struct {
	Timestamp             time.Time `json:"timestamp"`
	Source                string    `json:"source"`                            // auth email/key identifier
	Model                 string    `json:"model"`                             // model name
	RequestsLimit         int64     `json:"requests_limit,omitempty"`          // anthropic-ratelimit-requests-limit
	RequestsRemaining     int64     `json:"requests_remaining,omitempty"`      // anthropic-ratelimit-requests-remaining
	RequestsReset         time.Time `json:"requests_reset,omitempty"`          // anthropic-ratelimit-requests-reset
	TokensLimit           int64     `json:"tokens_limit,omitempty"`            // anthropic-ratelimit-tokens-limit
	TokensRemaining       int64     `json:"tokens_remaining,omitempty"`        // anthropic-ratelimit-tokens-remaining
	TokensReset           time.Time `json:"tokens_reset,omitempty"`            // anthropic-ratelimit-tokens-reset
	InputTokensLimit      int64     `json:"input_tokens_limit,omitempty"`      // anthropic-ratelimit-input-tokens-limit
	InputTokensRemaining  int64     `json:"input_tokens_remaining,omitempty"`  // anthropic-ratelimit-input-tokens-remaining
	InputTokensReset      time.Time `json:"input_tokens_reset,omitempty"`      // anthropic-ratelimit-input-tokens-reset
	OutputTokensLimit     int64     `json:"output_tokens_limit,omitempty"`     // anthropic-ratelimit-output-tokens-limit
	OutputTokensRemaining int64     `json:"output_tokens_remaining,omitempty"` // anthropic-ratelimit-output-tokens-remaining
	OutputTokensReset     time.Time `json:"output_tokens_reset,omitempty"`     // anthropic-ratelimit-output-tokens-reset
}

// IsEmpty kiểm tra xem record có chứa dữ liệu rate limit hợp lệ không.
func (r RateLimitRecord) IsEmpty() bool {
	return r.RequestsLimit == 0 && r.TokensLimit == 0 && r.InputTokensLimit == 0 && r.OutputTokensLimit == 0
}

// SourceUsage chứa usage summary cho 1 source (auth email/key).
type SourceUsage struct {
	Requests    int64            `json:"requests"`
	LatestLimit *RateLimitRecord `json:"latest_limit,omitempty"`
}

// WindowSummary chứa aggregated usage cho 1 time window.
type WindowSummary struct {
	TotalRequests int64                  `json:"total_requests"`
	LatestLimit   *RateLimitRecord       `json:"latest_limit,omitempty"`
	BySource      map[string]SourceUsage `json:"by_source,omitempty"`
}

// RateLimitStore lưu trữ in-memory các rate limit records với JSON persistence.
type RateLimitStore struct {
	mu      sync.RWMutex
	records []RateLimitRecord
}

var defaultRateLimitStore = NewRateLimitStore()

// GetRateLimitStore trả về global singleton store.
func GetRateLimitStore() *RateLimitStore { return defaultRateLimitStore }

// NewRateLimitStore tạo store mới.
func NewRateLimitStore() *RateLimitStore {
	return &RateLimitStore{}
}

// maxRecordAge giới hạn records được giữ trong memory (7 ngày).
const maxRecordAge = 7 * 24 * time.Hour

// Record thêm 1 rate limit record vào store.
func (s *RateLimitStore) Record(r RateLimitRecord) {
	if s == nil || r.IsEmpty() {
		return
	}
	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now()
	}

	s.mu.Lock()
	s.records = append(s.records, r)
	// Cleanup records cũ hơn 7 ngày mỗi 100 records
	if len(s.records)%100 == 0 {
		s.cleanupLocked()
	}
	count := len(s.records)
	s.mu.Unlock()

	// Auto-save sau mỗi 10 records
	if count%10 == 0 {
		go func() {
			_ = s.Save()
		}()
	}
}

// cleanupLocked xóa records cũ hơn maxRecordAge. Phải gọi trong lock.
func (s *RateLimitStore) cleanupLocked() {
	cutoff := time.Now().Add(-maxRecordAge)
	n := 0
	for _, r := range s.records {
		if r.Timestamp.After(cutoff) {
			s.records[n] = r
			n++
		}
	}
	s.records = s.records[:n]
}

// QueryByWindow trả về aggregated summary cho records trong time window.
func (s *RateLimitStore) QueryByWindow(d time.Duration) WindowSummary {
	summary := WindowSummary{
		BySource: make(map[string]SourceUsage),
	}
	if s == nil {
		return summary
	}

	cutoff := time.Now().Add(-d)

	s.mu.RLock()
	defer s.mu.RUnlock()

	var latestTime time.Time
	for i := range s.records {
		r := &s.records[i]
		if r.Timestamp.Before(cutoff) {
			continue
		}
		summary.TotalRequests++

		// Track latest record overall
		if r.Timestamp.After(latestTime) {
			latestTime = r.Timestamp
			rCopy := *r
			summary.LatestLimit = &rCopy
		}

		// Track per-source
		source := r.Source
		if source == "" {
			source = "unknown"
		}
		su := summary.BySource[source]
		su.Requests++
		if su.LatestLimit == nil || r.Timestamp.After(su.LatestLimit.Timestamp) {
			rCopy := *r
			su.LatestLimit = &rCopy
		}
		summary.BySource[source] = su
	}

	return summary
}

// rateLimitSnapshot dùng cho JSON persistence.
type rateLimitSnapshot struct {
	Records []RateLimitRecord `json:"records"`
}

// Save lưu records ra file JSON.
func (s *RateLimitStore) Save() error {
	if s == nil {
		return nil
	}
	filePath := GetRateLimitFilePath()
	if filePath == "" {
		return nil
	}

	s.mu.RLock()
	// Chỉ lưu records trong 7 ngày gần nhất
	cutoff := time.Now().Add(-maxRecordAge)
	var filtered []RateLimitRecord
	for _, r := range s.records {
		if r.Timestamp.After(cutoff) {
			filtered = append(filtered, r)
		}
	}
	s.mu.RUnlock()

	snapshot := rateLimitSnapshot{Records: filtered}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal ratelimit statistics: %w", err)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Atomic write: write to temp file, then rename
	tmpFile := filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		// Fallback: ghi trực tiếp
		if directErr := os.WriteFile(filePath, data, 0o644); directErr != nil {
			return fmt.Errorf("failed to write ratelimit file: %w", directErr)
		}
		return nil
	}

	if err := os.Rename(tmpFile, filePath); err != nil {
		_ = os.Remove(tmpFile)
		// Fallback: ghi trực tiếp (Docker file mount)
		if directErr := os.WriteFile(filePath, data, 0o644); directErr != nil {
			return fmt.Errorf("failed to write ratelimit file: %w", directErr)
		}
	}

	return nil
}

// Load đọc records từ file JSON và restore vào memory.
func (s *RateLimitStore) Load() error {
	if s == nil {
		return nil
	}
	filePath := GetRateLimitFilePath()
	if filePath == "" {
		return nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read ratelimit file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	var snapshot rateLimitSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("failed to unmarshal ratelimit statistics: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.records = snapshot.Records
	s.cleanupLocked()

	return nil
}

// StartRateLimitAutoSave bắt đầu auto-save rate limit statistics định kỳ.
func StartRateLimitAutoSave(ctx context.Context, interval time.Duration) {
	rlAutoSaveMu.Lock()
	defer rlAutoSaveMu.Unlock()

	if rlAutoSaveCancel != nil {
		rlAutoSaveCancel()
	}

	ctx, rlAutoSaveCancel = context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = defaultRateLimitStore.Save()
			}
		}
	}()
}

// StopRateLimitAutoSave dừng auto-save và save lần cuối.
func StopRateLimitAutoSave() {
	rlAutoSaveMu.Lock()
	if rlAutoSaveCancel != nil {
		rlAutoSaveCancel()
		rlAutoSaveCancel = nil
	}
	rlAutoSaveMu.Unlock()

	_ = defaultRateLimitStore.Save()
}

// ParseRateLimitHeaders parse rate limit headers từ HTTP response của Claude API.
// Trả về RateLimitRecord với các giá trị đã parse.
func ParseRateLimitHeaders(headers http.Header) RateLimitRecord {
	r := RateLimitRecord{
		Timestamp: time.Now(),
	}

	r.RequestsLimit = parseIntHeader(headers, "anthropic-ratelimit-requests-limit")
	r.RequestsRemaining = parseIntHeader(headers, "anthropic-ratelimit-requests-remaining")
	r.RequestsReset = parseTimeHeader(headers, "anthropic-ratelimit-requests-reset")
	r.TokensLimit = parseIntHeader(headers, "anthropic-ratelimit-tokens-limit")
	r.TokensRemaining = parseIntHeader(headers, "anthropic-ratelimit-tokens-remaining")
	r.TokensReset = parseTimeHeader(headers, "anthropic-ratelimit-tokens-reset")
	r.InputTokensLimit = parseIntHeader(headers, "anthropic-ratelimit-input-tokens-limit")
	r.InputTokensRemaining = parseIntHeader(headers, "anthropic-ratelimit-input-tokens-remaining")
	r.InputTokensReset = parseTimeHeader(headers, "anthropic-ratelimit-input-tokens-reset")
	r.OutputTokensLimit = parseIntHeader(headers, "anthropic-ratelimit-output-tokens-limit")
	r.OutputTokensRemaining = parseIntHeader(headers, "anthropic-ratelimit-output-tokens-remaining")
	r.OutputTokensReset = parseTimeHeader(headers, "anthropic-ratelimit-output-tokens-reset")

	return r
}

func parseIntHeader(headers http.Header, name string) int64 {
	v := headers.Get(name)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func parseTimeHeader(headers http.Header, name string) time.Time {
	v := headers.Get(name)
	if v == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}
