package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileReadUnchanged(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "stable.txt")
	content := "hello world\nline two\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	cache := NewReadCache(10)
	tool := NewReadFileTool(nil, cache)
	ctx := context.Background()

	// First read returns full content.
	result1, err := tool.Execute(ctx, map[string]any{"path": filePath})
	require.NoError(t, err)
	tr1 := result1.(tools.ToolResult)
	assert.True(t, tr1.Success)
	assert.NotEqual(t, FileUnchangedStub, tr1.Result)

	// Second read of the same unchanged file returns the stub.
	result2, err := tool.Execute(ctx, map[string]any{"path": filePath})
	require.NoError(t, err)
	tr2 := result2.(tools.ToolResult)
	assert.True(t, tr2.Success)
	assert.Equal(t, FileUnchangedStub, tr2.Result)
}

func TestFileReadChanged(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "changing.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("original content\n"), 0o644))

	cache := NewReadCache(10)
	tool := NewReadFileTool(nil, cache)
	ctx := context.Background()

	// First read.
	result1, err := tool.Execute(ctx, map[string]any{"path": filePath})
	require.NoError(t, err)
	tr1 := result1.(tools.ToolResult)
	assert.NotEqual(t, FileUnchangedStub, tr1.Result)

	// Modify the file.
	require.NoError(t, os.WriteFile(filePath, []byte("modified content\n"), 0o644))

	// Second read after modification returns full content, not stub.
	result2, err := tool.Execute(ctx, map[string]any{"path": filePath})
	require.NoError(t, err)
	tr2 := result2.(tools.ToolResult)
	assert.True(t, tr2.Success)
	assert.NotEqual(t, FileUnchangedStub, tr2.Result)
}

func TestFileReadUnchangedDifferentFile(t *testing.T) {
	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.txt")
	fileB := filepath.Join(dir, "b.txt")
	require.NoError(t, os.WriteFile(fileA, []byte("content A\n"), 0o644))
	require.NoError(t, os.WriteFile(fileB, []byte("content B\n"), 0o644))

	cache := NewReadCache(10)
	tool := NewReadFileTool(nil, cache)
	ctx := context.Background()

	// Read file A.
	_, err := tool.Execute(ctx, map[string]any{"path": fileA})
	require.NoError(t, err)

	// Reading file B (different file) returns full content.
	resultB, err := tool.Execute(ctx, map[string]any{"path": fileB})
	require.NoError(t, err)
	trB := resultB.(tools.ToolResult)
	assert.True(t, trB.Success)
	assert.NotEqual(t, FileUnchangedStub, trB.Result)
}

func TestFileReadUnchangedRawMode(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "raw.txt")
	content := "raw content here\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	cache := NewReadCache(10)
	tool := NewReadFileTool(nil, cache)
	ctx := context.Background()

	// First read in raw mode — raw mode does not store a snapshot tag,
	// so no hash is cached and the second raw read returns full content.
	result1, err := tool.Execute(ctx, map[string]any{"path": filePath, "raw": true})
	require.NoError(t, err)
	tr1 := result1.(tools.ToolResult)
	assert.Equal(t, content, tr1.Result)

	// Second raw read also returns full content (no hash cached from raw reads).
	result2, err := tool.Execute(ctx, map[string]any{"path": filePath, "raw": true})
	require.NoError(t, err)
	tr2 := result2.(tools.ToolResult)
	assert.Equal(t, content, tr2.Result)
}

func TestFileReadUnchangedWithOffset(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "offset.txt")
	content := "line1\nline2\nline3\nline4\nline5\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	cache := NewReadCache(10)
	tool := NewReadFileTool(nil, cache)
	ctx := context.Background()

	// Full read to populate cache.
	_, err := tool.Execute(ctx, map[string]any{"path": filePath})
	require.NoError(t, err)

	// Partial read with offset always returns content, even if unchanged.
	result, err := tool.Execute(ctx, map[string]any{"path": filePath, "offset": float64(2), "limit": float64(2)})
	require.NoError(t, err)
	tr := result.(tools.ToolResult)
	assert.True(t, tr.Success)
	assert.NotEqual(t, FileUnchangedStub, tr.Result)
}

func TestFileReadUnchangedNilCache(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "nocache.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("some content\n"), 0o644))

	// nil cache means unchanged detection is disabled.
	tool := NewReadFileTool(nil, nil)
	ctx := context.Background()

	result1, err := tool.Execute(ctx, map[string]any{"path": filePath})
	require.NoError(t, err)
	tr1 := result1.(tools.ToolResult)
	assert.NotEqual(t, FileUnchangedStub, tr1.Result)

	result2, err := tool.Execute(ctx, map[string]any{"path": filePath})
	require.NoError(t, err)
	tr2 := result2.(tools.ToolResult)
	assert.NotEqual(t, FileUnchangedStub, tr2.Result)
}
