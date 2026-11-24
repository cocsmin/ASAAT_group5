package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"cakefactory/internal/model"
)

// FilePans keeps every pan as one JSON file, which is literally a document
// passed between stations. It is the fallback when MongoDB is not available.
type FilePans struct {
	dir string
	mu  sync.Mutex
}

// NewFilePans creates the store directories.
func NewFilePans(dir string) (*FilePans, error) {
	for _, d := range []string{"pans", "state"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return nil, err
		}
	}
	return &FilePans{dir: dir}, nil
}

func (f *FilePans) write(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Save stores a pan document.
func (f *FilePans) Save(_ context.Context, p model.Pan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.write(filepath.Join(f.dir, "pans", p.ID+".json"), p)
}

// Get loads a pan document.
func (f *FilePans) Get(_ context.Context, id string) (model.Pan, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var p model.Pan
	b, err := os.ReadFile(filepath.Join(f.dir, "pans", id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return p, false, nil
	}
	if err != nil {
		return p, false, err
	}
	return p, true, json.Unmarshal(b, &p)
}

// List returns the most recently updated pans.
func (f *FilePans) List(_ context.Context, limit int) ([]model.Pan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	files, err := filepath.Glob(filepath.Join(f.dir, "pans", "*.json"))
	if err != nil {
		return nil, err
	}
	var out []model.Pan
	for _, name := range files {
		b, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		var p model.Pan
		if json.Unmarshal(b, &p) == nil {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SaveState stores a node snapshot.
func (f *FilePans) SaveState(_ context.Context, node string, state map[string]int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.write(filepath.Join(f.dir, "state", node+".json"), state)
}

// LoadState loads a node snapshot; a missing snapshot is not an error.
func (f *FilePans) LoadState(_ context.Context, node string) (map[string]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(f.dir, "state", node+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]int64
	return m, json.Unmarshal(b, &m)
}
