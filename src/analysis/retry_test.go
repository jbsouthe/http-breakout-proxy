package analysis

import (
	"testing"
	"time"
)

func TestRetryAnalyzerCountsWithinWindow(t *testing.T) {
	window := 10 * time.Second
	a := NewRetryAnalyzer(window)

	client := ClientID{IP: "1.2.3.4"}
	route := RouteKey{Host: "example.com", Path: "/api", Method: "GET"}

	now := time.Now()

	ev1 := &ObservedRequest{
		Timestamp:  now.Add(-2 * time.Second),
		Client:     client,
		Route:      route,
		Method:     "GET",
		Query:      "q=1",
		StatusCode: 500,
		Outcome:    Outcome5xx,
	}
	ev2 := &ObservedRequest{
		Timestamp:  now.Add(-1 * time.Second),
		Client:     client,
		Route:      route,
		Method:     "GET",
		Query:      "q=1",
		StatusCode: 500,
		Outcome:    Outcome5xx,
	}

	a.OnRequest(ev1)
	a.OnRequest(ev2)

	snaps := a.Snapshot(2)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot with minCount=2, got %d", len(snaps))
	}
	snap := snaps[0]

	if snap.Count != 2 {
		t.Fatalf("expected Count=2, got %d", snap.Count)
	}
	if snap.Client != client {
		t.Fatalf("unexpected client in snapshot: %#v", snap.Client)
	}
	if snap.Host != route.Host || snap.Path != route.Path || snap.Method != "GET" || snap.Query != "q=1" {
		t.Fatalf("unexpected key in snapshot: %#v", snap)
	}
	if snap.LastStatus != 500 || snap.LastOutcome != Outcome5xx {
		t.Fatalf("unexpected last status/outcome: %d / %v", snap.LastStatus, snap.LastOutcome)
	}
}

func TestRetryAnalyzerBurstResetOutsideWindow(t *testing.T) {
	window := 100 * time.Millisecond
	a := NewRetryAnalyzer(window)

	client := ClientID{IP: "1.2.3.4"}
	route := RouteKey{Host: "example.com", Path: "/api", Method: "GET"}

	base := time.Now()

	// First two requests within the retry window -> same burst.
	a.OnRequest(&ObservedRequest{
		Timestamp:  base,
		Client:     client,
		Route:      route,
		Method:     "GET",
		Query:      "x=1",
		StatusCode: 500,
		Outcome:    Outcome5xx,
	})
	a.OnRequest(&ObservedRequest{
		Timestamp:  base.Add(window / 2),
		Client:     client,
		Route:      route,
		Method:     "GET",
		Query:      "x=1",
		StatusCode: 500,
		Outcome:    Outcome5xx,
	})

	// Third request is “far” in logical time from the second -> new burst.
	a.OnRequest(&ObservedRequest{
		Timestamp:  base.Add(2 * window),
		Client:     client,
		Route:      route,
		Method:     "GET",
		Query:      "x=1",
		StatusCode: 500,
		Outcome:    Outcome5xx,
	})

	snaps := a.Snapshot(1)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	if snaps[0].Count != 1 {
		t.Fatalf("expected Count=1 after burst reset, got %d", snaps[0].Count)
	}
}

func TestRetryAnalyzerKeySeparation(t *testing.T) {
	window := 5 * time.Second
	a := NewRetryAnalyzer(window)

	client := ClientID{IP: "1.2.3.4"}
	route := RouteKey{Host: "example.com", Path: "/api", Method: "GET"}

	now := time.Now()

	// Same client+route but different query -> different RetryKey.
	a.OnRequest(&ObservedRequest{
		Timestamp: now,
		Client:    client,
		Route:     route,
		Method:    "GET",
		Query:     "a=1",
	})
	a.OnRequest(&ObservedRequest{
		Timestamp: now,
		Client:    client,
		Route:     route,
		Method:    "GET",
		Query:     "b=1",
	})

	snaps := a.Snapshot(1)
	if len(snaps) != 2 {
		t.Fatalf("expected 2 distinct snapshots for different queries, got %d", len(snaps))
	}
}
