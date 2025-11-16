package analysis

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

//
// Core identifiers and event model
//

// ClientID identifies a logical client (IP + coarse UA).
type ClientID struct {
	IP         string // normalized (v4-mapped, etc.)
	UserAgent  string // truncated/normalized UA, if desired
	ClientHint string // optional, e.g., X-Forwarded-For / custom fingerprint
}

// RouteKey identifies a logical route in your system.
type RouteKey struct {
	Host   string // normalized host (maybe authority or upstream logical name)
	Path   string // normalized path template if you do routing (/users/:id -> /users/{id})
	Method string
}

// Outcome is a coarse-grained view of request result.
type Outcome uint8

const (
	Outcome2xx Outcome = iota
	Outcome3xx
	Outcome4xx
	Outcome5xx
	OutcomeNetworkError
	OutcomeOther
)

// ObservedRequest is the normalized unit of observation that all analyzers operate on.
// You can build this from your existing capture object.
type ObservedRequest struct {
	ID          string // capture ID
	Timestamp   time.Time
	Client      ClientID
	Route       RouteKey
	Latency     time.Duration
	StatusCode  int
	Outcome     Outcome
	Method      string
	Scheme      string
	Proto       string
	Path        string
	Query       string
	ReqBytes    int64
	RespBytes   int64
	ReqHeaders  http.Header
	RespHeaders http.Header

	// Network / TLS metadata (optional but useful for some analyses).
	TLSState *tls.ConnectionState
	LocalIP  net.IP
	RemoteIP net.IP

	// If the proxy saw a transport-level error.
	TransportErr error

	//Some other useful fields
	TLS        TLSSignature
	ServerAddr string
	IsGRPC     bool
}

//
// Analyzer interface + fan-out registry
//

// Analyzer is the generic interface for all analysis modules.
type Analyzer interface {
	OnRequest(ev *ObservedRequest)
}

// Registry fans out events to multiple analyzers.
type Registry struct {
	analyzers []Analyzer
}

func NewRegistry(analyzers ...Analyzer) *Registry {
	return &Registry{analyzers: analyzers}
}

func (r *Registry) OnRequest(ev *ObservedRequest) {
	for _, a := range r.analyzers {
		a.OnRequest(ev)
	}
}

// QuantileEstimate is a minimal struct for latency quantiles.
// If you use HDR histograms or t-digests, you can wrap them here.
type QuantileEstimate struct {
	P50 time.Duration
	P90 time.Duration
	P99 time.Duration
}

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

// ClientFingerprint aggregates header ordering, UA, TLS, etc.
type ClientFingerprint struct {
	UserAgent     string
	HeaderOrder   []string // ordered list of header names
	TLSSignature  TLSSignature
	FirstSeen     time.Time
	LastSeen      time.Time
	ObservationCt int64
}

// ClientFingerprintAnalyzer tracks per-client fingerprint evolution.
type ClientFingerprintAnalyzer struct {
	ByClient map[ClientID]*ClientFingerprint
}

func NewClientFingerprintAnalyzer() *ClientFingerprintAnalyzer {
	return &ClientFingerprintAnalyzer{
		ByClient: make(map[ClientID]*ClientFingerprint),
	}
}

func (c *ClientFingerprintAnalyzer) OnRequest(ev *ObservedRequest) {
	// Derive fingerprint from ev and compare to previous.
}

//
// 8. Auth / cookie header stability
//

// AuthState tracks stability of auth-related headers and cookie structure.
type AuthState struct {
	LastUpdated         time.Time
	AuthHeaderPresent   bool
	AuthHeaderLength    int
	CookieKeySet        map[string]struct{}
	CookieKeySetVersion uint64 // increment when structure changes
	ChangeCount         int64
}

// AuthStabilityAnalyzer is keyed per ClientID.
type AuthStabilityAnalyzer struct {
	ByClient map[ClientID]*AuthState
}

func NewAuthStabilityAnalyzer() *AuthStabilityAnalyzer {
	return &AuthStabilityAnalyzer{
		ByClient: make(map[ClientID]*AuthState),
	}
}

func (a *AuthStabilityAnalyzer) OnRequest(ev *ObservedRequest) {
	// Extract Authorization header + cookie keys and compare to previous.
}

//
// 9. Response entropy / content-type drift
//

// EntropyStats stores simple entropy and drift tracking.
type EntropyStats struct {
	Count             int64
	AvgEntropy        float64
	LastEntropy       float64
	ContentTypeCounts map[string]int64
	LastUpdated       time.Time
}

// EntropyAnalyzer is keyed by RouteKey.
type EntropyAnalyzer struct {
	ByRoute map[RouteKey]*EntropyStats
}

func NewEntropyAnalyzer() *EntropyAnalyzer {
	return &EntropyAnalyzer{
		ByRoute: make(map[RouteKey]*EntropyStats),
	}
}

func (e *EntropyAnalyzer) OnRequest(ev *ObservedRequest) {
	// Compute approximate entropy from a sample of response body (if you store it) and update.
}

//
// 10. Composed analyzer
//

// NewDefaultRegistry shows how you can wire up a combined analyzer set.
func NewDefaultRegistry() *Registry {
	return NewRegistry(
		NewTemporalAnalyzer(time.Second, 300),
		NewRetryAnalyzer(30*time.Second),
		NewLatencyAnalyzer(),
		NewErrorTransitionAnalyzer(),
		NewSizeAnalyzer(),
		NewClientFingerprintAnalyzer(),
		NewMethodPathAnalyzer(),
		NewAuthStabilityAnalyzer(),
		NewEntropyAnalyzer(),
	)
}

func ClassifyOutcome(status int) Outcome {
	switch {
	case status >= 200 && status < 300:
		return Outcome2xx
	case status >= 300 && status < 400:
		return Outcome3xx
	case status >= 400 && status < 500:
		return Outcome4xx
	case status >= 500:
		return Outcome5xx
	default:
		return OutcomeOther
	}
}
