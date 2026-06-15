package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDownloadFile_AtomicSuccess verifies a successful download leaves the
// final file in place with no leftover .part file.
func TestDownloadFile_AtomicSuccess(t *testing.T) {
	body := strings.Repeat("A", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "voice.onnx")

	if err := downloadFile(context.Background(), srv.URL, dest); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != body {
		t.Fatalf("downloaded body mismatch: got %d bytes, want %d", len(got), len(body))
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf("expected .part file removed; stat err=%v", err)
	}
}

// TestDownloadFile_TruncationRemoved verifies a Content-Length mismatch
// causes downloadFile to remove the partial file and return an error,
// leaving no orphan file behind.
func TestDownloadFile_TruncationRemoved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Lie about Content-Length so verification fails after the copy.
		w.Header().Set("Content-Length", "99999")
		_, _ = io.WriteString(w, "too short")
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "voice.onnx")

	err := downloadFile(context.Background(), srv.URL, dest)
	if err == nil {
		t.Fatal("expected truncation error, got nil")
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatal("dest file should not exist after truncation failure")
	}
	if _, statErr := os.Stat(dest + ".part"); statErr == nil {
		t.Fatal(".part file should not exist after truncation failure")
	}
}

// TestDownloadFile_HTTPErrror verifies a non-200 response surfaces an error
// and writes no files.
func TestDownloadFile_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "voice.onnx")

	err := downloadFile(context.Background(), srv.URL, dest)
	if err == nil {
		t.Fatal("expected error on 404, got nil")
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatal("dest file should not exist on HTTP error")
	}
}

// TestDownloadFile_OverwritesPartial verifies that a stale .part file from a
// previous failed run is replaced rather than treated as a complete download.
func TestDownloadFile_OverwritesPartial(t *testing.T) {
	body := "fresh content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "voice.onnx")
	// Simulate a leftover partial from a prior interrupted download.
	if err := os.WriteFile(dest+".part", []byte("stale partial"), 0o644); err != nil {
		t.Fatalf("seed .part: %v", err)
	}

	if err := downloadFile(context.Background(), srv.URL, dest); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != body {
		t.Fatalf("expected overwritten content %q, got %q", body, string(got))
	}
}
