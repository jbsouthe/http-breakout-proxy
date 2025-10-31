package main

import (
	"sort"
	"sync"
)

type ColorRule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Query    string `json:"query"`
	Color    string `json:"color"`
	Note     string `json:"note"`
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority,omitempty"`
}

type ruleStore struct {
	sync.RWMutex
	rules []ColorRule
}

func (rs *ruleStore) getAll() []ColorRule {
	rs.RLock()
	defer rs.RUnlock()
	out := make([]ColorRule, len(rs.rules))
	copy(out, rs.rules)
	return out
}
func (rs *ruleStore) replace(all []ColorRule) {
	rs.Lock()
	defer rs.Unlock()
	// Copy then sort by Priority DESC; stable keeps original relative order for ties.
	copied := append([]ColorRule(nil), all...)
	sort.SliceStable(copied, func(i, j int) bool {
		return copied[i].Priority > copied[j].Priority
	})
	rs.rules = copied
}

func defaultColorRules() []ColorRule {
	return []ColorRule{
		{
			ID:       "1",
			Name:     "red",
			Color:    "#e74c3c", // red
			Query:    "status:5",
			Priority: 100,
			Note:     "Failed HTTP request, 5xx Errors",
			Enabled:  true,
		},
		{
			ID:       "2",
			Name:     "orange",
			Color:    "#d77d28", // orange
			Query:    "status:4",
			Priority: 100,
			Note:     "Failed HTTP request, 4xx Errors",
			Enabled:  true,
		},
		{
			ID:       "3",
			Name:     "blue",
			Color:    "#3498db", // blue
			Query:    "method:POST",
			Priority: 0,
			Note:     "General API traffic",
			Enabled:  true,
		},
		{
			ID:       "4",
			Name:     "green",
			Color:    "#2ecc71", // green
			Query:    "method:GET",
			Priority: 0,
			Note:     "GET request",
			Enabled:  true,
		},
	}
}