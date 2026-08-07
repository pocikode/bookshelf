package web

import (
	"sync"
	"time"
)

type UploadResult struct {
	Index    int       `json:"index"`
	Filename string    `json:"filename"`
	Status   string    `json:"status"`
	BookID   int64     `json:"book_id,omitempty"`
	Title    string    `json:"title,omitempty"`
	Error    *APIError `json:"error,omitempty"`
	Message  string    `json:"-"`
}
type flashEntry struct {
	results []UploadResult
	expires time.Time
}
type flashStore struct {
	mu      sync.Mutex
	entries map[string]flashEntry
	max     int
	now     func() time.Time
}

func newFlashStore() *flashStore {
	return &flashStore{entries: map[string]flashEntry{}, max: 128, now: time.Now}
}
func (f *flashStore) Put(key string, results []UploadResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	for k, v := range f.entries {
		if !now.Before(v.expires) {
			delete(f.entries, k)
		}
	}
	if len(f.entries) >= f.max {
		var oldest string
		var at time.Time
		for k, v := range f.entries {
			if oldest == "" || v.expires.Before(at) {
				oldest = k
				at = v.expires
			}
		}
		delete(f.entries, oldest)
	}
	f.entries[key] = flashEntry{append([]UploadResult(nil), results...), now.Add(5 * time.Minute)}
}
func (f *flashStore) Take(key string) []UploadResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.entries[key]
	delete(f.entries, key)
	if !ok || !f.now().Before(entry.expires) {
		return nil
	}
	for i := range entry.results {
		if entry.results[i].Error != nil {
			entry.results[i].Message = entry.results[i].Error.Message
		}
	}
	return entry.results
}
