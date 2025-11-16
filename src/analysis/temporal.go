package analysis

import (
	"sync"
	"time"
)

// TimeBucket aggregates request statistics in a fixed-width time window.
type TimeBucket struct {
	WindowStart    time.Time     // inclusive window start (UTC, quantized)
	Count          int64         // number of requests observed
	TotalLatency   time.Duration // sum of latencies
	MaxLatency     time.Duration // max latency in bucket
	MinLatency     time.Duration // min latency in bucket
	SquaredLatency float64       // sum(latency^2) for variance estimation
}

// MeanLatency returns the average latency in the bucket.
func (b *TimeBucket) MeanLatency() time.Duration {
	if b.Count == 0 {
		return 0
	}
	return time.Duration(int64(b.TotalLatency) / b.Count)
}

// TemporalAnalyzer maintains a fixed-size ring of TimeBuckets at a given
// temporal resolution (e.g., 1s buckets over the last 60 seconds).
type TemporalAnalyzer struct {
	mu         sync.RWMutex
	resolution time.Duration
	buckets    []TimeBucket
}

// NewTemporalAnalyzer constructs a TemporalAnalyzer with the specified
// bucket resolution and capacity (number of buckets).
func NewTemporalAnalyzer(resolution time.Duration, bucketCount int) *TemporalAnalyzer {
	if bucketCount <= 0 {
		bucketCount = 1
	}
	if resolution <= 0 {
		resolution = time.Second
	}
	return &TemporalAnalyzer{
		resolution: resolution,
		buckets:    make([]TimeBucket, bucketCount),
	}
}

// Temporal returns the TemporalAnalyzer registered in this registry, if any.
// It scans the analyzer slice and type-asserts.
func (r *Registry) Temporal() *TemporalAnalyzer {
	if r == nil {
		return nil
	}
	for _, a := range r.analyzers {
		if ta, ok := a.(*TemporalAnalyzer); ok {
			return ta
		}
	}
	return nil
}

// OnRequest ingests an ObservedRequest and updates the corresponding time bucket.
func (t *TemporalAnalyzer) OnRequest(ev *ObservedRequest) {
	if ev == nil || ev.Timestamp.IsZero() {
		return
	}

	ts := ev.Timestamp.UTC()
	// Quantize timestamp to resolution boundary.
	quantized := ts.Truncate(t.resolution)

	// Map quantized time to a ring index.
	slot := t.indexFor(quantized)

	lat := ev.Latency

	t.mu.Lock()
	defer t.mu.Unlock()

	b := &t.buckets[slot]

	// If this slot belongs to a different window, reset it.
	if b.WindowStart.IsZero() || !b.WindowStart.Equal(quantized) {
		*b = TimeBucket{
			WindowStart:    quantized,
			Count:          0,
			TotalLatency:   0,
			MaxLatency:     0,
			MinLatency:     0,
			SquaredLatency: 0,
		}
	}

	// Update aggregation statistics.
	b.Count++
	b.TotalLatency += lat
	if b.Count == 1 {
		b.MinLatency = lat
		b.MaxLatency = lat
	} else {
		if lat < b.MinLatency {
			b.MinLatency = lat
		}
		if lat > b.MaxLatency {
			b.MaxLatency = lat
		}
	}
	// Use float64 representation of nanoseconds for variance.
	ns := float64(lat)
	b.SquaredLatency += ns * ns
}

// indexFor computes the ring index for a quantized timestamp.
func (t *TemporalAnalyzer) indexFor(quantized time.Time) int {
	// Convert time to a bucket sequence number and mod by capacity.
	// Using UnixNano / resolution ensures contiguous integer buckets.
	seq := quantized.UnixNano() / int64(t.resolution)
	n := int64(len(t.buckets))
	if n <= 0 {
		return 0
	}
	mod := seq % n
	if mod < 0 {
		mod += n
	}
	return int(mod)
}

// Snapshot returns a copy of the current buckets, suitable for read-only
// inspection (e.g., metrics endpoint, UI graphing).
func (t *TemporalAnalyzer) Snapshot() []TimeBucket {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]TimeBucket, len(t.buckets))
	copy(out, t.buckets)
	return out
}
