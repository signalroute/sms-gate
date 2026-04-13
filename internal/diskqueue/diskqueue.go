package diskqueue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is a message stored in the disk queue.
type Entry struct {
	ID        string    `json:"id"`
	To        string    `json:"to"`
	Body      string    `json:"body"`
	ICCID     string    `json:"iccid"`
	CreatedAt time.Time `json:"created_at"`
	Retries   int       `json:"retries"`
}

// Queue persists entries as JSON files in a directory.
// Each entry is a separate file named <id>.json for easy inspection.
type Queue struct {
	mu  sync.Mutex
	dir string
}

// New creates a Queue backed by dir, creating the directory if needed.
func New(dir string) (*Queue, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("diskqueue: mkdir %s: %w", dir, err)
	}
	return &Queue{dir: dir}, nil
}

// Push writes an entry to disk atomically via a temp file + rename.
func (q *Queue) Push(e Entry) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("diskqueue: marshal entry %s: %w", e.ID, err)
	}

	tmp := filepath.Join(q.dir, e.ID+".tmp")
	dst := filepath.Join(q.dir, e.ID+".json")

	if err := os.WriteFile(tmp, data, 0640); err != nil {
		return fmt.Errorf("diskqueue: write temp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("diskqueue: rename to %s: %w", dst, err)
	}
	return nil
}

// Drain reads all entries, deletes their files, and returns the entries.
// Returns an empty slice (no error) when the directory is empty.
func (q *Queue) Drain() ([]Entry, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	matches, err := filepath.Glob(filepath.Join(q.dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("diskqueue: glob: %w", err)
	}

	entries := make([]Entry, 0, len(matches))
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("diskqueue: read %s: %w", path, err)
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, fmt.Errorf("diskqueue: unmarshal %s: %w", path, err)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("diskqueue: remove %s: %w", path, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Len returns the number of entries currently in the queue.
func (q *Queue) Len() (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	matches, err := filepath.Glob(filepath.Join(q.dir, "*.json"))
	if err != nil {
		return 0, fmt.Errorf("diskqueue: glob: %w", err)
	}
	return len(matches), nil
}

// Remove deletes a single entry by ID.
func (q *Queue) Remove(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	return os.Remove(filepath.Join(q.dir, id+".json"))
}
