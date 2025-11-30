package analysis

import (
	"net/http"
	"testing"
	"time"
)

func TestDeriveCookiePattern(t *testing.T) {
	h := http.Header{}
	h.Add("Cookie", "sid=aaa;  csrf=bbb")
	h.Add("Cookie", "sid=ccc") // duplicate key should be deduped

	pat := deriveCookiePattern(h)
	if pat != "csrf|sid" {
		t.Fatalf("deriveCookiePattern = %q, want %q", pat, "csrf|sid")
	}

	if deriveCookiePattern(nil) != "" {
		t.Fatalf("expected empty pattern for nil header")
	}
	if deriveCookiePattern(http.Header{}) != "" {
		t.Fatalf("expected empty pattern for empty header")
	}
}

func TestAuthCookieAnalyzerFlappingAndDrift(t *testing.T) {
	a := NewAuthCookieAnalyzer()

	client := ClientID{IP: "1.2.3.4"}
	route := RouteKey{Host: "example.com", Path: "/api", Method: "GET"}

	now := time.Now()

	// 1: auth present, cookie shape {csrf, sid}
	h1 := http.Header{}
	h1.Set("Authorization", "Bearer token1")
	h1.Add("Cookie", "sid=aaa; csrf=bbb")
	a.OnRequest(&ObservedRequest{
		Timestamp:  now.Add(-3 * time.Second),
		Client:     client,
		Route:      route,
		Method:     "GET",
		ReqHeaders: h1,
	})

	// 2: same auth value, same cookie keys but different values.
	h2 := http.Header{}
	h2.Set("Authorization", "Bearer token1")
	h2.Add("Cookie", "csrf=zzz; sid=yyy")
	a.OnRequest(&ObservedRequest{
		Timestamp:  now.Add(-2 * time.Second),
		Client:     client,
		Route:      route,
		Method:     "GET",
		ReqHeaders: h2,
	})

	// 3: auth missing, cookie shape {sid} only.
	h3 := http.Header{}
	h3.Add("Cookie", "sid=only")
	a.OnRequest(&ObservedRequest{
		Timestamp:  now.Add(-1 * time.Second),
		Client:     client,
		Route:      route,
		Method:     "GET",
		ReqHeaders: h3,
	})

	snaps := a.Snapshot(0, 0)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	snap := snaps[0]

	if snap.Client != client || snap.Host != route.Host {
		t.Fatalf("unexpected key in snapshot: %#v", snap)
	}

	if snap.TotalRequests != 3 {
		t.Fatalf("TotalRequests = %d, want 3", snap.TotalRequests)
	}
	if snap.AuthPresentCount != 2 {
		t.Fatalf("AuthPresentCount = %d, want 2", snap.AuthPresentCount)
	}
	if snap.AuthMissingCount != 1 {
		t.Fatalf("AuthMissingCount = %d, want 1", snap.AuthMissingCount)
	}
	if !snap.HasAuthFlapping {
		t.Fatalf("expected HasAuthFlapping = true")
	}
	if snap.AuthChangeCount == 0 {
		t.Fatalf("expected AuthChangeCount > 0")
	}

	// Cookie shape: first two requests -> "csrf|sid", last -> "sid".
	if snap.CurrentCookiePattern != "sid" {
		t.Fatalf("CurrentCookiePattern = %q, want %q", snap.CurrentCookiePattern, "sid")
	}
	if !snap.HasCookieDrift {
		t.Fatalf("expected HasCookieDrift = true")
	}
	if snap.CookiePatternChangeCount == 0 {
		t.Fatalf("expected CookiePatternChangeCount > 0")
	}

	if len(snap.CookiePatterns) != 2 {
		t.Fatalf("expected 2 distinct patterns, got %d", len(snap.CookiePatterns))
	}
	if snap.CookiePatterns["csrf|sid"] == 0 {
		t.Fatalf("expected pattern %q to be present", "csrf|sid")
	}
	if snap.CookiePatterns["sid"] == 0 {
		t.Fatalf("expected pattern %q to be present", "sid")
	}

	// Verify minRequests / minChanges filtering.
	if got := a.Snapshot(10, 0); len(got) != 0 {
		t.Fatalf("expected no snapshots for minRequests=10, got %d", len(got))
	}
	if got := a.Snapshot(0, 10); len(got) != 0 {
		t.Fatalf("expected no snapshots for minChanges=10, got %d", len(got))
	}
}
