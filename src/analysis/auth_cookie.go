package analysis

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// AuthCookieKey groups observations for a "logical client" and host.
// You can widen/narrow this later if needed.
type AuthCookieKey struct {
	Client ClientID
	Host   string
}

// AuthCookieState holds stability metrics for auth/cookie headers.
type AuthCookieState struct {
	FirstSeen     time.Time
	LastSeen      time.Time
	TotalRequests int64

	// Authorization presence / value stability.
	AuthPresentCount int64
	AuthMissingCount int64

	// For privacy and cardinality, we do not store the full tokens;
	// we only track the raw header string counts.
	// If you prefer, you can truncate or hash.
	AuthValues map[string]int64

	// Tracks how often auth presence/value "flaps".
	CurrentAuthValue string
	AuthChangeCount  int64 // increments when value transitions (including present<->missing)

	// Cookie key-set stability (structure, not values).
	// We model the cookie "shape" as a canonical pattern string, e.g. "sid|csrf|analytics".
	CurrentCookiePattern     string
	CookiePatternChangeCount int64
	CookiePatterns           map[string]int64
}

// AuthCookieSnapshot is a read-only view suitable for export.
type AuthCookieSnapshot struct {
	Client ClientID `json:"client"`
	Host   string   `json:"host"`

	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`

	TotalRequests int64 `json:"total_requests"`

	AuthPresentCount int64            `json:"auth_present_count"`
	AuthMissingCount int64            `json:"auth_missing_count"`
	AuthValues       map[string]int64 `json:"auth_values"` // header -> count
	AuthChangeCount  int64            `json:"auth_change_count"`

	CurrentCookiePattern     string           `json:"current_cookie_pattern"`
	CookiePatternChangeCount int64            `json:"cookie_pattern_change_count"`
	CookiePatterns           map[string]int64 `json:"cookie_patterns"`

	// Convenience flags.
	HasAuthFlapping bool `json:"has_auth_flapping"`
	HasCookieDrift  bool `json:"has_cookie_drift"`
}

// AuthCookieAnalyzer maintains auth/cookie stability state keyed by (ClientID, Host).
type AuthCookieAnalyzer struct {
	mu    sync.RWMutex
	byKey map[AuthCookieKey]*AuthCookieState
}

// NewAuthCookieAnalyzer constructs an empty analyzer.
func NewAuthCookieAnalyzer() *AuthCookieAnalyzer {
	return &AuthCookieAnalyzer{
		byKey: make(map[AuthCookieKey]*AuthCookieState),
	}
}

// OnRequest ingests an ObservedRequest and updates the auth/cookie state.
func (a *AuthCookieAnalyzer) OnRequest(ev *ObservedRequest) {
	if ev == nil {
		return
	}

	now := ev.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	key := AuthCookieKey{
		Client: ev.Client,
		Host:   ev.Route.Host,
	}

	authVal := ""
	if ev.ReqHeaders != nil {
		authVal = strings.TrimSpace(ev.ReqHeaders.Get("Authorization"))
	}
	authPresent := authVal != ""

	cookiePattern := deriveCookiePattern(ev.ReqHeaders)

	a.mu.Lock()
	defer a.mu.Unlock()

	st, ok := a.byKey[key]
	if !ok {
		st = &AuthCookieState{
			AuthValues:     make(map[string]int64),
			CookiePatterns: make(map[string]int64),
		}
		st.FirstSeen = now
		a.byKey[key] = st
	}

	st.LastSeen = now
	st.TotalRequests++

	// --- Authorization stability tracking ---
	if authPresent {
		st.AuthPresentCount++
		st.AuthValues[authVal]++
	} else {
		st.AuthMissingCount++
	}

	// Treat "missing" as a value as well, so present<->missing flips are counted.
	newAuthValue := authVal
	if !authPresent {
		newAuthValue = "<none>"
	}

	if st.TotalRequests == 1 {
		st.CurrentAuthValue = newAuthValue
	} else if newAuthValue != st.CurrentAuthValue {
		st.AuthChangeCount++
		st.CurrentAuthValue = newAuthValue
	}

	// --- Cookie pattern stability tracking ---
	if cookiePattern != "" {
		st.CookiePatterns[cookiePattern]++
	}

	if st.TotalRequests == 1 {
		st.CurrentCookiePattern = cookiePattern
	} else if cookiePattern != st.CurrentCookiePattern {
		st.CookiePatternChangeCount++
		st.CurrentCookiePattern = cookiePattern
	}
}

// deriveCookiePattern normalizes the cookie key set into a canonical pattern
// string like "csrf|session|tracking". Missing/empty yields "".
func deriveCookiePattern(h http.Header) string {
	if h == nil {
		return ""
	}
	raws := h.Values("Cookie")
	if len(raws) == 0 {
		return ""
	}

	keySet := make(map[string]struct{})

	for _, raw := range raws {
		parts := strings.Split(raw, ";")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if eq := strings.Index(p, "="); eq > 0 {
				key := p[:eq]
				keySet[key] = struct{}{}
			}
		}
	}

	if len(keySet) == 0 {
		return ""
	}

	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, "|")
}

// Snapshot returns per-(client, host) auth/cookie stability snapshots.
//
// minRequests: if > 0, only keys with TotalRequests >= minRequests are included.
// minChanges:  if > 0, only keys with (AuthChangeCount + CookiePatternChangeCount) >= minChanges
//
//	are included.
func (a *AuthCookieAnalyzer) Snapshot(minRequests, minChanges int64) []AuthCookieSnapshot {
	if a == nil {
		return nil
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]AuthCookieSnapshot, 0, len(a.byKey))
	for key, st := range a.byKey {
		if minRequests > 0 && st.TotalRequests < minRequests {
			continue
		}
		totalChanges := st.AuthChangeCount + st.CookiePatternChangeCount
		if minChanges > 0 && totalChanges < minChanges {
			continue
		}

		// Copy maps to keep internal state immutable to callers.
		authCopy := make(map[string]int64, len(st.AuthValues))
		for v, c := range st.AuthValues {
			authCopy[v] = c
		}
		cookieCopy := make(map[string]int64, len(st.CookiePatterns))
		for p, c := range st.CookiePatterns {
			cookieCopy[p] = c
		}

		snap := AuthCookieSnapshot{
			Client: key.Client,
			Host:   key.Host,

			FirstSeen: st.FirstSeen,
			LastSeen:  st.LastSeen,

			TotalRequests: st.TotalRequests,

			AuthPresentCount: st.AuthPresentCount,
			AuthMissingCount: st.AuthMissingCount,
			AuthValues:       authCopy,
			AuthChangeCount:  st.AuthChangeCount,

			CurrentCookiePattern:     st.CurrentCookiePattern,
			CookiePatternChangeCount: st.CookiePatternChangeCount,
			CookiePatterns:           cookieCopy,

			HasAuthFlapping: st.AuthChangeCount > 0,
			HasCookieDrift:  st.CookiePatternChangeCount > 0,
		}
		out = append(out, snap)
	}

	return out
}

// AuthCookie returns the AuthCookieAnalyzer registered in this registry, if any.
func (r *Registry) AuthCookie() *AuthCookieAnalyzer {
	if r == nil {
		return nil
	}
	for _, a := range r.analyzers {
		if ac, ok := a.(*AuthCookieAnalyzer); ok {
			return ac
		}
	}
	return nil
}
