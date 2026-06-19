package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Upload describes a stored file upload.
type Upload struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	MimeType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	RefCount  int       `json:"ref_count"`
}

// UploadService manages file uploads with SHA-256 dedup and refcounting.
type UploadService struct {
	mu           sync.Mutex
	dir          string
	maxSizeBytes int64
	allowedTypes map[string]bool
	dbPath       string
	logger       *slog.Logger
}

// NewUploadService creates a new UploadService.
// dir is the storage directory (e.g. ~/.meept/uploads).
// maxSizeMB is the maximum upload size in megabytes.
// allowedTypes is the list of accepted MIME types.
func NewUploadService(dir string, maxSizeMB int, allowedTypes []string) *UploadService {
	allowed := make(map[string]bool)
	for _, t := range allowedTypes {
		allowed[t] = true
	}
	return &UploadService{
		dir:          dir,
		maxSizeBytes: int64(maxSizeMB) * 1024 * 1024,
		allowedTypes: allowed,
		dbPath:       filepath.Join(dir, "uploads.json"),
		logger:       slog.Default(),
	}
}

// MaxSizeBytes returns the configured maximum upload size.
func (s *UploadService) MaxSizeBytes() int64 {
	return s.maxSizeBytes
}

// Upload stores a file, returning the upload descriptor. If the content hash
// matches an existing upload, the existing record is returned (dedup).
func (s *UploadService) Upload(ctx context.Context, reader io.Reader, filename string, mimeType string) (*Upload, error) {
	if !s.allowedTypes[mimeType] {
		return nil, fmt.Errorf("MIME type %q not allowed; accepted: %v", mimeType, s.allowedTypesList())
	}

	// Read all bytes (limited by maxSizeBytes)
	data, err := io.ReadAll(io.LimitReader(reader, s.maxSizeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read upload data: %w", err)
	}
	if int64(len(data)) > s.maxSizeBytes {
		return nil, fmt.Errorf("upload exceeds maximum size of %d bytes", s.maxSizeBytes)
	}

	// Compute SHA-256 hash
	hash := sha256.Sum256(data)
	id := hex.EncodeToString(hash[:])

	// Determine extension from filename
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = mimeToExt(mimeType)
	}
	path := filepath.Join(s.dir, id+ext)

	// Snapshot under lock: ensure dir exists, check dedup, capture prior records.
	s.mu.Lock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}
	records := s.loadRecords()
	if existing, ok := records[id]; ok {
		if _, statErr := os.Stat(existing.Path); statErr == nil {
			existing.RefCount++
			records[id] = existing
			if err := s.saveRecords(records); err != nil {
				s.logger.Warn("failed to persist upload refcount", "id", id, "error", err)
			}
			s.mu.Unlock()
			return &existing, nil
		}
		// File missing — fall through to re-store below
	}
	// Reserve a slot to claim dedup after the disk write succeeds.
	reserved := Upload{
		ID:        id,
		Path:      path,
		MimeType:  mimeType,
		SizeBytes: int64(len(data)),
		CreatedAt: time.Now().UTC(),
		RefCount:  1,
	}
	records[id] = reserved
	if err := s.saveRecords(records); err != nil {
		s.logger.Warn("failed to persist upload reservation", "id", id, "error", err)
	}
	s.mu.Unlock()

	// Disk I/O outside the mutex per CLAUDE.md mutex-scope rule.
	// If two callers race here with identical content, the second call's
	// reservation above is idempotent — the file content is identical.
	if err := os.WriteFile(path, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write upload file: %w", err)
	}

	// Extract image dimensions (decode-only, no mutation of service state).
	width, height := imageDimensions(data, mimeType)

	// Re-acquire briefly to record final dimensions.
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.loadRecords()
	if existing, ok := current[id]; ok {
		existing.Width = width
		existing.Height = height
		current[id] = existing
		if err := s.saveRecords(current); err != nil {
			s.logger.Warn("failed to persist upload dimensions", "id", id, "error", err)
		}
		return &existing, nil
	}
	reserved.Width = width
	reserved.Height = height
	current[id] = reserved
	if err := s.saveRecords(current); err != nil {
		s.logger.Warn("failed to persist upload record", "id", id, "error", err)
	}
	return &reserved, nil
}

// Load returns the raw bytes and MIME type for an upload by ID.
func (s *UploadService) Load(ctx context.Context, id string) ([]byte, string, error) {
	s.mu.Lock()
	records := s.loadRecords()
	upload, ok := records[id]
	s.mu.Unlock()

	if !ok {
		return nil, "", fmt.Errorf("upload not found: %s", id)
	}

	data, err := os.ReadFile(upload.Path)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read upload file: %w", err)
	}
	return data, upload.MimeType, nil
}

// Get returns upload metadata by ID.
func (s *UploadService) Get(ctx context.Context, id string) (*Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.loadRecords()
	upload, ok := records[id]
	if !ok {
		return nil, fmt.Errorf("upload not found: %s", id)
	}
	return &upload, nil
}

// Release decrements the refcount for an upload.
func (s *UploadService) Release(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.loadRecords()
	upload, ok := records[id]
	if !ok {
		return fmt.Errorf("upload not found: %s", id)
	}
	if upload.RefCount > 0 {
		upload.RefCount--
	}
	records[id] = upload
	return s.saveRecords(records)
}

// Acquire increments the refcount for an upload.
func (s *UploadService) Acquire(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.loadRecords()
	upload, ok := records[id]
	if !ok {
		return fmt.Errorf("upload not found: %s", id)
	}
	upload.RefCount++
	records[id] = upload
	return s.saveRecords(records)
}

// GCSweep deletes files with refcount=0 older than maxAge. Returns deleted IDs.
func (s *UploadService) GCSweep(maxAgeSeconds int64) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	records := s.loadRecords()
	cutoff := time.Now().UTC().Add(-time.Duration(maxAgeSeconds) * time.Second)
	var deleted []string

	for id, upload := range records {
		if upload.RefCount > 0 {
			continue
		}
		if upload.CreatedAt.After(cutoff) {
			continue
		}
		// Delete file
		if err := os.Remove(upload.Path); err != nil && !os.IsNotExist(err) {
			s.logger.Warn("failed to delete upload file", "id", id, "error", err)
			continue
		}
		delete(records, id)
		deleted = append(deleted, id)
	}

	if err := s.saveRecords(records); err != nil {
		s.logger.Warn("failed to save records after GC", "error", err)
	}

	return deleted, nil
}

// loadRecords reads the uploads metadata from the JSON store.
// Caller must hold s.mu.
func (s *UploadService) loadRecords() map[string]Upload {
	data, err := os.ReadFile(s.dbPath)
	if err != nil {
		return make(map[string]Upload)
	}
	var records map[string]Upload
	if err := json.Unmarshal(data, &records); err != nil {
		return make(map[string]Upload)
	}
	if records == nil {
		return make(map[string]Upload)
	}
	return records
}

// saveRecords writes the uploads metadata to the JSON store.
// Caller must hold s.mu.
func (s *UploadService) saveRecords(records map[string]Upload) error {
	data, err := json.Marshal(records)
	if err != nil {
		return fmt.Errorf("failed to marshal records: %w", err)
	}
	return os.WriteFile(s.dbPath, data, 0644)
}

func (s *UploadService) allowedTypesList() string {
	types := make([]string, 0, len(s.allowedTypes))
	for t := range s.allowedTypes {
		types = append(types, t)
	}
	return strings.Join(types, ", ")
}

// imageDimensions extracts width and height from image bytes.
func imageDimensions(data []byte, mimeType string) (width, height int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// mimeToExt returns a file extension for a MIME type.
func mimeToExt(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}
