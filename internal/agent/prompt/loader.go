// Package prompt provides composable system prompt building for agents.
package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Loader loads prompt components from the filesystem.
type Loader struct {
	searchPaths []string
	cache       map[string]string
	mu          sync.RWMutex
}

// NewLoader creates a new prompt loader with the given search paths.
// Paths are searched in order; first match wins.
func NewLoader(searchPaths []string) *Loader {
	return &Loader{
		searchPaths: searchPaths,
		cache:       make(map[string]string),
	}
}

// DefaultLoader creates a loader with default search paths.
func DefaultLoader() *Loader {
	homeDir, _ := os.UserHomeDir()

	paths := []string{}

	// Project-local prompts (highest priority)
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, ".meept", "prompts"))
	}

	// User prompts
	if homeDir != "" {
		paths = append(paths, filepath.Join(homeDir, ".meept", "prompts"))
	}

	// Built-in prompts (shipped with meept)
	// Try relative to binary or current directory
	paths = append(paths, "config/prompts")

	// Also check absolute path from typical install locations
	if homeDir != "" {
		paths = append(paths, filepath.Join(homeDir, ".meept", "config", "prompts"))
	}

	return NewLoader(paths)
}

// Load loads a prompt component by reference.
// Reference format: "category.name" -> category/name.md
// Example: "base.constitution" -> base/constitution.md
func (l *Loader) Load(ref string) (string, error) {
	// Check cache first
	l.mu.RLock()
	if content, ok := l.cache[ref]; ok {
		l.mu.RUnlock()
		return content, nil
	}
	l.mu.RUnlock()

	// Convert reference to path
	path := l.refToPath(ref)

	// Search paths
	for _, searchPath := range l.searchPaths {
		fullPath := filepath.Join(searchPath, path)
		content, err := os.ReadFile(fullPath)
		if err == nil {
			// Cache and return
			l.mu.Lock()
			l.cache[ref] = string(content)
			l.mu.Unlock()
			return string(content), nil
		}
	}

	return "", fmt.Errorf("prompt component not found: %s (searched: %v)", ref, l.searchPaths)
}

// LoadAll loads multiple prompt components and returns them in order.
func (l *Loader) LoadAll(refs []string) ([]string, error) {
	results := make([]string, 0, len(refs))
	for _, ref := range refs {
		content, err := l.Load(ref)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", ref, err)
		}
		results = append(results, content)
	}
	return results, nil
}

// LoadAllOptional loads components but skips missing ones without error.
func (l *Loader) LoadAllOptional(refs []string) []string {
	results := make([]string, 0, len(refs))
	for _, ref := range refs {
		content, err := l.Load(ref)
		if err == nil {
			results = append(results, content)
		}
	}
	return results
}

// refToPath converts a reference to a file path.
// "base.constitution" -> "base/constitution.md"
// "specialist.coder" -> "specialist/coder.md"
func (l *Loader) refToPath(ref string) string {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) == 2 {
		return filepath.Join(parts[0], parts[1]+".md")
	}
	return ref + ".md"
}

// ClearCache clears the component cache.
func (l *Loader) ClearCache() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache = make(map[string]string)
}

// Exists checks if a prompt component exists.
func (l *Loader) Exists(ref string) bool {
	path := l.refToPath(ref)
	for _, searchPath := range l.searchPaths {
		fullPath := filepath.Join(searchPath, path)
		if _, err := os.Stat(fullPath); err == nil {
			return true
		}
	}
	return false
}

// ListComponents returns all available prompt components.
func (l *Loader) ListComponents() []string {
	seen := make(map[string]bool)
	var components []string

	for _, searchPath := range l.searchPaths {
		if err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}

			// Convert path to reference
			relPath, err := filepath.Rel(searchPath, path)
			if err != nil {
				return err
			}

			ref := l.pathToRef(relPath)
			if !seen[ref] {
				seen[ref] = true
				components = append(components, ref)
			}
			return nil
		}); err != nil {
			continue
		}
	}

	return components
}

// pathToRef converts a file path back to a reference.
// "base/constitution.md" -> "base.constitution"
func (l *Loader) pathToRef(path string) string {
	// Remove .md extension
	path = strings.TrimSuffix(path, ".md")
	// Convert path separators to dots
	return strings.ReplaceAll(path, string(filepath.Separator), ".")
}

// AddSearchPath adds a search path to the loader.
func (l *Loader) AddSearchPath(path string) {
	l.searchPaths = append([]string{path}, l.searchPaths...)
}

// SearchPaths returns the current search paths.
func (l *Loader) SearchPaths() []string {
	return l.searchPaths
}
