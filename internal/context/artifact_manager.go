package context

import (
	"fmt"
	"sync"
	"time"
)

// ArtifactManager manages artifacts for multiple directories
type ArtifactManager struct {
	mu        sync.RWMutex
	scanners  map[string]*ArtifactScanner // workingDir -> scanner
	cache     *ArtifactCache
	defaultTTL time.Duration
}

// NewArtifactManager creates a new artifact manager
func NewArtifactManager(ttl time.Duration) *ArtifactManager {
	return &ArtifactManager{
		scanners:  make(map[string]*ArtifactScanner),
		cache:     NewArtifactCache(ttl),
		defaultTTL: ttl,
	}
}

// ScanDirectory scans a directory for Claude artifacts
func (am *ArtifactManager) ScanDirectory(dir string) (*Artifacts, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	// Normalize the directory
	normalizedDir, err := NormalizePath(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize directory: %w", err)
	}

	// Get or create scanner for this directory
	scanner, exists := am.scanners[normalizedDir]
	if !exists {
		scanner = NewArtifactScanner(normalizedDir, am.cache)
		am.scanners[normalizedDir] = scanner
	}

	// Scan for artifacts
	artifacts, err := scanner.Scan()
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory %s: %w", normalizedDir, err)
	}

	return artifacts, nil
}

// GetArtifacts retrieves artifacts for a directory, scanning if necessary
func (am *ArtifactManager) GetArtifacts(dir string) (*Artifacts, error) {
	return am.ScanDirectory(dir)
}

// HasArtifacts returns true if a directory has Claude artifacts
func (am *ArtifactManager) HasArtifacts(dir string) (bool, error) {
	artifacts, err := am.GetArtifacts(dir)
	if err != nil {
		return false, err
	}
	return artifacts.Available, nil
}

// Invalidate invalidates cached artifacts for a directory
func (am *ArtifactManager) Invalidate(dir string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	normalizedDir, err := NormalizePath(dir)
	if err != nil {
		return fmt.Errorf("failed to normalize directory: %w", err)
	}

	// Invalidate cache
	am.cache.Invalidate(normalizedDir)

	return nil
}

// InvalidateAll invalidates all cached artifacts
func (am *ArtifactManager) InvalidateAll() {
	am.mu.Lock()
	defer am.mu.Unlock()

	am.cache.Clear()
}

// GetScanner returns the scanner for a directory
func (am *ArtifactManager) GetScanner(dir string) (*ArtifactScanner, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	normalizedDir, err := NormalizePath(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize directory: %w", err)
	}

	scanner, exists := am.scanners[normalizedDir]
	if !exists {
		scanner = NewArtifactScanner(normalizedDir, am.cache)
		am.scanners[normalizedDir] = scanner
	}

	return scanner, nil
}

// Cleanup removes scanners for directories that are no longer needed
func (am *ArtifactManager) Cleanup() {
	am.mu.Lock()
	defer am.mu.Unlock()

	// Clear all scanners and cache
	am.scanners = make(map[string]*ArtifactScanner)
	am.cache.Clear()
}

// GetCacheStats returns cache statistics
func (am *ArtifactManager) GetCacheStats() map[string]interface{} {
	am.mu.RLock()
	defer am.mu.RUnlock()

	return map[string]interface{}{
		"scanners": len(am.scanners),
		"cache_entries": func() int {
			am.cache.mu.RLock()
			defer am.cache.mu.RUnlock()
			return len(am.cache.entries)
		}(),
	}
}
