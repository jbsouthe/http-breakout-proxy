package main // analysis_adapter.go
import (
	"HTTPBreakoutBox/src/analysis"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// temporalBucketDTO is a JSON-serializable representation of a temporal bucket.
type temporalBucketDTO struct {
	WindowStart   time.Time `json:"window_start"` // RFC3339 by default
	Count         int64     `json:"count"`
	MeanLatencyMs float64   `json:"mean_latency_ms"`
	StdDevMs      float64   `json:"stddev_latency_ms"`
	MinLatencyMs  float64   `json:"min_latency_ms"`
	MaxLatencyMs  float64   `json:"max_latency_ms"`
}

// handleTemporalMetrics exposes the temporal distribution as JSON.
func handleTemporalMetrics(w http.ResponseWriter, r *http.Request) {
	if analysisRegistry == nil {
		http.Error(w, "analysis registry not initialized", http.StatusServiceUnavailable)
		return
	}

	ta := analysisRegistry.Temporal()
	if ta == nil {
		http.Error(w, "temporal analyzer not available", http.StatusServiceUnavailable)
		return
	}

	buckets := ta.Snapshot()

	out := make([]temporalBucketDTO, 0, len(buckets))

	for _, b := range buckets {
		if b.WindowStart.IsZero() || b.Count == 0 {
			// Skip completely empty / never-used buckets
			continue
		}

		// Mean latency in ns
		meanNs := float64(b.TotalLatency) / float64(b.Count)

		// Variance in ns^2: E[X^2] - (E[X])^2
		varNs2 := b.SquaredLatency/float64(b.Count) - meanNs*meanNs
		if varNs2 < 0 {
			varNs2 = 0 // numeric guard
		}
		stdNs := math.Sqrt(varNs2)

		dto := temporalBucketDTO{
			WindowStart:   b.WindowStart,
			Count:         b.Count,
			MeanLatencyMs: meanNs / 1e6,
			StdDevMs:      stdNs / 1e6,
			MinLatencyMs:  float64(b.MinLatency) / 1e6,
			MaxLatencyMs:  float64(b.MaxLatency) / 1e6,
		}

		out = append(out, dto)
	}

	w.Header().Set("Content-Type", "application/json")
	// You can tune this to indent or not; this is fine for now.
	if err := json.NewEncoder(w).Encode(out); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// toHTTPHeader converts a map[string][]string to http.Header.
func toHTTPHeader(m map[string][]string) http.Header {
	if m == nil {
		return nil
	}
	h := make(http.Header, len(m))
	for k, v := range m {
		// copy slice so stored headers can't be mutated externally
		cp := make([]string, len(v))
		copy(cp, v)
		h[k] = cp
	}
	return h
}

// observedFromCapture reconstructs an ObservedRequest from a stored Capture.
// Because this runs at startup from disk, some fields (TLSState, client IP)
// will be unavailable or zero-valued.
func observedFromCapture(c Capture) *analysis.ObservedRequest {
	if c.URL == "" {
		return nil
	}

	u, err := url.Parse(c.URL)
	if err != nil {
		// skip malformed/legacy captures
		return nil
	}

	// Choose best latency signal: TotalMs if present, fallback to DurationMs.
	var latency time.Duration
	switch {
	case c.TotalMs > 0:
		latency = time.Duration(c.TotalMs) * time.Millisecond
	case c.DurationMs > 0:
		latency = time.Duration(c.DurationMs) * time.Millisecond
	default:
		latency = 0
	}

	// Prefer explicit wire/body counters if present.
	reqBytes := c.RequestBytesTotal
	if reqBytes == 0 && c.RequestBodyBytes > 0 {
		reqBytes = c.RequestBodyBytes
	}

	respBytes := c.ResponseBytesTotal
	if respBytes == 0 && c.ResponseBodyBytes > 0 {
		respBytes = c.ResponseBodyBytes
	}

	client := analysis.ClientID{
		IP:         c.ClientIP,
		UserAgent:  c.UserAgent,
		ClientHint: c.XForwardedFor,
	}

	route := analysis.RouteKey{
		Host:   u.Host,
		Path:   u.Path,
		Method: c.Method,
	}

	tlsSig := analysis.TLSSignature{
		Version:      c.TLSVersion,
		CipherSuite:  c.TLSCipherSuite,
		ALPNProtocol: c.TLSALPN,
		ServerName:   c.TLSServerName,
		Resumed:      c.TLSResumed,
	}

	ev := &analysis.ObservedRequest{
		ID: strconv.FormatInt(c.ID, 10),

		Timestamp: c.Time,

		Client: client,
		Route:  route,

		Latency:    latency,
		StatusCode: c.ResponseStatus,
		Outcome:    analysis.ClassifyOutcome(c.ResponseStatus),

		Method: c.Method,
		Proto:  c.Proto,
		Scheme: c.Scheme,

		Path:  u.Path,
		Query: u.RawQuery,

		ReqBytes:  reqBytes,
		RespBytes: respBytes,

		ReqHeaders:  toHTTPHeader(c.RequestHeaders),
		RespHeaders: toHTTPHeader(c.ResponseHeaders),

		TLS:        tlsSig,
		ServerAddr: c.ServerAddr,

		IsGRPC: c.IsGRPC,
	}

	return ev
}

// RebuildAnalysisFromCaptures replays historical captures through the analyzers.
func RebuildAnalysisFromCaptures(reg *analysis.Registry, captures []Capture) {
	if reg == nil {
		return
	}
	for _, c := range captures {
		ev := observedFromCapture(c)
		if ev == nil {
			continue
		}
		reg.OnRequest(ev)
	}
}
