package backup

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/klauspost/compress/zstd"
)

const (
	syncTempDirName = "sync-temp"
	maxSyncTempSize = 1 << 30 // 1 GB hard cap
)

// TempManager manages temporary files during sync operations.
type TempManager struct {
	tempDir string
	mu      sync.Mutex
	// tracked tracks temp files that need cleanup
	tracked []string
	// stopped indicates Cleanup has been called
	stopped bool
}

// NewTempManager creates a new temporary file manager for the given base dir.
// Creates the temp directory if it doesn't exist.
func NewTempManager(baseDir string) (*TempManager, error) {
	tm := &TempManager{
		tempDir: filepath.Join(baseDir, syncTempDirName),
	}

	if err := os.MkdirAll(tm.tempDir, 0o700); err != nil {
		return nil, fmt.Errorf("temp: create temp dir: %w", err)
	}

	// Clean stale temp files from previous runs
	if err := tm.cleanupStale(); err != nil {
		slog.Warn("backup: failed to clean stale temp files", "error", err)
	}

	return tm, nil
}

// cleanupStale removes all files in the temp directory (from previous crashed runs).
func (m *TempManager) cleanupStale() error {
	entries, err := os.ReadDir(m.tempDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		path := filepath.Join(m.tempDir, e.Name())
		if err := os.RemoveAll(path); err != nil {
			slog.Warn("backup: failed to remove stale temp file", "file", path, "error", err)
		}
	}
	return nil
}

// ReservePeerDB decompresses a peer backup file from the repo checkout into a temp file.
// Returns the path to the decompressed DB.
func (m *TempManager) ReservePeerDB(peerBackupPath string) (string, error) {
	if peerBackupPath == "" {
		return "", SyncWrap("reserve", ErrBackupNotCompressed)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate temp directory size
	if err := m.checkTempSize(); err != nil {
		return "", err
	}

	// Decompress to temp
	_, fileName := filepath.Split(peerBackupPath)
	// peerBackupPath is like "backups/2026-06-26/machine-a/local.db.zst"
	// We extract "local.db" as the base name
	baseName := fileName
	if len(baseName) > 4 && baseName[len(baseName)-4:] == ".zst" {
		baseName = baseName[:len(baseName)-4]
	}

	tempPath := filepath.Join(m.tempDir, "peer-"+baseName)

	if err := DecompressFile(peerBackupPath, tempPath); err != nil {
		return "", SyncWrap("decompress", err)
	}

	m.tracked = append(m.tracked, tempPath)
	return tempPath, nil
}

func (m *TempManager) checkTempSize() error {
	total, err := dirSize(m.tempDir)
	if err != nil {
		return &SyncError{
			Op:    "temp_size",
			Err:   err,
			Message: "failed to calculate temp directory size",
		}
	}
	if total > maxSyncTempSize {
		return &SyncError{
			Op:      "temp_size",
			Message: fmt.Sprintf("temp directory exceeds 1GB cap (%d bytes)", total),
		}
	}
	return nil
}

// Remove removes a single temp file from tracking and deletes it.
func (m *TempManager) Remove(path string) {
	if path == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, p := range m.tracked {
		if p == path {
			m.tracked = append(m.tracked[:i], m.tracked[i+1:]...)
			break
		}
	}

	_ = os.Remove(path) // best-effort
}

// Cleanup removes all tracked temp files and the temp directory.
// Safe to call multiple times.
func (m *TempManager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return nil
	}
	m.stopped = true

	for _, path := range m.tracked {
		_ = os.Remove(path)
	}

	// Remove the temp directory itself if empty
	_ = os.Remove(m.tempDir)

	return nil
}

// dirSize recursively calculates the total size of a directory in bytes.
func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// DecompressReader decompresses from src reader to dst writer using zstd.
// This is a streaming decompression variant useful for large files.
func DecompressReader(src io.Reader, dst io.Writer) error {
	reader, err := zstd.NewReader(src)
	if err != nil {
		return SyncWrap("decompress_reader_zstd", err)
	}
	defer reader.Close()

	if _, err := io.Copy(dst, reader); err != nil {
		return SyncWrap("decompress_reader_copy", err)
	}
	return nil
}
