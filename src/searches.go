package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type SearchItem struct {
	ID        string    `json:"id"`
	Query     string    `json:"query"` // the raw filter string
	Label     string    `json:"label,omitempty"`
	Pinned    bool      `json:"pinned,omitempty"`
	Count     int       `json:"count,omitempty"`
	LastUsed  time.Time `json:"last_used"`
	CreatedAt time.Time `json:"created_at"`
}

type searchStore struct {
	sync.RWMutex
	items []SearchItem // MRU: pinned first (stable), then recency descending
	cap   int
}

func newSearchStore(cap int) *searchStore { return &searchStore{cap: cap} }
func (s *searchStore) getAll() []SearchItem {
	s.RLock()
	defer s.RUnlock()
	out := make([]SearchItem, len(s.items))
	copy(out, s.items)
	return out
}
func (s *searchStore) replace(all []SearchItem) {
	s.Lock()
	defer s.Unlock()
	s.items = normalizeAndSort(all, s.cap)
}
func (s *searchStore) upsertRaw(q string, label string, pin bool) SearchItem {
	now := time.Now().UTC()
	s.Lock()
	defer s.Unlock()

	q = strings.TrimSpace(q)
	if q == "" {
		return SearchItem{}
	}

	// de-dupe by exact query
	idx := -1
	for i := range s.items {
		if s.items[i].Query == q {
			idx = i
			break
		}
	}
	if idx >= 0 {
		it := s.items[idx]
		it.LastUsed = now
		it.Count++
		if label != "" {
			it.Label = label
		}
		if pin {
			it.Pinned = true
		}
		// move to front of its section (pinned or unpinned)
		s.items = append(append(s.items[:idx], s.items[idx+1:]...), it)
	} else {
		it := SearchItem{
			ID:        fmt.Sprintf("s-%d", now.UnixNano()),
			Query:     q,
			Label:     label,
			Pinned:    pin,
			Count:     1,
			LastUsed:  now,
			CreatedAt: now,
		}
		s.items = append([]SearchItem{it}, s.items...)
	}
	s.items = normalizeAndSort(s.items, s.cap)
	return s.items[0]
}
func normalizeAndSort(in []SearchItem, cap int) []SearchItem {
	// stable: pinned first (recency within pinned), then unpinned by recency
	pinned, rest := make([]SearchItem, 0, len(in)), make([]SearchItem, 0, len(in))
	for _, it := range in {
		if it.Query == "" {
			continue
		}
		if it.Pinned {
			pinned = append(pinned, it)
		} else {
			rest = append(rest, it)
		}
	}
	sort.SliceStable(pinned, func(i, j int) bool { return pinned[i].LastUsed.After(pinned[j].LastUsed) })
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].LastUsed.After(rest[j].LastUsed) })
	out := append(pinned, rest...)
	if cap > 0 && len(out) > cap {
		out = out[:cap]
	}
	return out
}