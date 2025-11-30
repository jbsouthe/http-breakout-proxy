package analysis

import "testing"

func TestErrorTransitionAnalyzerConsecutiveCountersAndTransitions(t *testing.T) {
	a := NewErrorTransitionAnalyzer()

	client := ClientID{IP: "1.2.3.4"}
	route := RouteKey{Host: "example.com", Path: "/api", Method: "GET"}

	mk := func(out Outcome) *ObservedRequest {
		return &ObservedRequest{
			Client:  client,
			Route:   route,
			Outcome: out,
		}
	}

	// Sequence:
	// 5xx, 5xx, network_error, 2xx, 4xx
	a.OnRequest(mk(Outcome5xx))
	a.OnRequest(mk(Outcome5xx))
	a.OnRequest(mk(OutcomeNetworkError))
	a.OnRequest(mk(Outcome2xx))
	a.OnRequest(mk(Outcome4xx))

	snaps := a.Snapshot(0)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	snap := snaps[0]

	if snap.Client != client {
		t.Fatalf("unexpected client: %#v", snap.Client)
	}
	if snap.LastOutcome != Outcome4xx {
		t.Fatalf("LastOutcome = %v, want %v", snap.LastOutcome, Outcome4xx)
	}
	// After final 4xx, only Consecutive4xx should be non-zero.
	if snap.Consecutive5xx != 0 {
		t.Fatalf("Consecutive5xx = %d, want 0", snap.Consecutive5xx)
	}
	if snap.Consecutive4xx != 1 {
		t.Fatalf("Consecutive4xx = %d, want 1", snap.Consecutive4xx)
	}
	if snap.ConsecutiveErrors != 0 {
		t.Fatalf("ConsecutiveErrors = %d, want 0", snap.ConsecutiveErrors)
	}

	// Examine transition matrix.
	tr := snap.Transitions
	if tr == nil {
		t.Fatalf("Transitions map is nil")
	}
	if tr[Outcome5xx][Outcome5xx] != 1 {
		t.Fatalf("expected 5xx->5xx = 1, got %d", tr[Outcome5xx][Outcome5xx])
	}
	if tr[Outcome5xx][OutcomeNetworkError] != 1 {
		t.Fatalf("expected 5xx->network_error = 1, got %d", tr[Outcome5xx][OutcomeNetworkError])
	}
	if tr[OutcomeNetworkError][Outcome2xx] != 1 {
		t.Fatalf("expected network_error->2xx = 1, got %d", tr[OutcomeNetworkError][Outcome2xx])
	}
	if tr[Outcome2xx][Outcome4xx] != 1 {
		t.Fatalf("expected 2xx->4xx = 1, got %d", tr[Outcome2xx][Outcome4xx])
	}
}

func TestErrorTransitionAnalyzerMinErrorsFilter(t *testing.T) {
	a := NewErrorTransitionAnalyzer()

	c1 := ClientID{IP: "1.1.1.1"}
	c2 := ClientID{IP: "2.2.2.2"}
	route := RouteKey{Host: "example.com", Path: "/api", Method: "GET"}

	// Client 1 has two consecutive 5xx.
	a.OnRequest(&ObservedRequest{Client: c1, Route: route, Outcome: Outcome5xx})
	a.OnRequest(&ObservedRequest{Client: c1, Route: route, Outcome: Outcome5xx})

	// Client 2 has only one error.
	a.OnRequest(&ObservedRequest{Client: c2, Route: route, Outcome: Outcome5xx})

	snaps := a.Snapshot(2)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot with minErrors=2, got %d", len(snaps))
	}
	if snaps[0].Client != c1 {
		t.Fatalf("expected client with IP %q, got %#v", c1.IP, snaps[0].Client)
	}
}
