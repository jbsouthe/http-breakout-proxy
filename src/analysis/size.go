package analysis

import (
	"math"
	"sync"
	"time"
)

//
// 5. Content-length / payload size profiling
//

// SizeStats tracks basic statistics for a univariate byte-size distribution.
type SizeStats struct {
	Count        int64   // number of observations
	TotalBytes   int64   // sum of sizes
	SquaredBytes float64 // sum(size^2) for variance computation
	MaxBytes     int64   // maximum observed size
	MinBytes     int64   // minimum observed size
	LastUpdated  time.Time
}

// Mean returns the arithmetic mean of the size distribution.
func (s *SizeStats) Mean() float64 {
	if s.Count == 0 {
		return 0
	}
	return float64(s.TotalBytes) / float64(s.Count)
}

// StdDev returns the standard deviation of the size distribution.
func (s *SizeStats) StdDev() float64 {
	if s.Count == 0 {
		return 0
	}
	mean := s.Mean()
	// E[X^2] - (E[X])^2
	e2 := s.SquaredBytes / float64(s.Count)
	variance := e2 - mean*mean
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}

// PayloadProfile contains independent statistics for request and response sizes.
type PayloadProfile struct {
	Request  SizeStats
	Response SizeStats
}

// RouteSizeSnapshot is a read-only view combining RouteKey and its payload stats.
type RouteSizeSnapshot struct {
	Route RouteKey `json:"route"`

	ReqCount int64   `json:"req_count"`
	ReqMean  float64 `json:"req_mean_bytes"`
	ReqStd   float64 `json:"req_std_bytes"`
	ReqMin   int64   `json:"req_min_bytes"`
	ReqMax   int64   `json:"req_max_bytes"`

	ResCount int64   `json:"res_count"`
	ResMean  float64 `json:"res_mean_bytes"`
	ResStd   float64 `json:"res_std_bytes"`
	ResMin   int64   `json:"res_min_bytes"`
	ResMax   int64   `json:"res_max_bytes"`

	LastUpdated time.Time `json:"last_updated"`
}

// SizeAnalyzer maintains payload size statistics keyed by RouteKey.
type SizeAnalyzer struct {
	mu      sync.RWMutex
	byRoute map[RouteKey]*PayloadProfile
}

// NewSizeAnalyzer constructs an empty SizeAnalyzer.
func NewSizeAnalyzer() *SizeAnalyzer {
	return &SizeAnalyzer{
		byRoute: make(map[RouteKey]*PayloadProfile),
	}
}

// OnRequest ingests an ObservedRequest and updates the corresponding route profile.
func (a *SizeAnalyzer) OnRequest(ev *ObservedRequest) {
	if ev == nil {
		return
	}

	now := ev.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	reqBytes := ev.ReqBytes
	if reqBytes < 0 {
		reqBytes = 0
	}
	resBytes := ev.RespBytes
	if resBytes < 0 {
		resBytes = 0
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	profile, ok := a.byRoute[ev.Route]
	if !ok {
		profile = &PayloadProfile{}
		a.byRoute[ev.Route] = profile
	}

	if reqBytes > 0 || ev.Method != "" {
		updateSizeStats(&profile.Request, reqBytes, now)
	}
	if resBytes > 0 || ev.StatusCode != 0 {
		updateSizeStats(&profile.Response, resBytes, now)
	}
}

// updateSizeStats mutates a SizeStats instance with a new sample.
func updateSizeStats(s *SizeStats, size int64, now time.Time) {
	if size < 0 {
		size = 0
	}

	s.Count++
	s.TotalBytes += size
	if s.Count == 1 {
		s.MinBytes = size
		s.MaxBytes = size
	} else {
		if size < s.MinBytes {
			s.MinBytes = size
		}
		if size > s.MaxBytes {
			s.MaxBytes = size
		}
	}
	f := float64(size)
	s.SquaredBytes += f * f
	s.LastUpdated = now
}

// Snapshot returns per-route payload size statistics.
//
// minCount: if > 0, only routes whose request OR response count is >= minCount
//
//	are included.
func (a *SizeAnalyzer) Snapshot(minCount int64) []RouteSizeSnapshot {
	if a == nil {
		return nil
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]RouteSizeSnapshot, 0, len(a.byRoute))
	for route, profile := range a.byRoute {
		req := profile.Request
		res := profile.Response

		if minCount > 0 &&
			req.Count < minCount &&
			res.Count < minCount {
			continue
		}

		last := req.LastUpdated
		if res.LastUpdated.After(last) {
			last = res.LastUpdated
		}

		out = append(out, RouteSizeSnapshot{
			Route: route,

			ReqCount: req.Count,
			ReqMean:  req.Mean(),
			ReqStd:   req.StdDev(),
			ReqMin:   req.MinBytes,
			ReqMax:   req.MaxBytes,

			ResCount: res.Count,
			ResMean:  res.Mean(),
			ResStd:   res.StdDev(),
			ResMin:   res.MinBytes,
			ResMax:   res.MaxBytes,

			LastUpdated: last,
		})
	}
	return out
}

// Size returns the SizeAnalyzer registered in this registry, if any.
func (r *Registry) Size() *SizeAnalyzer {
	if r == nil {
		return nil
	}
	for _, a := range r.analyzers {
		if sa, ok := a.(*SizeAnalyzer); ok {
			return sa
		}
	}
	return nil
}
