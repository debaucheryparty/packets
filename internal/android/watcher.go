package android

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type FileChangeEvent struct {
	Path    string
	ModTime time.Time
	Hash    string
}

type Watcher struct {
	dir      string
	interval time.Duration
	ignore   []string

	Changes chan []FileChangeEvent
	Errors  chan error

	mu        sync.Mutex
	lastState map[string]watchEntry
}

type watchEntry struct {
	modTime time.Time
	size    int64
	hash    string
}

func NewWatcher(dir string, interval time.Duration, ignore []string) *Watcher {
	return &Watcher{
		dir:       dir,
		interval:  interval,
		ignore:    append(defaultIgnore(), ignore...),
		Changes:   make(chan []FileChangeEvent, 1),
		Errors:    make(chan error, 1),
		lastState: make(map[string]watchEntry),
	}
}

func defaultIgnore() []string {
	return []string{
		".git", "build", ".gradle", ".idea",
		"node_modules", "__pycache__", ".DS_Store",
	}
}

func (w *Watcher) Start(ctx context.Context) {
	_ = w.scan()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prev := w.snapshot()
			if err := w.scan(); err != nil {
				select {
				case w.Errors <- err:
				default:
				}
				continue
			}
			curr := w.snapshot()
			changes := diff(prev, curr)
			if len(changes) > 0 {
				select {
				case w.Changes <- changes:
				default:
				}
			}
		}
	}
}

func (w *Watcher) scan() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if w.shouldIgnore(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		h := quickHash(path, info.ModTime(), info.Size())
		w.lastState[path] = watchEntry{
			modTime: info.ModTime(),
			size:    info.Size(),
			hash:    h,
		}
		return nil
	})
}

func (w *Watcher) snapshot() map[string]watchEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	cp := make(map[string]watchEntry, len(w.lastState))
	for k, v := range w.lastState {
		cp[k] = v
	}
	return cp
}

func (w *Watcher) shouldIgnore(name string) bool {
	for _, ig := range w.ignore {
		if name == ig {
			return true
		}
	}
	return false
}

func diff(prev, curr map[string]watchEntry) []FileChangeEvent {
	var changes []FileChangeEvent
	for path, c := range curr {
		p, existed := prev[path]
		if !existed || p.hash != c.hash {
			changes = append(changes, FileChangeEvent{
				Path:    path,
				ModTime: c.modTime,
				Hash:    c.hash,
			})
		}
	}
	for path := range prev {
		if _, ok := curr[path]; !ok {
			changes = append(changes, FileChangeEvent{Path: path})
		}
	}
	return changes
}

func quickHash(path string, modTime time.Time, size int64) string {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte(modTime.String()))
	h.Write([]byte(fmt.Sprintf("%d", size))) //nolint:staticcheck

	if f, err := os.Open(path); err == nil {
		buf := make([]byte, 4096)
		n, _ := f.Read(buf)
		h.Write(buf[:n])
		f.Close() //nolint:errcheck
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
