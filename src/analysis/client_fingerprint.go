package analysis

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

//
// 6. Client fingerprint / UA drift
//

// TLSSignature captures a coarse TLS fingerprint.
type TLSSignature struct {
	Version       uint16
	CipherSuite   uint16
	ALPNProtocol  string
	ServerName    string
	Resumed       bool
	SupportsEarly bool
}

// fingerprintKey groups observations by "logical client" without baking in UA.
// That way UA changes are visible as drift instead of new keys.
type fingerprintKey struct {
	IP         string
	ClientHint string
}

// ClientFingerprint aggregates fingerprint evolution for a single logical client.
type ClientFingerprint struct {
	FirstSeen        time.Time
	LastSeen         time.Time
	ObservationCount int64

	// UA history.
	CurrentUA     string
	UAChangeCount int64
	UserAgents    map[string]int64 // UA -> count

	// TLS history.
	CurrentTLS     TLSSignature
	TLSChangeCount int64
	TLSSignatures  map[TLSSignature]int64 // fingerprint -> count

	// Header key set (coarse approximation of header ordering/shape).
	HeaderKeyCounts map[string]int64 // header name (canonical) -> count
}

// ClientFingerprintSnapshot is a read-only view suitable for export.
type ClientFingerprintSnapshot struct {
	ClientIP         string    `json:"client_ip"`
	ClientHint       string    `json:"client_hint"`
	FirstSeen        time.Time `json:"first_seen"`
	LastSeen         time.Time `json:"last_seen"`
	ObservationCount int64     `json:"observation_count"`

	CurrentUA     string           `json:"current_ua"`
	UAChangeCount int64            `json:"ua_change_count"`
	UserAgents    map[string]int64 `json:"user_agents"`

	CurrentTLS     TLSSignature           `json:"current_tls"`
	TLSChangeCount int64                  `json:"tls_change_count"`
	TLSSignatures  map[TLSSignature]int64 `json:"tls_signatures"`

	HeaderKeyCounts map[string]int64 `json:"header_key_counts"`

	// Convenience flags for drift/anomaly detection.
	HasUADrift  bool `json:"has_ua_drift"`
	HasTLSDrift bool `json:"has_tls_drift"`
}

// ClientFingerprintAnalyzer tracks per-client fingerprint evolution.
type ClientFingerprintAnalyzer struct {
	mu       sync.RWMutex
	byClient map[fingerprintKey]*ClientFingerprint
}

// NewClientFingerprintAnalyzer constructs an empty analyzer.
func NewClientFingerprintAnalyzer() *ClientFingerprintAnalyzer {
	return &ClientFingerprintAnalyzer{
		byClient: make(map[fingerprintKey]*ClientFingerprint),
	}
}

// OnRequest ingests an ObservedRequest and updates fingerprint state.
func (a *ClientFingerprintAnalyzer) OnRequest(ev *ObservedRequest) {
	if ev == nil {
		return
	}

	now := ev.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	key := fingerprintKey{
		IP:         ev.Client.IP,
		ClientHint: ev.Client.ClientHint,
	}

	ua := strings.TrimSpace(ev.Client.UserAgent)
	tls := ev.TLS

	a.mu.Lock()
	defer a.mu.Unlock()

	fp, ok := a.byClient[key]
	if !ok {
		fp = &ClientFingerprint{
			FirstSeen:       now,
			UserAgents:      make(map[string]int64),
			TLSSignatures:   make(map[TLSSignature]int64),
			HeaderKeyCounts: make(map[string]int64),
		}
		a.byClient[key] = fp
	}

	fp.LastSeen = now
	fp.ObservationCount++

	// UA drift
	if fp.ObservationCount == 1 {
		fp.CurrentUA = ua
	} else if ua != "" && ua != fp.CurrentUA {
		fp.UAChangeCount++
		fp.CurrentUA = ua
	}
	if ua != "" {
		fp.UserAgents[ua]++
	}

	// TLS drift (only if we got a non-zero TLS signature)
	if tls.Version != 0 || tls.CipherSuite != 0 || tls.ALPNProtocol != "" || tls.ServerName != "" {
		if fp.ObservationCount == 1 {
			fp.CurrentTLS = tls
		} else if !tlsEqual(fp.CurrentTLS, tls) {
			fp.TLSChangeCount++
			fp.CurrentTLS = tls
		}
		fp.TLSSignatures[tls]++
	}

	// Header shape: count which headers tend to appear
	recordHeaderKeys(fp.HeaderKeyCounts, ev.ReqHeaders)
}

// tlsEqual compares two TLSSignatures.
func tlsEqual(a, b TLSSignature) bool {
	return a.Version == b.Version &&
		a.CipherSuite == b.CipherSuite &&
		a.ALPNProtocol == b.ALPNProtocol &&
		a.ServerName == b.ServerName &&
		a.Resumed == b.Resumed
}

// recordHeaderKeys increments counts for header keys present in h.
func recordHeaderKeys(m map[string]int64, h http.Header) {
	if h == nil {
		return
	}
	for k := range h {
		if k == "" {
			continue
		}
		// Normalize to canonical header key representation.
		ck := http.CanonicalHeaderKey(k)
		m[ck]++
	}
}

// Snapshot returns client fingerprint snapshots.
//
// minChanges: minimum (UAChangeCount + TLSChangeCount) to include.
// If minChanges == 0, all clients are returned.
func (a *ClientFingerprintAnalyzer) Snapshot(minChanges int64) []ClientFingerprintSnapshot {
	if a == nil {
		return nil
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]ClientFingerprintSnapshot, 0, len(a.byClient))
	for key, fp := range a.byClient {
		totalChanges := fp.UAChangeCount + fp.TLSChangeCount
		if minChanges > 0 && totalChanges < minChanges {
			continue
		}

		// Copy maps to keep internal state immutable to callers.
		uaCopy := make(map[string]int64, len(fp.UserAgents))
		for s, c := range fp.UserAgents {
			uaCopy[s] = c
		}

		tlsCopy := make(map[TLSSignature]int64, len(fp.TLSSignatures))
		for sig, c := range fp.TLSSignatures {
			tlsCopy[sig] = c
		}

		headerCopy := make(map[string]int64, len(fp.HeaderKeyCounts))
		for h, c := range fp.HeaderKeyCounts {
			headerCopy[h] = c
		}

		snap := ClientFingerprintSnapshot{
			ClientIP:         key.IP,
			ClientHint:       key.ClientHint,
			FirstSeen:        fp.FirstSeen,
			LastSeen:         fp.LastSeen,
			ObservationCount: fp.ObservationCount,

			CurrentUA:     fp.CurrentUA,
			UAChangeCount: fp.UAChangeCount,
			UserAgents:    uaCopy,

			CurrentTLS:     fp.CurrentTLS,
			TLSChangeCount: fp.TLSChangeCount,
			TLSSignatures:  tlsCopy,

			HeaderKeyCounts: headerCopy,

			HasUADrift:  fp.UAChangeCount > 0,
			HasTLSDrift: fp.TLSChangeCount > 0,
		}

		out = append(out, snap)
	}
	return out
}

// ClientFingerprint returns the ClientFingerprintAnalyzer registered in this registry, if any.
func (r *Registry) ClientFingerprint() *ClientFingerprintAnalyzer {
	if r == nil {
		return nil
	}
	for _, a := range r.analyzers {
		if cfa, ok := a.(*ClientFingerprintAnalyzer); ok {
			return cfa
		}
	}
	return nil
}
