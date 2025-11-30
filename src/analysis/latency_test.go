package analysis

import (
	"testing"
	"time"
)

func TestLatencyAnalyzerSingleRoute(t *testing.T) {
	a := NewLatencyAnalyzer()
	route := RouteKey{
		Host:   "example.com",
		Path:   "/api",
		Method: "GET",
	}
	t0 := time.Unix(0, 0)

	a.OnRequest(&ObservedRequest{
		Route:     route,
		Latency:   100 * time.Millisecond,
		Timestamp: t0,
	})
	a.OnRequest(&ObservedRequest{
		Route:     route,
		Latency:   200 * time.Millisecond,
		Timestamp: t0.Add(1 * time.Second),
	})

	snaps := a.Snapshot(0)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	s := snaps[0]
	if s.Route != route {
		t.Fatalf("unexpected route in snapshot: %#v", s.Route)
	}
	if s.Count != 2 {
		t.Fatalf("expected Count=2, got %d", s.Count)
	}
	if s.Mean != 150*time.Millisecond {
		t.Fatalf("expected Mean=150ms, got %s", s.Mean)
	}
	if s.Min != 100*time.Millisecond || s.Max != 200*time.Millisecond {
		t.Fatalf("unexpected Min/Max: %s/%s", s.Min, s.Max)
	}
	if s.LastUpdated.IsZero() {
		t.Fatalf("expected LastUpdated to be set")
	}
}

func TestLatencyAnalyzerSnapshotMinCount(t *testing.T) {
	a := NewLatencyAnalyzer()
	route1 := RouteKey{Host: "h1", Path: "/a", Method: "GET"}
	route2 := RouteKey{Host: "h2", Path: "/b", Method: "GET"}

	a.OnRequest(&ObservedRequest{Route: route1, Latency: 10 * time.Millisecond})
	a.OnRequest(&ObservedRequest{Route: route2, Latency: 20 * time.Millisecond})
	a.OnRequest(&ObservedRequest{Route: route2, Latency: 30 * time.Millisecond})

	snaps := a.Snapshot(2)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot with minCount=2, got %d", len(snaps))
	}
	if snaps[0].Route != route2 {
		t.Fatalf("expected route2 in snapshot, got %#v", snaps[0].Route)
	}
}
