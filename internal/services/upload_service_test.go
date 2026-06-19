package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadServiceUpload(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUploadService(tmpDir, 20, []string{"image/png"})

	// Create a tiny test PNG
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode PNG: %v", err)
	}

	upload, err := svc.Upload(t.Context(), bytes.NewReader(buf.Bytes()), "test.png", "image/png")
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// Verify ID is SHA-256 hash of content
	hash := sha256.Sum256(buf.Bytes())
	expectedID := hex.EncodeToString(hash[:])
	if upload.ID != expectedID {
		t.Errorf("expected ID %q, got %q", expectedID, upload.ID)
	}

	if upload.MimeType != "image/png" {
		t.Errorf("expected MIME image/png, got %s", upload.MimeType)
	}
	if upload.Width != 4 || upload.Height != 4 {
		t.Errorf("expected 4x4, got %dx%d", upload.Width, upload.Height)
	}

	// File should exist on disk
	if _, err := os.Stat(upload.Path); err != nil {
		t.Errorf("file not on disk: %v", err)
	}
}

func TestUploadServiceDedup(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUploadService(tmpDir, 20, []string{"image/png"})

	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

	first, err := svc.Upload(t.Context(), bytes.NewReader(data), "a.png", "image/png")
	if err != nil {
		t.Fatalf("first upload failed: %v", err)
	}
	second, err := svc.Upload(t.Context(), bytes.NewReader(data), "b.png", "image/png")
	if err != nil {
		t.Fatalf("second upload failed: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("dedup failed: %s != %s", first.ID, second.ID)
	}
}

func TestUploadServiceRejectDisallowedMIME(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUploadService(tmpDir, 20, []string{"image/png"})

	_, err := svc.Upload(t.Context(), bytes.NewReader([]byte("data")), "test.bmp", "image/bmp")
	if err == nil {
		t.Fatal("expected error for disallowed MIME type")
	}
}

func TestUploadServiceLoad(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUploadService(tmpDir, 20, []string{"image/png"})

	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	upload, err := svc.Upload(t.Context(), bytes.NewReader(data), "test.png", "image/png")
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	loaded, mime, err := svc.Load(t.Context(), upload.ID)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !bytes.Equal(loaded, data) {
		t.Errorf("loaded data mismatch")
	}
	if mime != "image/png" {
		t.Errorf("expected image/png, got %s", mime)
	}
}

func TestUploadServiceGC(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUploadService(tmpDir, 20, []string{"image/png"})

	// Create a file directly (simulate orphaned upload)
	path := filepath.Join(tmpDir, "orphan.png")
	if err := os.WriteFile(path, []byte("orphan"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// GC should not delete files newer than retention
	deleted, err := svc.GCSweep(7 * 24 * 3600) // 7 days
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deletions for new files, got %d", len(deleted))
	}

	// Verify file still exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should still exist: %v", err)
	}
}
