package main

import (
	"testing"
	"time"
)

func TestSearchStoreUpsertAndOrder(t *testing.T) {
	s := newSearchStore(10)

	// insert two queries, second pinned
	it1 := s.upsertRaw("  foo  ", "", false)
	if it1.Query != "foo" {
		t.Fatalf("expected normalized query 'foo', got %q", it1.Query)
	}
	if it1.Count != 1 || it1.Pinned {
		t.Fatalf("unexpected item after first upsert: %#v", it1)
	}

	it2 := s.upsertRaw("bar", "Bar label", true)
	if !it2.Pinned {
		t.Fatalf("expected second search to be pinned")
	}

	all := s.getAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 items, got %d", len(all))
	}
	// pinned first
	if all[0].ID != it2.ID || all[1].ID != it1.ID {
		t.Fatalf("unexpected order: got [%s,%s], want [%s,%s]", all[0].ID, all[1].ID, it2.ID, it1.ID)
	}
}

func TestSearchStoreDeduplicateAndCount(t *testing.T) {
	s := newSearchStore(10)

	it1 := s.upsertRaw("foo", "", false)
	time.Sleep(1 * time.Nanosecond) // keep LastUsed monotonic
	it2 := s.upsertRaw("foo", "label", true)

	if it1.ID != it2.ID {
		t.Fatalf("expected same ID for duplicate query, got %s and %s", it1.ID, it2.ID)
	}
	if it2.Count != 2 {
		t.Fatalf("expected Count=2 after duplicate search, got %d", it2.Count)
	}
	if !it2.Pinned {
		t.Fatalf("expected item to be pinned after second upsert")
	}
	if it2.Label != "label" {
		t.Fatalf("expected label to be updated, got %q", it2.Label)
	}
}

func TestSearchStoreCapacity(t *testing.T) {
	s := newSearchStore(3)

	// insert 4 distinct queries, with small capacity => oldest dropped
	for i := 0; i < 4; i++ {
		q := string(rune('a' + i)) // 'a', 'b', 'c', 'd'
		_ = s.upsertRaw(q, "", false)
	}

	all := s.getAll()
	if len(all) != 3 {
		t.Fatalf("expected 3 items due to capacity, got %d", len(all))
	}

	// because we keep MRU and cap=3, the earliest ("a") should be dropped.
	for _, it := range all {
		if it.Query == "a" {
			t.Fatalf("expected query 'a' to be evicted due to capacity")
		}
	}
}

func TestSearchStoreEmptyAndWhitespaceIgnored(t *testing.T) {
	s := newSearchStore(10)

	empty := s.upsertRaw("   ", "", false)
	if (empty != SearchItem{}) {
		t.Fatalf("expected zero SearchItem for whitespace query, got %#v", empty)
	}

	if got := s.getAll(); len(got) != 0 {
		t.Fatalf("expected no items, got %d", len(got))
	}
}
