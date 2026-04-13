package diskqueue

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func newTestQueue(t *testing.T) *Queue {
	t.Helper()
	dir := t.TempDir()
	q, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return q
}

func sampleEntry(id string) Entry {
	return Entry{
		ID:        id,
		To:        "+4917612345678",
		Body:      "hello " + id,
		ICCID:     "8949XXXX",
		CreatedAt: time.Now().Truncate(time.Millisecond),
		Retries:   0,
	}
}

// 1. New creates the directory.
func TestNew_CreatesDirectory(t *testing.T) {
	dir := t.TempDir() + "/queue-sub"
	if _, err := New(dir); err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory not created: %v", err)
	}
}

// 2. Push writes a .json file for the entry.
func TestPush_WritesFile(t *testing.T) {
	q := newTestQueue(t)
	e := sampleEntry("push-test")
	if err := q.Push(e); err != nil {
		t.Fatalf("Push: %v", err)
	}
	path := q.dir + "/" + e.ID + ".json"
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s to exist: %v", path, err)
	}
}

// 3. Len returns the correct count.
func TestLen_ReturnsCount(t *testing.T) {
	q := newTestQueue(t)
	for i := range 5 {
		if err := q.Push(sampleEntry(fmt.Sprintf("len-%d", i))); err != nil {
			t.Fatalf("Push: %v", err)
		}
	}
	n, err := q.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != 5 {
		t.Errorf("Len = %d, want 5", n)
	}
}

// 4. Drain returns all entries and clears the queue.
func TestDrain_ReturnsAllAndClears(t *testing.T) {
	q := newTestQueue(t)
	ids := []string{"drain-a", "drain-b", "drain-c"}
	for _, id := range ids {
		if err := q.Push(sampleEntry(id)); err != nil {
			t.Fatalf("Push: %v", err)
		}
	}

	entries, err := q.Drain()
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("Drain returned %d entries, want 3", len(entries))
	}

	n, err := q.Len()
	if err != nil {
		t.Fatalf("Len after Drain: %v", err)
	}
	if n != 0 {
		t.Errorf("Len after Drain = %d, want 0", n)
	}
}

// 5. Drain on empty dir returns empty slice, no error.
func TestDrain_EmptyDir(t *testing.T) {
	q := newTestQueue(t)
	entries, err := q.Drain()
	if err != nil {
		t.Fatalf("Drain on empty: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(entries))
	}
}

// 6. Remove deletes a specific entry.
func TestRemove_DeletesEntry(t *testing.T) {
	q := newTestQueue(t)
	ids := []string{"rm-a", "rm-b"}
	for _, id := range ids {
		if err := q.Push(sampleEntry(id)); err != nil {
			t.Fatalf("Push: %v", err)
		}
	}

	if err := q.Remove("rm-a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	n, err := q.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != 1 {
		t.Errorf("Len after Remove = %d, want 1", n)
	}

	entries, err := q.Drain()
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "rm-b" {
		t.Errorf("unexpected entries after Remove: %+v", entries)
	}
}

// 7. Push with the same ID overwrites the existing file.
func TestPush_SameIDOverwrites(t *testing.T) {
	q := newTestQueue(t)
	e := sampleEntry("overwrite")
	if err := q.Push(e); err != nil {
		t.Fatalf("first Push: %v", err)
	}

	e.Body = "updated body"
	e.Retries = 3
	if err := q.Push(e); err != nil {
		t.Fatalf("second Push: %v", err)
	}

	n, err := q.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != 1 {
		t.Errorf("Len = %d after overwrite, want 1", n)
	}

	entries, err := q.Drain()
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if entries[0].Body != "updated body" || entries[0].Retries != 3 {
		t.Errorf("overwrite not applied: %+v", entries[0])
	}
}

// 8. Concurrent Push is safe.
func TestPush_Concurrent(t *testing.T) {
	t.Parallel()
	q := newTestQueue(t)

	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func(i int) {
			defer wg.Done()
			if err := q.Push(sampleEntry(fmt.Sprintf("concurrent-%d", i))); err != nil {
				t.Errorf("concurrent Push %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	n, err := q.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != workers {
		t.Errorf("Len = %d, want %d", n, workers)
	}
}
