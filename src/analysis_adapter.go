package main // analysis_adapter.go
import (
	"HTTPBreakoutBox/src/analysis"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

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
