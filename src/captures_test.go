package main

import (
	"testing"
	"time"
)

func TestCaptureStoreAddAndListOrder(t *testing.T) {
	store := newCaptureStore(3)

	t0 := time.Unix(0, 0)
	c1 := store.add(Capture{Time: t0.Add(1 * time.Second)})
	c2 := store.add(Capture{Time: t0.Add(2 * time.Second)})
	c3 := store.add(Capture{Time: t0.Add(3 * time.Second)})

	if c1.ID != 1 || c2.ID != 2 || c3.ID != 3 {
		t.Fatalf("unexpected IDs: %d, %d, %d", c1.ID, c2.ID, c3.ID)
	}

	all := store.list()
	if len(all) != 3 {
		t.Fatalf("expected 3 captures, got %d", len(all))
	}
	// list returns oldest first
	if all[0].ID != c1.ID || all[1].ID != c2.ID || all[2].ID != c3.ID {
		t.Fatalf("unexpected order in list: got IDs %v", []int64{all[0].ID, all[1].ID, all[2].ID})
	}
}

func TestCaptureStoreRingBufferEviction(t *testing.T) {
	store := newCaptureStore(2)

	//c1 := store.add(Capture{})
	c2 := store.add(Capture{})
	c3 := store.add(Capture{})

	all := store.list()
	if len(all) != 2 {
		t.Fatalf("expected 2 captures, got %d", len(all))
	}
	// c1 should have been evicted
	if all[0].ID != c2.ID || all[1].ID != c3.ID {
		t.Fatalf("expected IDs [%d,%d], got [%d,%d]", c2.ID, c3.ID, all[0].ID, all[1].ID)
	}
}

func TestCaptureStoreGetAndDelete(t *testing.T) {
	store := newCaptureStore(4)

	c1 := store.add(Capture{URL: "/a"})
	c2 := store.add(Capture{URL: "/b"})

	got, ok := store.get(c1.ID)
	if !ok || got.ID != c1.ID || got.URL != "/a" {
		t.Fatalf("get(%d) = %#v, %v", c1.ID, got, ok)
	}

	if !store.delete(c1.ID) {
		t.Fatalf("delete(%d) = false, want true", c1.ID)
	}
	if _, ok := store.get(c1.ID); ok {
		t.Fatalf("expected capture %d to be deleted", c1.ID)
	}
	// deleting again should report false
	if store.delete(c1.ID) {
		t.Fatalf("second delete(%d) = true, want false", c1.ID)
	}

	all := store.list()
	if len(all) != 1 || all[0].ID != c2.ID {
		t.Fatalf("unexpected remaining captures: %#v", all)
	}
}

func TestCaptureStorePopulateFromSliceAndClear(t *testing.T) {
	store := newCaptureStore(3)

	caps := []Capture{
		{ID: 10, URL: "/a"},
		{ID: 11, URL: "/b"},
		{ID: 12, URL: "/c"},
	}
	store.populateFromSlice(caps)

	all := store.list()
	if len(all) != 3 {
		t.Fatalf("expected 3 captures after populateFromSlice, got %d", len(all))
	}
	if all[0].ID != 10 || all[1].ID != 11 || all[2].ID != 12 {
		t.Fatalf("unexpected order/IDs after populateFromSlice: %#v", all)
	}

	// seq should be > max ID
	c := store.add(Capture{URL: "/d"})
	if c.ID <= 12 {
		t.Fatalf("expected seq to be advanced beyond 12, got %d", c.ID)
	}

	store.clear()
	if got := store.list(); len(got) != 0 {
		t.Fatalf("expected empty after clear, got %d", len(got))
	}
}

func TestCaptureStoreUpdateName(t *testing.T) {
	store := newCaptureStore(2)
	c := store.add(Capture{URL: "/a"})

	updated, ok := store.updateName(c.ID, "test-name")
	if !ok {
		t.Fatalf("updateName(%d) = false, want true", c.ID)
	}
	if updated.Name != "test-name" {
		t.Fatalf("updateName did not change Name, got %q", updated.Name)
	}

	// unknown id
	if _, ok := store.updateName(9999, "nope"); ok {
		t.Fatalf("updateName on unknown id should return false")
	}
}
