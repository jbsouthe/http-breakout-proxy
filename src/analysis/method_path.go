package analysis

import (
	"strings"
	"sync"
	"time"
	"unicode"
)

//
// 7. Method–path density mapping / anomaly detection
//

// EndpointUsage tracks frequency and basic metadata for an endpoint.
type EndpointUsage struct {
	Count       int64
	FirstSeen   time.Time
	LastSeen    time.Time
	StatusCount map[int]int64

	// Anomaly hints:
	NonStandardMethod bool // e.g., methods outside GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS
	HighEntropyPath   bool // e.g., path segments that look like random IDs
}

// MethodPathSnapshot is a read-only snapshot for one route.
type MethodPathSnapshot struct {
	Route       RouteKey      `json:"route"`
	Count       int64         `json:"count"`
	FirstSeen   time.Time     `json:"first_seen"`
	LastSeen    time.Time     `json:"last_seen"`
	StatusCount map[int]int64 `json:"status_count"`
	// Anomaly flags:
	NonStandardMethod bool `json:"non_standard_method"`
	HighEntropyPath   bool `json:"high_entropy_path"`
	Rare              bool `json:"rare"` // derived at snapshot time
}

// MethodPathAnalyzer maps RouteKey -> EndpointUsage and applies simple
// heuristics for anomaly detection.
type MethodPathAnalyzer struct {
	mu      sync.RWMutex
	byRoute map[RouteKey]*EndpointUsage
}

// NewMethodPathAnalyzer constructs an empty analyzer.
func NewMethodPathAnalyzer() *MethodPathAnalyzer {
	return &MethodPathAnalyzer{
		byRoute: make(map[RouteKey]*EndpointUsage),
	}
}

// OnRequest ingests an ObservedRequest and updates method/path statistics.
func (a *MethodPathAnalyzer) OnRequest(ev *ObservedRequest) {
	if ev == nil {
		return
	}

	now := ev.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	u, ok := a.byRoute[ev.Route]
	if !ok {
		u = &EndpointUsage{
			StatusCount: make(map[int]int64),
		}
		u.FirstSeen = now
		a.byRoute[ev.Route] = u

		// Method anomaly only needs to be computed once.
		u.NonStandardMethod = isNonStandardMethod(ev.Method)
		// High-entropy path anomaly only needs to be computed once.
		u.HighEntropyPath = isHighEntropyPath(ev.Route.Path)
	}

	u.Count++
	u.LastSeen = now
	u.StatusCount[ev.StatusCode]++
}

// Snapshot returns a snapshot of method-path density and anomaly hints.
//
// minCount: if > 0, routes with Count < minCount are suppressed *unless*
//
//	they are flagged as anomalous (non-standard method or high-entropy).
func (a *MethodPathAnalyzer) Snapshot(minCount int64) []MethodPathSnapshot {
	if a == nil {
		return nil
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	const rareThreshold int64 = 5 // heuristic "low density" threshold

	out := make([]MethodPathSnapshot, 0, len(a.byRoute))
	for route, usage := range a.byRoute {
		// Filter: keep endpoints with enough density OR anomalous endpoints.
		if minCount > 0 &&
			usage.Count < minCount &&
			!usage.NonStandardMethod &&
			!usage.HighEntropyPath {
			continue
		}

		// Copy status counts so caller cannot mutate internal state.
		statusCopy := make(map[int]int64, len(usage.StatusCount))
		for code, c := range usage.StatusCount {
			statusCopy[code] = c
		}

		snap := MethodPathSnapshot{
			Route:       route,
			Count:       usage.Count,
			FirstSeen:   usage.FirstSeen,
			LastSeen:    usage.LastSeen,
			StatusCount: statusCopy,

			NonStandardMethod: usage.NonStandardMethod,
			HighEntropyPath:   usage.HighEntropyPath,
			Rare:              usage.Count < rareThreshold,
		}
		out = append(out, snap)
	}

	return out
}

// isNonStandardMethod marks methods outside the usual set as "non-standard".
func isNonStandardMethod(m string) bool {
	switch m {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
		return false
	default:
		// You can extend this with WebDAV methods if needed.
		return true
	}
}

// isHighEntropyPath uses a simple heuristic to flag "weird" paths,
// such as those with long, mostly-hex or mixed-alnum segments that
// look like random IDs, fuzzing, or enumeration.
func isHighEntropyPath(path string) bool {
	if path == "" || path == "/" {
		return false
	}

	segments := strings.Split(path, "/")
	for _, seg := range segments {
		if len(seg) < 12 {
			continue
		}
		var letterCount, digitCount, otherCount int
		for _, r := range seg {
			switch {
			case unicode.IsLetter(r):
				letterCount++
			case unicode.IsDigit(r):
				digitCount++
			default:
				if r != '-' && r != '_' && r != '.' {
					otherCount++
				}
			}
		}
		length := len(seg)
		if length == 0 {
			continue
		}
		// Heuristic: long segments with mostly letters+digits and
		// enough "entropy" are considered suspicious.
		alnum := letterCount + digitCount
		if alnum >= int(float64(length)*0.8) && length >= 16 {
			return true
		}
		// Or segments where digits dominate strongly, like numeric IDs.
		if digitCount >= int(float64(length)*0.9) && length >= 10 {
			return true
		}
	}
	return false
}

// MethodPath returns the MethodPathAnalyzer registered in this registry, if any.
func (r *Registry) MethodPath() *MethodPathAnalyzer {
	if r == nil {
		return nil
	}
	for _, a := range r.analyzers {
		if mp, ok := a.(*MethodPathAnalyzer); ok {
			return mp
		}
	}
	return nil
}
