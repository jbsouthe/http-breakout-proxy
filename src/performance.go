package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

type phases struct {
	startRT          time.Time // RoundTrip start
	dnsStart, dnsEnd time.Time
	conStart, conEnd time.Time
	tlsStart, tlsEnd time.Time
	wroteReq         time.Time
	firstByte        time.Time
	done             time.Time

	serverAddr string
	reused     bool
	h2         bool
}

// req pointer -> phases
var phaseMap sync.Map // map[string]*phases

func millis(a, b time.Time) int64 {
	if a.IsZero() || b.IsZero() {
		return 0
	}
	return b.Sub(a).Milliseconds()
}

type tracingRT struct{ base http.RoundTripper }

func (t tracingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	p := &phases{startRT: time.Now()}
	key := reqKey(req)
	phaseMap.Store(key, p)

	ct := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { p.dnsStart = time.Now() },
		DNSDone:  func(httptrace.DNSDoneInfo) { p.dnsEnd = time.Now() },

		ConnectStart: func(_, _ string) { p.conStart = time.Now() },
		ConnectDone:  func(_, _ string, _ error) { p.conEnd = time.Now() },

		TLSHandshakeStart: func() { p.tlsStart = time.Now() },
		TLSHandshakeDone: func(cs tls.ConnectionState, _ error) {
			p.tlsEnd = time.Now()
			p.h2 = (cs.NegotiatedProtocol == "h2")
		},

		GotConn: func(ci httptrace.GotConnInfo) {
			p.reused = ci.Reused
			if ci.Conn != nil && ci.Conn.RemoteAddr() != nil {
				p.serverAddr = ci.Conn.RemoteAddr().String()
			}
		},

		WroteRequest: func(httptrace.WroteRequestInfo) { p.wroteReq = time.Now() },

		GotFirstResponseByte: func() { p.firstByte = time.Now() },
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), ct))

	// Delegate to base
	resp, err := t.base.RoundTrip(req)

	// Mark completion when the body is fully read by caller (OnResponse handler will set p.done)
	// If an error happened before body is available, mark done now.
	if err != nil {
		p.done = time.Now()
	}
	return resp, err
}