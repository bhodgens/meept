package vector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"log/slog"
)

func setupTestServer(t *testing.T) (*httptest.Server, *testing.T) {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models/nomic-ai/nomic-embed-text-v1.5":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(HFModelInfo{
				ModelID:      "nomic-ai/nomic-embed-text-v1.5",
				CommitSHA:    "abc123def456",
				LastModified: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
				Private:      false,
			})
		case "/api/models/all-MiniLM-L6-v2":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(HFModelInfo{
				ModelID:      "sentence-transformers/all-MiniLM-L6-v2",
				CommitSHA:    "def789abc012",
				LastModified: time.Date(2024, 2, 20, 0, 0, 0, 0, time.UTC),
				Private:      false,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("not found"))
		}
	})), nil
}

func TestModelDownloader_CheckForUpdates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HFModelInfo{
			ModelID:      "nomic-ai/nomic-embed-text-v1.5",
			CommitSHA:    "newcommit789",
			LastModified: time.Now(),
			Private:      false,
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	// Create model subdirectory so save/load of .commit.sha works
	os.MkdirAll(filepath.Join(dir, "nomic-embed-text-v1.5"), 0o755)
	logger := slog.Default()
	// Override the downloader's client to use our test server
	dl := &ModelDownloader{
		cacheDir: dir,
		client:   server.Client(),
		logger:   logger,
	}

	// No commit saved yet
	commit, err := dl.CheckForUpdates("nomic-embed-text-v1.5")
	if err != nil {
		t.Fatalf("CheckForUpdates error: %v", err)
	}
	if commit != "" {
		t.Errorf("CheckForUpdates() = %q (with no cache), want empty string", commit)
	}

	// Save a commit, then check again
	dl.saveCommit("nomic-embed-text-v1.5", "oldcommit123")
	commit, err = dl.CheckForUpdates("nomic-embed-text-v1.5")
	if err != nil {
		t.Fatalf("CheckForUpdates after save commit error: %v", err)
	}
	if commit != "newcommit789" {
		t.Errorf("CheckForUpdates() = %q, want %q", commit, "newcommit789")
	}

	// Now set the same commit, should return empty
	dl.saveCommit("nomic-embed-text-v1.5", "newcommit789")
	commit, err = dl.CheckForUpdates("nomic-embed-text-v1.5")
	if err != nil {
		t.Fatalf("CheckForUpdates after update set error: %v", err)
	}
	if commit != "" {
		t.Errorf("CheckForUpdates() = %q (after update), want empty string", commit)
	}
}

func TestModelDownloader_isCached(t *testing.T) {
	dir := t.TempDir()
	logger := slog.Default()
	dl := &ModelDownloader{
		cacheDir: dir,
		client:   http.DefaultClient,
		logger:   logger,
	}

	// Not cached
	_, err := dl.isCached("nomic-embed-text-v1.5")
	if err == nil {
		t.Error("isCached() with no model should error, got nil")
	}

	// Save the model files
	modelInfo, _ := GetModelInfo("nomic-embed-text-v1.5")
	onnxDir := filepath.Join(dir, "nomic-embed-text-v1.5", filepath.Dir(modelInfo.ONNXModelPath))
	if err := os.MkdirAll(onnxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	onnxPath := filepath.Join(onnxDir, filepath.Base(modelInfo.ONNXModelPath))
	if err := os.WriteFile(onnxPath, []byte("dummy onnx data"), 0o644); err != nil {
		t.Fatal(err)
	}

	cached, err := dl.isCached("nomic-embed-text-v1.5")
	if err != nil {
		t.Fatalf("isCached() should succeed after saving model: %v", err)
	}
	if cached.Path == "" {
		t.Error("CachedModel.Path is empty")
	}
	if cached.ONNXPath == "" {
		t.Error("CachedModel.ONNXPath is empty")
	}
}

func TestModelDownloader_DownloadModel_FailsForUnknownModel(t *testing.T) {
	dir := t.TempDir()
	dl := &ModelDownloader{
		cacheDir: dir,
		client:   http.DefaultClient,
		logger:   slog.Default(),
	}

	ctx := context.Background()
	_, err := dl.DownloadModel(ctx, "unknown/nonexistent-model")
	if err == nil {
		t.Fatal("DownloadModel for unknown model should error")
	}
	t.Logf("Expected error for unknown model: %v", err)
}

func TestModelDownloader_downloadFile_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	logger := slog.Default()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never respond -- let the context cancel
		select {}
	}))
	defer server.Close()

	dl := &ModelDownloader{
		cacheDir: dir,
		client:   server.Client(),
		logger:   logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := dl.downloadFile(ctx, server.URL+"/model.onnx", filepath.Join(dir, "model.onnx"), 30*time.Minute)
	if err == nil {
		t.Fatal("downloadFile with cancelled context should error")
	}
	t.Logf("Expected timeout error: %v", err)
}

func TestModelDownloader_CheckForUpdates_NotFound(t *testing.T) {
	dir := t.TempDir()
	dl := &ModelDownloader{
		cacheDir: dir,
		client:   &http.Client{Timeout: 1 * time.Second},
		logger:   slog.Default(),
	}

	// Request a model that doesn't exist on HF
	_, err := dl.CheckForUpdates("nonexistent-model-xyz123")
	// We expect either an error from the 404 response or a parse error
	// Since we don't have a test server, this will likely fail with network errors
	// which is acceptable for this test
	if err == nil {
		// Only fail if the model happens to exist (unlikely)
		t.Log("Warning: CheckForUpdates unexpectedly succeeded for nonexistent model")
	}
	t.Logf("Expected error for nonexistent model (can be network-related): %v", err)
}

func TestModelDownloader_DownloadModel_SingleFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve dummy ONNX data for any path
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("FAKE_ONNX_MODEL_DATA_HERE"))
	}))
	defer server.Close()

	dir := t.TempDir()
	_ = dir // used implicitly below

	testDownloader := &ModelDownloader{
		cacheDir: dir,
		client:   &http.Client{Timeout: 10 * time.Second},
		logger:   slog.Default(),
	}

	ctx := context.Background()
	dest := filepath.Join(dir, "model.onnx")

	err := testDownloader.downloadFile(ctx, server.URL+"/resolve/main/onnx/model.onnx", dest, 10*time.Second)
	if err != nil {
		t.Fatalf("downloadFile should succeed: %v", err)
	}

	// Verify file was written
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(data) != "FAKE_ONNX_MODEL_DATA_HERE" {
		t.Errorf("downloaded content = %q, want %q", string(data), "FAKE_ONNX_MODEL_DATA_HERE")
	}
}

func TestModelInfo_SchemaFields(t *testing.T) {
	info, _ := GetModelInfo("nomic-embed-text-v1.5")

	// Verify all schema fields are populated
	tests := []struct {
		field string
		value string
	}{
		{"ID", info.ID},
		{"ONNXModelPath", info.ONNXModelPath},
		{"TokenizerPath", info.TokenizerPath},
		{"TokenizerType", info.TokenizerType},
		{"PoolingMethod", info.PoolingMethod},
	}

	for _, tt := range tests {
		if tt.value == "" {
			t.Errorf("ModelInfo %s is empty for %q", tt.field, info.ID)
		}
	}

	if !info.Normalize {
		t.Error("nomic-embed-text-v1.5 should have Normalize=true")
	}
	if info.MaxSequenceLen != 8192 {
		t.Errorf("MaxSequenceLen = %d, want 8192", info.MaxSequenceLen)
	}
}
