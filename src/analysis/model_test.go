package analysis

import "testing"

func TestClassifyOutcome(t *testing.T) {
	tests := []struct {
		status int
		want   Outcome
	}{
		{199, OutcomeOther},
		{200, Outcome2xx},
		{250, Outcome2xx},
		{301, Outcome3xx},
		{399, Outcome3xx},
		{400, Outcome4xx},
		{451, Outcome4xx},
		{500, Outcome5xx},
		{599, Outcome5xx},
	}

	for _, tt := range tests {
		if got := ClassifyOutcome(tt.status); got != tt.want {
			t.Errorf("ClassifyOutcome(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}
