package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// SignatureEntry holds a cached thinking signature with timestamp
type SignatureEntry struct {
	Signature string
	Timestamp time.Time
}

const (
	// SignatureCacheTTL is how long signatures are valid
	SignatureCacheTTL = 2 * time.Hour

	// MaxEntriesPerSession limits memory usage per session
	MaxEntriesPerSession = 100

	// SignatureTextHashLen is the length of the hash key (16 hex chars = 64-bit key space)
	SignatureTextHashLen = 16

	// MinValidSignatureLen is the minimum length for a signature to be considered valid
	MinValidSignatureLen = 50

	// CacheCleanupInterval controls how often stale entries are purged
	CacheCleanupInterval = 10 * time.Minute
)

// signatureCache stores signatures by model group -> textHash -> SignatureEntry
var signatureCache sync.Map

// cacheCleanupOnce ensures the background cleanup goroutine starts only once
var cacheCleanupOnce sync.Once

// groupCache is the inner map type
type groupCache struct {
	mu      sync.RWMutex
	entries map[string]SignatureEntry
}

// hashText creates a stable, Unicode-safe key from text content
func hashText(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])[:SignatureTextHashLen]
}

// getOrCreateGroupCache gets or creates a cache bucket for a model group
func getOrCreateGroupCache(groupKey string) *groupCache {
	// Start background cleanup on first access
	cacheCleanupOnce.Do(startCacheCleanup)

	if val, ok := signatureCache.Load(groupKey); ok {
		return val.(*groupCache)
	}
	sc := &groupCache{entries: make(map[string]SignatureEntry)}
	actual, _ := signatureCache.LoadOrStore(groupKey, sc)
	return actual.(*groupCache)
}

// startCacheCleanup launches a background goroutine that periodically
// removes caches where all entries have expired.
func startCacheCleanup() {
	go func() {
		ticker := time.NewTicker(CacheCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			purgeExpiredCaches()
		}
	}()
}

// purgeExpiredCaches removes caches with no valid (non-expired) entries.
func purgeExpiredCaches() {
	now := time.Now()
	signatureCache.Range(func(key, value any) bool {
		sc := value.(*groupCache)
		sc.mu.Lock()
		// Remove expired entries
		for k, entry := range sc.entries {
			if now.Sub(entry.Timestamp) > SignatureCacheTTL {
				delete(sc.entries, k)
			}
		}
		isEmpty := len(sc.entries) == 0
		sc.mu.Unlock()
		// Remove cache bucket if empty
		if isEmpty {
			signatureCache.Delete(key)
		}
		return true
	})
}

// CacheSignature stores a thinking signature for a given model group and text.
// Used for Claude models that require signed thinking blocks in multi-turn conversations.
func CacheSignature(modelName, text, signature string) {
	if text == "" || signature == "" {
		return
	}
	if len(signature) < MinValidSignatureLen {
		return
	}

	groupKey := GetModelGroup(modelName)
	textHash := hashText(text)
	sc := getOrCreateGroupCache(groupKey)
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.entries[textHash] = SignatureEntry{
		Signature: signature,
		Timestamp: time.Now(),
	}
}

// GetCachedSignature retrieves a cached signature for a given model group and text.
// Returns empty string if not found or expired.
func GetCachedSignature(modelName, text string) string {
	groupKey := GetModelGroup(modelName)

	if text == "" {
		if groupKey == "gemini" {
			return "skip_thought_signature_validator"
		}
		return ""
	}
	val, ok := signatureCache.Load(groupKey)
	if !ok {
		if groupKey == "gemini" {
			return "skip_thought_signature_validator"
		}
		return ""
	}
	sc := val.(*groupCache)

	textHash := hashText(text)

	now := time.Now()

	sc.mu.Lock()
	entry, exists := sc.entries[textHash]
	if !exists {
		sc.mu.Unlock()
		if groupKey == "gemini" {
			return "skip_thought_signature_validator"
		}
		return ""
	}
	if now.Sub(entry.Timestamp) > SignatureCacheTTL {
		delete(sc.entries, textHash)
		sc.mu.Unlock()
		if groupKey == "gemini" {
			return "skip_thought_signature_validator"
		}
		return ""
	}

	// Refresh TTL on access (sliding expiration).
	entry.Timestamp = now
	sc.entries[textHash] = entry
	sc.mu.Unlock()

	return entry.Signature
}

// ClearSignatureCache clears signature cache for a specific model group or all groups.
func ClearSignatureCache(modelName string) {
	if modelName == "" {
		signatureCache.Range(func(key, _ any) bool {
			signatureCache.Delete(key)
			return true
		})
		return
	}
	groupKey := GetModelGroup(modelName)
	signatureCache.Delete(groupKey)
}

// HasValidSignature checks if a signature is valid (non-empty and long enough)
func HasValidSignature(modelName, signature string) bool {
	return (signature != "" && len(signature) >= MinValidSignatureLen) || (signature == "skip_thought_signature_validator" && GetModelGroup(modelName) == "gemini")
}

func GetModelGroup(modelName string) string {
	if strings.Contains(modelName, "gpt") {
		return "gpt"
	} else if strings.Contains(modelName, "claude") {
		return "claude"
	} else if strings.Contains(modelName, "gemini") {
		return "gemini"
	}
	return modelName
}

// ============================================================================
// Thinking Cache - Lưu trữ toàn bộ thinking text + signature theo thinkingID
// ============================================================================

// ThinkingEntry holds cached thinking content with signature
type ThinkingEntry struct {
	ThinkingText string
	Signature    string
	Timestamp    time.Time
}

const (
	// ThinkingCacheTTL là thời gian thinking cache còn hiệu lực (dài hơn signature cache)
	ThinkingCacheTTL = 2 * time.Hour

	// MaxThinkingEntriesPerSession giới hạn số thinking entries mỗi session
	MaxThinkingEntriesPerSession = 100

	// ThinkingIDLen là độ dài của thinkingID (32 hex chars = 128-bit)
	ThinkingIDLen = 32
)

// thinkingCache stores thinking by sessionId -> thinkingId -> ThinkingEntry
var thinkingCache sync.Map

// thinkingSessionCache là inner map type cho thinking cache
type thinkingSessionCache struct {
	mu      sync.RWMutex
	entries map[string]ThinkingEntry
}

// GenerateThinkingID tạo hash-based ID từ thinking text
func GenerateThinkingID(thinkingText string) string {
	h := sha256.Sum256([]byte(thinkingText))
	return hex.EncodeToString(h[:])[:ThinkingIDLen]
}

// getOrCreateThinkingSession gets or creates a thinking session cache
func getOrCreateThinkingSession(sessionID string) *thinkingSessionCache {
	if val, ok := thinkingCache.Load(sessionID); ok {
		return val.(*thinkingSessionCache)
	}
	sc := &thinkingSessionCache{entries: make(map[string]ThinkingEntry)}
	actual, _ := thinkingCache.LoadOrStore(sessionID, sc)
	return actual.(*thinkingSessionCache)
}

// CacheThinking lưu thinking content với signature theo sessionID và thinkingID
func CacheThinking(sessionID, thinkingID, thinkingText, signature string) {
	if sessionID == "" || thinkingID == "" || thinkingText == "" {
		return
	}

	sc := getOrCreateThinkingSession(sessionID)

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Evict expired entries nếu đạt capacity
	if len(sc.entries) >= MaxThinkingEntriesPerSession {
		now := time.Now()
		for key, entry := range sc.entries {
			if now.Sub(entry.Timestamp) > ThinkingCacheTTL {
				delete(sc.entries, key)
			}
		}
		// Nếu vẫn đạt capacity, xóa entries cũ nhất
		if len(sc.entries) >= MaxThinkingEntriesPerSession {
			oldest := make([]struct {
				key string
				ts  time.Time
			}, 0, len(sc.entries))
			for key, entry := range sc.entries {
				oldest = append(oldest, struct {
					key string
					ts  time.Time
				}{key, entry.Timestamp})
			}
			sort.Slice(oldest, func(i, j int) bool {
				return oldest[i].ts.Before(oldest[j].ts)
			})

			toRemove := len(oldest) / 4
			if toRemove < 1 {
				toRemove = 1
			}

			for i := 0; i < toRemove; i++ {
				delete(sc.entries, oldest[i].key)
			}
		}
	}

	sc.entries[thinkingID] = ThinkingEntry{
		ThinkingText: thinkingText,
		Signature:    signature,
		Timestamp:    time.Now(),
	}
}

// GetCachedThinking lấy cached thinking entry theo sessionID và thinkingID
// Trả về nil nếu không tìm thấy hoặc đã expired
func GetCachedThinking(sessionID, thinkingID string) *ThinkingEntry {
	if sessionID == "" || thinkingID == "" {
		return nil
	}

	val, ok := thinkingCache.Load(sessionID)
	if !ok {
		return nil
	}
	sc := val.(*thinkingSessionCache)

	sc.mu.RLock()
	entry, exists := sc.entries[thinkingID]
	sc.mu.RUnlock()

	if !exists {
		return nil
	}

	// Check if expired
	if time.Since(entry.Timestamp) > ThinkingCacheTTL {
		sc.mu.Lock()
		delete(sc.entries, thinkingID)
		sc.mu.Unlock()
		return nil
	}

	return &entry
}

// ClearThinkingCache xóa thinking cache cho một session cụ thể hoặc tất cả sessions
func ClearThinkingCache(sessionID string) {
	if sessionID != "" {
		thinkingCache.Delete(sessionID)
	} else {
		thinkingCache.Range(func(key, _ any) bool {
			thinkingCache.Delete(key)
			return true
		})
	}
}
