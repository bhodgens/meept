// Package agent provides file system watcher hooks.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileWatcherHook watches filesystem paths matching patterns.
type FileWatcherHook struct {
	os.FileInfo
	Pattern  string        `json:"pattern"`
	Callback func(path string)
	Debounce time.Duration `json:"debounce"`
	Ignore   []string      `json:"ignore"`
	
	watcher  *fsnotify.Watcher
	logger   *slog.Logger
	mu       sync.RWMutex
	running  bool
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewFileWatcherHook creates a new file watcher hook.
func NewFileWatcherHook(pattern string, debounce time.Duration, ignore []string, logger *slog.Logger) *FileWatcherHook {
	if debounce == 0 {
		debounce = 500 * time.Millisecond
	}
	return &FileWatcherHook{
		Pattern:  pattern,
		Debounce: debounce,
		Ignore:   ignore,
		logger:   logger,
	}
}

// Start begins watching filesystem paths.
func (f *FileWatcherHook) Start(ctx context.Context) error {
	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		f.mu.Unlock()
		return fmt.Errorf("failed to create watcher: %w", err)
	}

	f.ctx, f.cancel = context.WithCancel(ctx)
	f.watcher = watcher
	f.running = true
	f.mu.Unlock()

	// I/O outside lock (CLAUDE.md mutex-scope rule)
	if err := watcher.Add("."); err != nil {
		watcher.Close()
		f.mu.Lock()
		f.watcher = nil
		f.running = false
		f.mu.Unlock()
		return fmt.Errorf("failed to add watch: %w", err)
	}

	go f.watchLoop()
	return nil
}

// Stop halts filesystem watching.
func (f *FileWatcherHook) Stop() error {
	f.mu.Lock()
	if !f.running {
		f.mu.Unlock()
		return nil
	}

	cancel := f.cancel
	watcher := f.watcher
	f.running = false
	f.mu.Unlock()

	// I/O outside lock (CLAUDE.md mutex-scope rule)
	if cancel != nil {
		cancel()
	}
	if watcher != nil {
		watcher.Close()
	}
	return nil
}

// shouldIgnore checks if path should be ignored.
func (f *FileWatcherHook) shouldIgnore(path string) bool {
	for _, prefix := range f.Ignore {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// matchesPattern checks if path matches the glob pattern.
func (f *FileWatcherHook) matchesPattern(path string) bool {
	matched, err := filepath.Match(f.Pattern, filepath.Base(path))
	return err == nil && matched
}

// IsRunning returns true if the watcher is active.
func (f *FileWatcherHook) IsRunning() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.running
}

// watchLoop processes filesystem events with debouncing.
func (f *FileWatcherHook) watchLoop() {
	var debounceTimer *time.Timer
	var pendingPath string
	var mu sync.Mutex

	for {
		select {
		case <-f.ctx.Done():
			return
		case event, ok := <-f.watcher.Events:
			if !ok {
				return
			}

			if f.shouldIgnore(event.Name) || !f.matchesPattern(event.Name) {
				continue
			}

			mu.Lock()
			pendingPath = event.Name
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(f.Debounce, func() {
				mu.Lock()
				path := pendingPath
				mu.Unlock()
				if f.Callback != nil {
					if f.logger != nil {
						f.logger.Debug("file watcher triggered", "path", path)
					}
					f.Callback(path)
				}
			})
			mu.Unlock()

		case err, ok := <-f.watcher.Errors:
			if !ok {
				return
			}
			if f.logger != nil {
				f.logger.Warn("file watcher error", "error", err)
			}
		}
	}
}
