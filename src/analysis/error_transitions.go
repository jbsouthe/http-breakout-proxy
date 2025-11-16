package analysis

import (
	"sync"
	"time"
)

//
// 4. Error-state transition analysis per client
//

// TransitionMatrix tracks transitions between coarse outcomes.
type TransitionMatrix struct {
	// counts[from][to]
	Counts [6][6]uint64
}

// ErrorTransitionState is per-client error state.
type ErrorTransitionState struct {
	LastOutcomeValid bool
	LastOutcome      Outcome
	LastUpdated      time.Time

	// Transitions[from][to] = count
	Transitions map[Outcome]map[Outcome]uint64

	Consecutive5xx    int64
	Consecutive4xx    int64
	ConsecutiveErrors int64 // 5xx + network_error treated as "errors"
}

// ClientErrorSnapshot is a read-only view for a single client.
type ClientErrorSnapshot struct {
	Client            ClientID
	LastOutcome       Outcome
	LastUpdated       time.Time
	Consecutive5xx    int64
	Consecutive4xx    int64
	ConsecutiveErrors int64

	// Transition counts flattened for easier consumption.
	// You can ignore this if you just care about the consecutive counters.
	Transitions map[Outcome]map[Outcome]uint64
}

// ErrorTransitionAnalyzer keeps state per client and tracks error transitions.
type ErrorTransitionAnalyzer struct {
	mu       sync.RWMutex
	byClient map[ClientID]*ErrorTransitionState
}

// NewErrorTransitionAnalyzer constructs an empty analyzer.
func NewErrorTransitionAnalyzer() *ErrorTransitionAnalyzer {
	return &ErrorTransitionAnalyzer{
		byClient: make(map[ClientID]*ErrorTransitionState),
	}
}

// OnRequest ingests an ObservedRequest and updates the client's error state.
func (a *ErrorTransitionAnalyzer) OnRequest(ev *ObservedRequest) {
	if ev == nil {
		return
	}
	now := ev.Timestamp
	if now.IsZero() {
		now = time.Now()
	}
	client := ev.Client
	outcome := ev.Outcome

	a.mu.Lock()
	defer a.mu.Unlock()

	st, ok := a.byClient[client]
	if !ok {
		st = &ErrorTransitionState{
			Transitions: make(map[Outcome]map[Outcome]uint64),
		}
		a.byClient[client] = st
	}

	// Transition matrix update.
	if st.LastOutcomeValid {
		from := st.LastOutcome
		to := outcome
		row, ok := st.Transitions[from]
		if !ok {
			row = make(map[Outcome]uint64)
			st.Transitions[from] = row
		}
		row[to]++
	}

	// Update consecutive counters.
	switch outcome {
	case Outcome5xx:
		st.Consecutive5xx++
		st.ConsecutiveErrors++
	case Outcome4xx:
		st.Consecutive4xx++
		// 4xx are usually "client misuse", not server errors;
		// choose whether they should count toward ConsecutiveErrors.
		// Here we only track them separately, not in ConsecutiveErrors.
	case OutcomeNetworkError:
		st.ConsecutiveErrors++
	default:
		// reset on "good" outcomes
		st.Consecutive5xx = 0
		st.Consecutive4xx = 0
		st.ConsecutiveErrors = 0
	}

	st.LastOutcome = outcome
	st.LastOutcomeValid = true
	st.LastUpdated = now
}

// Snapshot returns a per-client snapshot.
//
// minErrors: minimum ConsecutiveErrors (or Consecutive5xx/4xx) to include.
// You can pass 0 to get all clients.
func (a *ErrorTransitionAnalyzer) Snapshot(minErrors int64) []ClientErrorSnapshot {
	if a == nil {
		return nil
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]ClientErrorSnapshot, 0, len(a.byClient))
	for client, st := range a.byClient {
		// Filter by minErrors threshold.
		if minErrors > 0 &&
			st.ConsecutiveErrors < minErrors &&
			st.Consecutive5xx < minErrors &&
			st.Consecutive4xx < minErrors {
			continue
		}

		// Shallow copy of transitions; values are maps but we treat them as read-only.
		transCopy := make(map[Outcome]map[Outcome]uint64, len(st.Transitions))
		for from, row := range st.Transitions {
			rowCopy := make(map[Outcome]uint64, len(row))
			for to, cnt := range row {
				rowCopy[to] = cnt
			}
			transCopy[from] = rowCopy
		}

		snap := ClientErrorSnapshot{
			Client:            client,
			LastOutcome:       st.LastOutcome,
			LastUpdated:       st.LastUpdated,
			Consecutive5xx:    st.Consecutive5xx,
			Consecutive4xx:    st.Consecutive4xx,
			ConsecutiveErrors: st.ConsecutiveErrors,
			Transitions:       transCopy,
		}
		out = append(out, snap)
	}
	return out
}

// ErrorTransitions returns the ErrorTransitionAnalyzer registered in this registry, if any.
func (r *Registry) ErrorTransitions() *ErrorTransitionAnalyzer {
	if r == nil {
		return nil
	}
	for _, a := range r.analyzers {
		if eta, ok := a.(*ErrorTransitionAnalyzer); ok {
			return eta
		}
	}
	return nil
}
