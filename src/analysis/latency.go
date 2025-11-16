package analysis

import (
	"math"
	"sync"
	"time"
)

// LatencyStats holds aggregated latency metrics for a single route.
type LatencyStats struct {
	Count       int64         // number of observations
	Total       time.Duration // sum of latencies
	SquaredNS   float64       // sum(latency^2) in nanoseconds^2
	Max         time.Duration // max latency
	Min         time.Duration // min latency
	LastUpdated time.Time     // last time this route saw traffic
}

// Mean returns the average latency for the route.
func (s *LatencyStats) Mean() time.Duration {
	if s.Count == 0 {
		return 0
	}
	return time.Duration(int64(s.Total) / s.Count)
}

// StdDev returns the standard deviation of latency.
func (s *LatencyStats) StdDev() time.Duration {
	if s.Count == 0 {
		return 0
	}
	meanNs := float64(s.Total) / float64(s.Count)
	// E[X^2] - (E[X])^2
	varNs2 := s.SquaredNS/float64(s.Count) - meanNs*meanNs
	if varNs2 < 0 {
		varNs2 = 0
	}
	return time.Duration(math.Sqrt(varNs2))
}

// RouteLatencySnapshot is a read-only view combining RouteKey + stats.
type RouteLatencySnapshot struct {
	Route       RouteKey      // host, path, method
	Count       int64         // number of requests
	Mean        time.Duration // mean latency
	StdDev      time.Duration // stddev
	Min         time.Duration // min latency
	Max         time.Duration // max latency
	LastUpdated time.Time     // last seen
}

// LatencyAnalyzer aggregates latency distributions per route (RouteKey).
type LatencyAnalyzer struct {
	mu      sync.RWMutex
	byRoute map[RouteKey]*LatencyStats
}

// NewLatencyAnalyzer constructs an empty LatencyAnalyzer.
func NewLatencyAnalyzer() *LatencyAnalyzer {
	return &LatencyAnalyzer{
		byRoute: make(map[RouteKey]*LatencyStats),
	}
}

// OnRequest ingests an ObservedRequest and updates the route's stats.
func (a *LatencyAnalyzer) OnRequest(ev *ObservedRequest) {
	if ev == nil {
		return
	}

	lat := ev.Latency
	if lat < 0 {
		lat = 0
	}
	now := ev.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	stats, ok := a.byRoute[ev.Route]
	if !ok {
		stats = &LatencyStats{
			Count:       0,
			Total:       0,
			SquaredNS:   0,
			Max:         0,
			Min:         0,
			LastUpdated: now,
		}
		a.byRoute[ev.Route] = stats
	}

	stats.Count++
	stats.Total += lat
	if stats.Count == 1 {
		stats.Min = lat
		stats.Max = lat
	} else {
		if lat < stats.Min {
			stats.Min = lat
		}
		if lat > stats.Max {
			stats.Max = lat
		}
	}

	ns := float64(lat)
	stats.SquaredNS += ns * ns
	stats.LastUpdated = now
}

// Snapshot returns a snapshot of per-route latency stats.
// If minCount > 0, routes with fewer than minCount observations are filtered out.
func (a *LatencyAnalyzer) Snapshot(minCount int64) []RouteLatencySnapshot {
	if a == nil {
		return nil
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]RouteLatencySnapshot, 0, len(a.byRoute))
	for route, stats := range a.byRoute {
		if minCount > 0 && stats.Count < minCount {
			continue
		}

		snap := RouteLatencySnapshot{
			Route:       route,
			Count:       stats.Count,
			Mean:        stats.Mean(),
			StdDev:      stats.StdDev(),
			Min:         stats.Min,
			Max:         stats.Max,
			LastUpdated: stats.LastUpdated,
		}
		out = append(out, snap)
	}
	return out
}

// Latency returns the LatencyAnalyzer registered in this registry, if any.
func (r *Registry) Latency() *LatencyAnalyzer {
	if r == nil {
		return nil
	}
	for _, a := range r.analyzers {
		if la, ok := a.(*LatencyAnalyzer); ok {
			return la
		}
	}
	return nil
}
