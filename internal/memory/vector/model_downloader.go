package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"log/slog"
)

// CachedModel represents a locally cached model.
type CachedModel struct {
	// Path to the model directory.
	Path string
	// Path to the ONNX model file.
	ONNXPath string
	// Path to the tokenizer file.
	TokenizerPath string
	// Model metadata from HuggingFace API.
	LastModified time.Time
	// Commit SHA of the model version on HuggingFace.
	CommitSHA string
}

// ModelDownloader manages downloading and caching models from HuggingFace.
type ModelDownloader struct {
	cacheDir string
	client   *http.Client
	logger   *slog.Logger
	apiBase  string
}

// NewModelDownloader creates a model downloader with the given cache directory.
func NewModelDownloader(cacheDir string, logger *slog.Logger) *ModelDownloader {
	if cacheDir == "" {
		// Default to ~/.meept/models
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".meept", "models")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ModelDownloader{
		cacheDir: cacheDir,
		client: &http.Client{
			Timeout: 30 * time.Minute,
		},
		logger:  logger,
		apiBase: "https://huggingface.co",
	}
}

// DownloadModel downloads a model from HuggingFace if not already cached,
// returning a CachedModel pointing to the local files.
func (d *ModelDownloader) DownloadModel(ctx context.Context, modelID string) (*CachedModel, error) {
	// Check if already cached
	if cached, err := d.isCached(modelID); err == nil && cached != nil {
		return cached, nil
	}

	modelInfo, ok := GetModelInfo(modelID)
	if !ok {
		return nil, fmt.Errorf("unknown model: %s", modelID)
	}

	modelDir := filepath.Join(d.cacheDir, modelID)
	//nolint:gosec // user config directory permissions
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create model directory: %w", err)
	}

	// Download model files
	hfPath := url.QueryEscape(modelID)
	hfHUB := d.apiBase

	d.logger.Info("downloading model", "model", modelID)

	// Download tokenizer
	tokenizerLocal := filepath.Join(modelDir, modelInfo.TokenizerPath)
	tokenizerDir := filepath.Dir(tokenizerLocal)
	if tokenizerDir != modelDir {
		//nolint:gosec // user config directory permissions
		if err := os.MkdirAll(tokenizerDir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create tokenizer directory: %w", err)
		}
	}

	if err := d.downloadFile(ctx,
		fmt.Sprintf("%s/%s/resolve/main/%s", hfHUB, hfPath, modelInfo.TokenizerPath),
		tokenizerLocal, 10*time.Minute,
	); err != nil {
		d.logger.Warn("tokenizer download failed, skipping", "error", err)
	}

	// Download ONNX model
	onnxLocal := filepath.Join(modelDir, modelInfo.ONNXModelPath)
	onnxDir := filepath.Dir(onnxLocal)
	if onnxDir != modelDir {
		//nolint:gosec // user config directory permissions
		if err := os.MkdirAll(onnxDir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create onnx directory: %w", err)
		}
	}

	if err := d.downloadFile(ctx,
		fmt.Sprintf("%s/%s/resolve/main/%s", hfHUB, hfPath, modelInfo.ONNXModelPath),
		onnxLocal, 30*time.Minute,
	); err != nil {
		return nil, fmt.Errorf("failed to download ONNX model: %w", err)
	}

	// Get commit info
	commitSHA, modified, err := d.getCommitInfo(modelID)
	if err != nil {
		d.logger.Warn("failed to get commit info", "error", err)
	}

	return &CachedModel{
		Path:          modelDir,
		ONNXPath:      onnxLocal,
		TokenizerPath: tokenizerLocal,
		LastModified:  modified,
		CommitSHA:     commitSHA,
	}, nil
}

// CheckForUpdates checks if a newer version of the model is available on HuggingFace.
// Returns the new commit SHA if update is available, empty string if up-to-date.
func (d *ModelDownloader) CheckForUpdates(modelID string) (newCommit string, err error) {
	if !HasModel(modelID) {
		return "", fmt.Errorf("unknown model: %s", modelID)
	}

	cachedCommit, err := d.getCachedCommit(modelID)
	if err != nil {
		return "", err
	}
	if cachedCommit == "" {
		// Not cached, can't compare
		return "", nil
	}

	// Get current commit from HuggingFace API
	apiURL := fmt.Sprintf("%s/api/models/%s", d.apiBase, modelID)
	resp, err := d.client.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch model info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HuggingFace API returned status %d", resp.StatusCode)
	}

	var hfInfo HFModelInfo
	if err := json.NewDecoder(resp.Body).Decode(&hfInfo); err != nil {
		return "", fmt.Errorf("failed to parse HuggingFace response: %w", err)
	}

	if hfInfo.CommitSHA != cachedCommit {
		return hfInfo.CommitSHA, nil
	}
	return "", nil // up-to-date
}

// isCached checks if the model files exist locally and are valid.
func (d *ModelDownloader) isCached(modelID string) (*CachedModel, error) {
	modelInfo, ok := GetModelInfo(modelID)
	if !ok {
		return nil, fmt.Errorf("unknown model: %s", modelID)
	}

	modelDir := filepath.Join(d.cacheDir, modelID)
	onnxPath := filepath.Join(modelDir, modelInfo.ONNXModelPath)

	// Check ONNX file exists and is non-zero size
	info, err := os.Stat(onnxPath)
	if err != nil || info.Size() == 0 {
		return nil, fmt.Errorf("ONNX model not cached")
	}

	// Cache directory exists, return CachedModel
	cached := &CachedModel{
		Path:     modelDir,
		ONNXPath: onnxPath,
	}

	// Try to locate tokenizer
	tokenizerPath := filepath.Join(modelDir, modelInfo.TokenizerPath)
	if _, err := os.Stat(tokenizerPath); err == nil {
		cached.TokenizerPath = tokenizerPath
	}

	// Read cached commit
	if commitSHA, err := d.getCachedCommit(modelID); err == nil {
		cached.CommitSHA = commitSHA
	}

	cached.LastModified = info.ModTime()
	return cached, nil
}

// downloadFile downloads a URL to a local path with progress logging.
func (d *ModelDownloader) downloadFile(ctx context.Context, url, localPath string, maxDuration time.Duration) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Make context timeout-aware for large downloads
	ctx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "meept-vector-memory/1.0")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: status %d", url, resp.StatusCode)
	}

	// Get total size for progress
	totalSize := resp.ContentLength

	// Create output file (atomically via temp file)
	tmpPath := localPath + ".tmp"
	//nolint:gosec // user config file permissions
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	var writer io.Writer = outFile
	if totalSize > 0 {
		writer = &progressWriter{
			w:            outFile,
			total:        totalSize,
			totalWritten: 0,
			logger:       d.logger,
			filename:     filepath.Base(localPath),
		}
	}

	written, err := io.Copy(writer, resp.Body)
	if err != nil {
		outFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write file: %w", err)
	}
	outFile.Close()

	// Move temp file to final path
	if err := os.Rename(tmpPath, localPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize file: %w", err)
	}

	if totalSize > 0 {
		d.logger.Info("download complete", "file", filepath.Base(localPath), "size_mb", float64(written)/1e6)
	}

	return nil
}

// getCommitInfo fetches the latest commit SHA and modification time from HuggingFace.
func (d *ModelDownloader) getCommitInfo(modelID string) (commitSHA string, modified time.Time, err error) {
	apiURL := fmt.Sprintf("%s/api/models/%s", d.apiBase, modelID)
	resp, err := d.client.Get(apiURL)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var hfInfo HFModelInfo
	if err := json.NewDecoder(resp.Body).Decode(&hfInfo); err != nil {
		return "", time.Time{}, fmt.Errorf("parse response: %w", err)
	}
	return hfInfo.CommitSHA, hfInfo.LastModified, nil
}

// getCachedCommit reads the commit SHA from a cached model's metadata.
// Returns empty string if no cached metadata exists (not an error).
func (d *ModelDownloader) getCachedCommit(modelID string) (string, error) {
	metaPath := filepath.Join(d.cacheDir, modelID, ".commit.sha")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return "", nil // Not cached yet, not a hard error
	}
	return string(data), nil
}

// saveCommit stores the commit SHA to disk.
func (d *ModelDownloader) saveCommit(modelID, commitSHA string) error {
	dir := filepath.Join(d.cacheDir, modelID)
	//nolint:gosec // user config directory permissions
	return os.WriteFile(filepath.Join(dir, ".commit.sha"), []byte(commitSHA), 0o644)
}

// HFModelInfo holds model information from the HuggingFace API.
type HFModelInfo struct {
	ModelID      string    `json:"modelId"`
	CommitSHA    string    `json:"sha"`
	LastModified time.Time `json:"lastModified"`
	Private      bool      `json:"private"`
	SpaceSDK     string    `json:"sdk"`
}

// progressWriter wraps an io.Writer and logs progress periodically.
type progressWriter struct {
	w            io.Writer
	total        int64
	totalWritten int64
	logger       *slog.Logger
	filename     string
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.w.Write(p)
	pw.totalWritten += int64(n)

	// Log every ~10% progress, or at 100%
	if pw.totalWritten == int64(n) && pw.totalWritten < pw.total {
		// First write
		pw.logger.Info("downloading model file", "file", pw.filename, "downloaded_mb", float64(pw.totalWritten)/1e6, "total_mb", float64(pw.total)/1e6)
	} else if pw.total > 0 {
		digit := int(pw.totalWritten * 10 / pw.total)
		prev := int((pw.totalWritten - int64(n)) * 10 / pw.total)
		if digit > prev {
			pw.logger.Info("downloading model file", "file", pw.filename, "progress", strconv.Itoa(digit*10)+"%")
		}
	}

	return n, err
}
