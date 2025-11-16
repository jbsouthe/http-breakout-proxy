package main // analysis_adapter.go
import (
	"HTTPBreakoutBox/src/analysis"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

type routeLatencyDTO struct {
	Method      string    `json:"method"`
	Host        string    `json:"host"`
	Path        string    `json:"path"`
	Count       int64     `json:"count"`
	MeanMs      float64   `json:"mean_ms"`
	StdDevMs    float64   `json:"stddev_ms"`
	MinMs       float64   `json:"min_ms"`
	MaxMs       float64   `json:"max_ms"`
	LastUpdated time.Time `json:"last_updated"`
}

// handleRouteLatencyMetrics exposes per-route latency stats.
//
// Optional query params:
//
//	?min=<N>    -> minimum count per route (default 10)
//	?limit=<K>  -> max number of routes to return (default 100)
//
// Routes are sorted by descending MeanMs.
func handleRouteLatencyMetrics(w http.ResponseWriter, r *http.Request) {
	if analysisRegistry == nil {
		http.Error(w, "analysis registry not initialized", http.StatusServiceUnavailable)
		return
	}

	la := analysisRegistry.Latency()
	if la == nil {
		http.Error(w, "latency analyzer not available", http.StatusServiceUnavailable)
		return
	}

	// Parse query params
	q := r.URL.Query()

	minCount := int64(10)
	if s := q.Get("min"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
			minCount = v
		}
	}

	limit := 100
	if s := q.Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			limit = v
		}
	}

	// Snapshot from analyzer
	snap := la.Snapshot(minCount)
	if len(snap) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]routeLatencyDTO{})
		return
	}

	// Convert to DTOs
	dtos := make([]routeLatencyDTO, 0, len(snap))
	for _, s := range snap {
		dto := routeLatencyDTO{
			Method:      s.Route.Method,
			Host:        s.Route.Host,
			Path:        s.Route.Path,
			Count:       s.Count,
			MeanMs:      float64(s.Mean) / 1e6,
			StdDevMs:    float64(s.StdDev) / 1e6,
			MinMs:       float64(s.Min) / 1e6,
			MaxMs:       float64(s.Max) / 1e6,
			LastUpdated: s.LastUpdated,
		}
		dtos = append(dtos, dto)
	}

	// Sort by descending mean latency
	// (you can sort by MaxMs if you prefer tail behavior)
	sort.Slice(dtos, func(i, j int) bool {
		// NaN guard
		if math.IsNaN(dtos[i].MeanMs) {
			return false
		}
		if math.IsNaN(dtos[j].MeanMs) {
			return true
		}
		return dtos[i].MeanMs > dtos[j].MeanMs
	})

	if len(dtos) > limit {
		dtos = dtos[:limit]
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(dtos); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

type retryOutcome string

func mapOutcome(o analysis.Outcome) retryOutcome {
	switch o {
	case analysis.Outcome2xx:
		return "2xx"
	case analysis.Outcome3xx:
		return "3xx"
	case analysis.Outcome4xx:
		return "4xx"
	case analysis.Outcome5xx:
		return "5xx"
	case analysis.OutcomeNetworkError:
		return "network_error"
	default:
		return "other"
	}
}

// RetryDTO is the JSON shape we expose for each hot retry key.
type RetryDTO struct {
	ClientIP      string       `json:"client_ip"`
	UserAgent     string       `json:"user_agent"`
	ClientHint    string       `json:"client_hint"`
	Method        string       `json:"method"`
	Host          string       `json:"host"`
	Path          string       `json:"path"`
	Query         string       `json:"query"`
	Count         int64        `json:"count"`
	LastTimestamp time.Time    `json:"last_timestamp"`
	LastStatus    int          `json:"last_status"`
	LastOutcome   retryOutcome `json:"last_outcome"`
}

// handleRetryMetrics exposes retry/duplicate request info as JSON.
//
// Optional query param: ?min=<N> to require at least N requests in the burst.
// Default is 2 (at least one retry).
func handleRetryMetrics(w http.ResponseWriter, r *http.Request) {
	if analysisRegistry == nil {
		http.Error(w, "analysis registry not initialized", http.StatusServiceUnavailable)
		return
	}

	ra := analysisRegistry.Retry()
	if ra == nil {
		http.Error(w, "retry analyzer not available", http.StatusServiceUnavailable)
		return
	}

	minCount := int64(2) // default: at least one retry
	if s := r.URL.Query().Get("min"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
			minCount = v
		}
	}

	snap := ra.Snapshot(minCount)

	out := make([]RetryDTO, 0, len(snap))
	for _, rs := range snap {
		out = append(out, RetryDTO{
			ClientIP:      rs.Client.IP,
			UserAgent:     rs.Client.UserAgent,
			ClientHint:    rs.Client.ClientHint,
			Method:        rs.Method,
			Host:          rs.Host,
			Path:          rs.Path,
			Query:         rs.Query,
			Count:         rs.Count,
			LastTimestamp: rs.LastTimestamp,
			LastStatus:    rs.LastStatus,
			LastOutcome:   mapOutcome(rs.LastOutcome),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

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
