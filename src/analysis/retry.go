package analysis

import (
	"sync"
	"time"
)

// RetryKey groups "similar" requests for retry analysis.
// All fields are comparable so this can be used as a map key.
type RetryKey struct {
	Client ClientID
	Method string
	Host   string
	Path   string
	Query  string
}

// RetryState tracks retry statistics for a given key.
type RetryState struct {
	LastTimestamp time.Time
	Count         int64
	LastStatus    int
	LastOutcome   Outcome
}

// RetryAnalyzer detects bursts of repeated requests for the same RetryKey
// within a configurable time window.
type RetryAnalyzer struct {
	mu     sync.RWMutex
	Window time.Duration
	byKey  map[RetryKey]*RetryState
}

// NewRetryAnalyzer constructs a RetryAnalyzer with the given time window.
// Any two matching requests (same RetryKey) whose timestamps differ by <= Window
// are considered part of the same retry "burst".
func NewRetryAnalyzer(window time.Duration) *RetryAnalyzer {
	if window <= 0 {
		window = 30 * time.Second
	}
	return &RetryAnalyzer{
		Window: window,
		byKey:  make(map[RetryKey]*RetryState),
	}
}

// OnRequest ingests an ObservedRequest and updates the retry state.
func (a *RetryAnalyzer) OnRequest(ev *ObservedRequest) {
	if ev == nil {
		return
	}

	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	key := RetryKey{
		Client: ev.Client,
		Method: ev.Method,
		Host:   ev.Route.Host,
		Path:   ev.Route.Path,
		Query:  ev.Query,
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	st, ok := a.byKey[key]
	if !ok {
		a.byKey[key] = &RetryState{
			LastTimestamp: ts,
			Count:         1,
			LastStatus:    ev.StatusCode,
			LastOutcome:   ev.Outcome,
		}
		return
	}

	// If this request is "close enough" in time to the last one, increment
	// the retry counter; otherwise, start a new burst with Count=1.
	if ts.Sub(st.LastTimestamp) <= a.Window {
		st.Count++
	} else {
		st.Count = 1
	}
	st.LastTimestamp = ts
	st.LastStatus = ev.StatusCode
	st.LastOutcome = ev.Outcome
}

// RetrySnapshot is a read-only view of a hot retry key.
type RetrySnapshot struct {
	Client        ClientID
	Method        string
	Host          string
	Path          string
	Query         string
	Count         int64
	LastTimestamp time.Time
	LastStatus    int
	LastOutcome   Outcome
}

// Snapshot returns all keys that currently look like retries, i.e. keys whose
// Count >= minCount and whose last observation is still within the window.
func (a *RetryAnalyzer) Snapshot(minCount int64) []RetrySnapshot {
	if a == nil {
		return nil
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	now := time.Now()
	out := make([]RetrySnapshot, 0, len(a.byKey))

	for key, st := range a.byKey {
		if st.Count < minCount {
			continue
		}
		if now.Sub(st.LastTimestamp) > a.Window {
			// stale burst, effectively expired
			continue
		}
		out = append(out, RetrySnapshot{
			Client:        key.Client,
			Method:        key.Method,
			Host:          key.Host,
			Path:          key.Path,
			Query:         key.Query,
			Count:         st.Count,
			LastTimestamp: st.LastTimestamp,
			LastStatus:    st.LastStatus,
			LastOutcome:   st.LastOutcome,
		})
	}

	return out
}

// Retry returns the RetryAnalyzer registered in this registry, if any.
func (r *Registry) Retry() *RetryAnalyzer {
	if r == nil {
		return nil
	}
	for _, a := range r.analyzers {
		if ra, ok := a.(*RetryAnalyzer); ok {
			return ra
		}
	}
	return nil
}
