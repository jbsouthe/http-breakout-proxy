package analysis

import (
	"strings"
	"sync"
	"time"
)

// ResponseProfileState tracks response characteristics per route.
type ResponseProfileState struct {
	FirstSeen time.Time
	LastSeen  time.Time
	Count     int64

	// Content-Type / Content-Encoding distributions.
	ContentTypes     map[string]int64
	ContentEncodings map[string]int64

	PrimaryContentType     string
	ContentTypeChangeCount int64

	HighEntropyCount int64
	LowEntropyCount  int64
}

// ResponseProfileSnapshot is a read-only view for a route.
type ResponseProfileSnapshot struct {
	Route RouteKey `json:"route"`

	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Count     int64     `json:"count"`

	PrimaryContentType     string           `json:"primary_content_type"`
	ContentTypeChangeCount int64            `json:"content_type_change_count"`
	ContentTypes           map[string]int64 `json:"content_types"`
	ContentEncodings       map[string]int64 `json:"content_encodings"`

	HighEntropyCount int64 `json:"high_entropy_count"`
	LowEntropyCount  int64 `json:"low_entropy_count"`

	// Convenience flags.
	HasContentTypeDrift bool `json:"has_content_type_drift"`
	HasEntropyMix       bool `json:"has_entropy_mix"` // both high and low seen
}

// ResponseProfileAnalyzer aggregates response entropy / type drift per route.
type ResponseProfileAnalyzer struct {
	mu      sync.RWMutex
	byRoute map[RouteKey]*ResponseProfileState
}

// NewResponseProfileAnalyzer constructs an empty analyzer.
func NewResponseProfileAnalyzer() *ResponseProfileAnalyzer {
	return &ResponseProfileAnalyzer{
		byRoute: make(map[RouteKey]*ResponseProfileState),
	}
}

// OnRequest ingests an ObservedRequest and updates the response profile.
func (a *ResponseProfileAnalyzer) OnRequest(ev *ObservedRequest) {
	if ev == nil {
		return
	}

	now := ev.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	ct := ""
	ce := ""
	if ev.RespHeaders != nil {
		ct = strings.TrimSpace(ev.RespHeaders.Get("Content-Type"))
		ce = strings.TrimSpace(ev.RespHeaders.Get("Content-Encoding"))
	}
	ctNorm := normalizeContentType(ct)
	ceNorm := strings.ToLower(ce)

	// Heuristic entropy classification.
	isHighEntropy := classifyEntropy(ctNorm, ceNorm, ev.RespBytes)

	a.mu.Lock()
	defer a.mu.Unlock()

	st, ok := a.byRoute[ev.Route]
	if !ok {
		st = &ResponseProfileState{
			ContentTypes:     make(map[string]int64),
			ContentEncodings: make(map[string]int64),
		}
		st.FirstSeen = now
		a.byRoute[ev.Route] = st
	}

	st.LastSeen = now
	st.Count++

	if ctNorm != "" {
		st.ContentTypes[ctNorm]++
	}
	if ceNorm != "" {
		st.ContentEncodings[ceNorm]++
	}

	// Track primary content-type and drift.
	newCT := ctNorm
	if newCT == "" {
		newCT = "<none>"
	}

	if st.Count == 1 {
		st.PrimaryContentType = newCT
	} else if newCT != st.PrimaryContentType {
		st.ContentTypeChangeCount++
		st.PrimaryContentType = newCT
	}

	// Entropy counts.
	if isHighEntropy {
		st.HighEntropyCount++
	} else {
		st.LowEntropyCount++
	}
}

// Snapshot returns per-route response profile snapshots.
//
// minCount: if > 0, only routes with Count >= minCount are included.
// minChanges: if > 0, only routes with ContentTypeChangeCount >= minChanges
//
//	OR with both high and low entropy observed are included.
func (a *ResponseProfileAnalyzer) Snapshot(minCount, minChanges int64) []ResponseProfileSnapshot {
	if a == nil {
		return nil
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]ResponseProfileSnapshot, 0, len(a.byRoute))
	for route, st := range a.byRoute {
		if minCount > 0 && st.Count < minCount {
			continue
		}

		hasCTDrift := st.ContentTypeChangeCount > 0
		hasEntropyMix := st.HighEntropyCount > 0 && st.LowEntropyCount > 0

		if minChanges > 0 && !hasCTDrift && !hasEntropyMix {
			continue
		}

		ctCopy := make(map[string]int64, len(st.ContentTypes))
		for v, c := range st.ContentTypes {
			ctCopy[v] = c
		}

		ceCopy := make(map[string]int64, len(st.ContentEncodings))
		for v, c := range st.ContentEncodings {
			ceCopy[v] = c
		}

		snap := ResponseProfileSnapshot{
			Route: route,

			FirstSeen: st.FirstSeen,
			LastSeen:  st.LastSeen,
			Count:     st.Count,

			PrimaryContentType:     st.PrimaryContentType,
			ContentTypeChangeCount: st.ContentTypeChangeCount,
			ContentTypes:           ctCopy,
			ContentEncodings:       ceCopy,

			HighEntropyCount: st.HighEntropyCount,
			LowEntropyCount:  st.LowEntropyCount,

			HasContentTypeDrift: hasCTDrift,
			HasEntropyMix:       hasEntropyMix,
		}
		out = append(out, snap)
	}

	return out
}

// normalizeContentType returns the lowercase media type without parameters.
func normalizeContentType(raw string) string {
	if raw == "" {
		return ""
	}
	semi := strings.Index(raw, ";")
	if semi >= 0 {
		raw = raw[:semi]
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

// classifyEntropy is a coarse heuristic for "high entropy" responses.
func classifyEntropy(ct, ce string, bytes int64) bool {
	// Obvious binary / compressed types.
	if strings.HasPrefix(ct, "image/") ||
		strings.HasPrefix(ct, "video/") ||
		strings.HasPrefix(ct, "audio/") {
		return true
	}

	switch ct {
	case "application/octet-stream",
		"application/pdf",
		"application/zip",
		"application/x-gzip",
		"application/x-protobuf",
		"application/grpc",
		"application/grpc+proto":
		return true
	}

	// Compressed encodings are likely high-entropy on the wire.
	if ce == "gzip" || ce == "br" || ce == "deflate" {
		// Treat larger bodies as "high entropy".
		if bytes > 0 {
			return true
		}
	}

	// Obvious text / structured types.
	if strings.HasPrefix(ct, "text/") {
		return false
	}

	switch ct {
	case "application/json",
		"application/xml",
		"application/xhtml+xml",
		"application/x-www-form-urlencoded":
		return false
	}

	// Unknown: default to "low entropy" unless clearly binary.
	return false
}

// ResponseProfile returns the ResponseProfileAnalyzer registered in this registry, if any.
func (r *Registry) ResponseProfile() *ResponseProfileAnalyzer {
	if r == nil {
		return nil
	}
	for _, a := range r.analyzers {
		if rp, ok := a.(*ResponseProfileAnalyzer); ok {
			return rp
		}
	}
	return nil
}
