package repomap

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheConfig_Defaults(t *testing.T) {
	config := DefaultCacheConfig()
	assert.Equal(t, "auto", config.RefreshMode)
	assert.Equal(t, int64(500*1024*1024), config.MaxCacheSize)
	assert.True(t, config.EnableMemoryCache)
	assert.Equal(t, 100, config.MemoryCacheSize)
}

func TestValidateCacheConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  CacheConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: CacheConfig{
				RefreshMode:       "auto",
				MaxCacheSize:      1000,
				MemoryCacheSize:   50,
				EnableMemoryCache: true,
			},
			wantErr: false,
		},
		{
			name: "invalid refresh mode",
			config: CacheConfig{
				RefreshMode:     "invalid",
				MaxCacheSize:    1000,
				MemoryCacheSize: 50,
			},
			wantErr: true,
		},
		{
			name: "zero max cache size",
			config: CacheConfig{
				RefreshMode:     "auto",
				MaxCacheSize:    0,
				MemoryCacheSize: 50,
			},
			wantErr: true,
		},
		{
			name: "zero memory cache size",
			config: CacheConfig{
				RefreshMode:     "auto",
				MaxCacheSize:    1000,
				MemoryCacheSize: 0,
			},
			wantErr: true,
		},
		{
			name: "all valid modes",
			config: CacheConfig{
				RefreshMode:     "manual",
				MaxCacheSize:    1000,
				MemoryCacheSize: 50,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCacheConfig(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTagCache_Get_Set(t *testing.T) {
	// Create a temporary directory for the cache
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	config := CacheConfig{
		CacheDir:        tmpDir,
		RefreshMode:     "auto",
		MaxCacheSize:    10 * 1024 * 1024,
		MemoryCacheSize: 10,
	}

	cache, err := NewTagCache(config)
	require.NoError(t, err)

	// Create some test tags
	tags := []Tag{
		{FName: "/test/file.go", RelFname: "file.go", Line: 10, Name: "TestFunc", Kind: "function", IsDef: true},
		{FName: "/test/file.go", RelFname: "file.go", Line: 20, Name: "TestVar", Kind: "variable", IsDef: true},
	}

	// Test Set
	mtime := time.Now()
	err = cache.Set("/test/file.go", mtime, tags)
	require.NoError(t, err)

	// Test Get - should find valid cache
	retrieved, found, err := cache.Get("/test/file.go", mtime)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 2, len(retrieved))
}

func TestTagCache_Get_Stale(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	config := CacheConfig{
		CacheDir:        tmpDir,
		RefreshMode:     "auto",
		MaxCacheSize:    10 * 1024 * 1024,
		MemoryCacheSize: 10,
	}

	cache, err := NewTagCache(config)
	require.NoError(t, err)

	tags := []Tag{
		{FName: "/test/file.go", RelFname: "file.go", Line: 10, Name: "TestFunc", Kind: "function", IsDef: true},
	}

	// Set with old mtime
	oldMtime := time.Now().Add(-1 * time.Hour)
	err = cache.Set("/test/file.go", oldMtime, tags)
	require.NoError(t, err)

	// Get with new mtime - should return stale
	newMtime := time.Now()
	retrieved, found, err := cache.Get("/test/file.go", newMtime)
	require.NoError(t, err)
	assert.False(t, found) // Cache should be invalid due to mtime change
	assert.Nil(t, retrieved)
}

func TestTagCache_Get_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	config := CacheConfig{
		CacheDir:        tmpDir,
		RefreshMode:     "auto",
		MaxCacheSize:    10 * 1024 * 1024,
		MemoryCacheSize: 10,
	}

	cache, err := NewTagCache(config)
	require.NoError(t, err)

	// Try to get a file that was never cached
	retrieved, found, err := cache.Get("/nonexistent/file.go", time.Now())
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, retrieved)
}

func TestTagCache_Invalidate(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	config := CacheConfig{
		CacheDir:        tmpDir,
		RefreshMode:     "auto",
		MaxCacheSize:    10 * 1024 * 1024,
		MemoryCacheSize: 10,
	}

	cache, err := NewTagCache(config)
	require.NoError(t, err)

	tags := []Tag{
		{FName: "/test/file.go", RelFname: "file.go", Line: 10, Name: "TestFunc", Kind: "function", IsDef: true},
	}

	mtime := time.Now()
	err = cache.Set("/test/file.go", mtime, tags)
	require.NoError(t, err)

	// Verify it's cached
	_, found, _ := cache.Get("/test/file.go", mtime)
	assert.True(t, found)

	// Invalidate
	err = cache.Invalidate("/test/file.go")
	require.NoError(t, err)

	// Verify it's gone
	_, found, _ = cache.Get("/test/file.go", mtime)
	assert.False(t, found)
}

func TestTagCache_Clear(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	config := CacheConfig{
		CacheDir:        tmpDir,
		RefreshMode:     "auto",
		MaxCacheSize:    10 * 1024 * 1024,
		MemoryCacheSize: 10,
	}

	cache, err := NewTagCache(config)
	require.NoError(t, err)

	// Add some cached files
	for i := range 3 {
		tags := []Tag{{FName: "/test/file.go", RelFname: "file.go", Line: i, Name: "Func", Kind: "function", IsDef: true}}
		err := cache.Set(filepath.Join(tmpDir, "cache_file"), time.Now(), tags)
		require.NoError(t, err)
	}

	// Get stats before clear
	numFiles, _, err := cache.Stats()
	require.NoError(t, err)
	assert.Greater(t, numFiles, int64(0))

	// Clear
	err = cache.Clear()
	require.NoError(t, err)

	// Verify cleared
	numFiles, _, err = cache.Stats()
	require.NoError(t, err)
	assert.Equal(t, int64(0), numFiles)
}

func TestMapCache_Get_Set(t *testing.T) {
	config := DefaultCacheConfig()
	config.MemoryCacheSize = 10

	cache := NewMapCache(config)

	rendered := RenderedMap{
		Content: "test content",
		Tokens:  100,
	}

	// Set
	files := []string{"file1.go", "file2.go"}
	idents := []string{"TestFunc", "TestStruct"}

	cache.Set(files, idents, rendered)

	// Get - should find it
	cached, found := cache.Get(files, idents, 0) // 0 = no expiration
	assert.True(t, found)
	assert.Equal(t, "test content", cached.Content)
	assert.Equal(t, 100, cached.Tokens)
}

func TestMapCache_Get_NotFound(t *testing.T) {
	config := DefaultCacheConfig()
	config.MemoryCacheSize = 10

	cache := NewMapCache(config)

	// Try to get something never cached
	files := []string{"nonexistent.go"}
	idents := []string{}

	cached, found := cache.Get(files, idents, 0)
	assert.False(t, found)
	assert.Nil(t, cached)
}

func TestMapCache_Get_Expired(t *testing.T) {
	config := DefaultCacheConfig()
	config.MemoryCacheSize = 10

	cache := NewMapCache(config)

	rendered := RenderedMap{
		Content: "test content",
		Tokens:  100,
	}

	files := []string{"file1.go"}
	idents := []string{}

	cache.Set(files, idents, rendered)

	// Get with very small maxAge - should be expired
	cached, found := cache.Get(files, idents, 1*time.Nanosecond)
	assert.False(t, found) // Should be expired
	assert.Nil(t, cached)
}

func TestMapCache_Size(t *testing.T) {
	config := DefaultCacheConfig()
	config.MemoryCacheSize = 10

	cache := NewMapCache(config)

	assert.Equal(t, 0, cache.Size())

	for i := range 5 {
		files := []string{string(rune('a' + i))}
		cache.Set(files, []string{}, RenderedMap{Content: "test"})
	}

	assert.Equal(t, 5, cache.Size())
}

func TestMapCache_Clear(t *testing.T) {
	config := DefaultCacheConfig()
	config.MemoryCacheSize = 10

	cache := NewMapCache(config)

	cache.Set([]string{"file1.go"}, []string{}, RenderedMap{Content: "test1"})
	cache.Set([]string{"file2.go"}, []string{}, RenderedMap{Content: "test2"})

	assert.Equal(t, 2, cache.Size())

	cache.Clear()

	assert.Equal(t, 0, cache.Size())
}

func TestGetFileMtime(t *testing.T) {
	// Create a temp file
	tmpFile := filepath.Join(t.TempDir(), "test.go")
	err := os.WriteFile(tmpFile, []byte("package test"), 0644)
	require.NoError(t, err)

	mtime := getFileMtime(tmpFile)
	assert.False(t, mtime.IsZero())

	// Non-existent file should return zero time
	zeroMtime := getFileMtime("/nonexistent/file.go")
	assert.True(t, zeroMtime.IsZero())
}
