package config

import (
	"log/slog"
	"sync"
)

// ReloadRegistry manages reload callbacks for config file changes.
type ReloadRegistry struct {
	hooks map[string][]ReloadFunc
	logger *slog.Logger
	mu     sync.RWMutex
}

// NewReloadRegistry creates an empty registry.
func NewReloadRegistry() *ReloadRegistry {
	return &ReloadRegistry{
		hooks:  make(map[string][]ReloadFunc),
		logger: slog.Default(),
	}
}

// Register adds a reload hook for a config file path or name.
func (r *ReloadRegistry) Register(path string, fn ReloadFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks[path] = append(r.hooks[path], fn)
}

// Trigger calls all hooks registered for the given path.
// Errors from individual hooks are logged but don't stop subsequent hooks.
func (r *ReloadRegistry) Trigger(path string, commitHash string) {
	r.mu.RLock()
	fns := r.hooks[path]
	r.mu.RUnlock()

	for i, fn := range fns {
		if fn == nil {
			continue
		}
		// Recover from panics in user-supplied callbacks.
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					r.logger.Error("config sync: reload hook panicked",
						"hook", i, "path", path, "recovered", rec)
				}
			}()
			if err := fn(nil, nil); err != nil {
				r.logger.Warn("config sync: reload hook returned error",
					"hook", i, "path", path, "error", err, "commit", commitHash)
			}
		}()
	}
}

// Len returns the number of hooks registered for a path.
func (r *ReloadRegistry) Len(path string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.hooks[path])
}

// RegisteredHooks returns a list of registered hook paths.
func (r *ReloadRegistry) RegisteredHooks() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.hooks))
	for k := range r.hooks {
		keys = append(keys, k)
	}
	return keys
}
