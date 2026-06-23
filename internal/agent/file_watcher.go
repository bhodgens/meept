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

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// FileWatcherHook watches filesystem paths matching patterns.
type FileWatcherHook struct {
	os.FileInfo
	Pattern  string        `json:"pattern"`
	Callback func(path string)
	Debounce time.Duration `json:"debounce"`
	Ignore   []string      `json:"ignore"`

	// Async, when true, runs the callback in a background goroutine so
	// the watcher loop never blocks on callback I/O. Failures are logged.
	Async bool `json:"async,omitempty"`

	// AsyncRewake, when true (and Async must also be true), publishes a
	// hook.async_rewake bus signal after the async callback finishes so
	// the agent loop wakes up. Requires SetBus to have been called.
	AsyncRewake bool `json:"async_rewake,omitempty"`

	watcher *fsnotify.Watcher
	logger  *slog.Logger
	mu      sync.RWMutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc

	// bus is used for the async-rewake signal. Optional.
	bus *bus.MessageBus

	// sessionID is included in the rewake payload.
	sessionID string

	// wg tracks in-flight async callbacks for graceful shutdown.
	wg sync.WaitGroup
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

// SetBus wires a MessageBus reference for async-rewake signals.
// Nil is safely ignored (defensive nil guard per CLAUDE.md rule).
func (f *FileWatcherHook) SetBus(b *bus.MessageBus) {
	if b == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bus = b
}

// SetSessionID records the active session ID for inclusion in rewake payloads.
func (f *FileWatcherHook) SetSessionID(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionID = id
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

// Stop halts filesystem watching. It waits for in-flight async callbacks
// to complete before returning.
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

	// Drain any in-flight async callbacks.
	f.wg.Wait()
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

// invokeCallback runs the user-provided callback, synchronously or
// asynchronously depending on the Async flag. When AsyncRewake is true
// (and a bus is wired), a hook.async_rewake signal is published after
// successful async completion.
func (f *FileWatcherHook) invokeCallback(path string) {
	if f.Callback == nil {
		return
	}

	if !f.Async {
		f.Callback(path)
		return
	}

	// Snapshot rewake inputs under lock.
	f.mu.RLock()
	busRef := f.bus
	sid := f.sessionID
	f.mu.RUnlock()

	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		f.Callback(path)

		if f.AsyncRewake && busRef != nil {
			rewakePayload := map[string]any{
				"session_id": sid,
				"hook_type":  "file_watcher",
				"hook_name":  "file:" + f.Pattern,
				"path":       path,
			}
			msg, err := models.NewBusMessage(models.MessageTypeEvent, "hook", rewakePayload)
			if err == nil {
				busRef.Publish(HookAsyncRewakeTopic, msg)
				if f.logger != nil {
					f.logger.Debug("file watcher async rewake published",
						"topic", HookAsyncRewakeTopic,
						"path", path,
						"session_id", sid,
					)
				}
			} else if f.logger != nil {
				f.logger.Warn("file watcher rewake: failed to marshal payload", "error", err)
			}
		} else if f.AsyncRewake && busRef == nil && f.logger != nil {
			f.logger.Warn("async rewake requested but bus is nil",
				"pattern", f.Pattern,
			)
		}
	}()
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
				if f.logger != nil {
					f.logger.Debug("file watcher triggered", "path", path)
				}
				f.invokeCallback(path)
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
