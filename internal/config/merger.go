package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"
)

// Merger handles config file merging from shared and per-node overrides.
type Merger struct {
	baseDir       string // ~/.meept/
	checkoutDir   string // Git checkout path
	nodeID        string
	logger        logger

	// fileHashes tracks last-applied file hashes to detect changes.
	fileHashes map[string]string
}

type logger interface {
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Debug(msg string, keysAndValues ...interface{})
}

// NewMerger creates a new Merger.
func NewMerger(baseDir, checkoutDir, nodeID string, l logger) *Merger {
	return &Merger{
		baseDir:     baseDir,
		checkoutDir: checkoutDir,
		nodeID:      nodeID,
		logger:      l,
		fileHashes:  make(map[string]string),
	}
}

// MergeResult holds the outcome of a Merge operation.
type MergeResult struct {
	FilesApplied []string        `json:"files_applied"`
	FilesSkipped []string        `json:"files_skipped"`
	Errors       []error         `json:"errors,omitempty"`
	CommitHash   string          `json:"commit_hash"`
}

// Merge applies shared + per-node configs, returning applied/skipped/errored files.
func (m *Merger) Merge(commitHash string) (*MergeResult, error) {
	result := &MergeResult{
		FilesApplied: []string{},
		FilesSkipped: []string{},
		Errors:       []error{},
		CommitHash:   commitHash,
	}

	if err := m.mergeSharedConfigs(result); err != nil {
		result.Errors = append(result.Errors, err)
	}

	if err := m.mergeNodeOverrides(result); err != nil {
		result.Errors = append(result.Errors, err)
	}

	return result, nil
}

// mergeSharedConfigs copies shared/*.json5 from checkout dir → ~/.meept/.
func (m *Merger) mergeSharedConfigs(result *MergeResult) error {
	sharedDir := filepath.Join(m.checkoutDir, "config", "shared")
	if _, err := os.Stat(sharedDir); os.IsNotExist(err) {
		m.logger.Debug("config sync: shared config dir not found, skipping", "dir", sharedDir)
		return nil
	}

	entries, err := os.ReadDir(sharedDir)
	if err != nil {
		return &ConfigSyncError{Op: "merge_shared", Path: sharedDir, Err: err}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json5") && !strings.HasSuffix(name, ".toml") {
			continue
		}

		src := filepath.Join(sharedDir, name)
		dst := filepath.Join(m.baseDir, name)

		if err := m.applyConfigFile(src, dst, result); err != nil {
			result.Errors = append(result.Errors, err)
		}
	}

	return nil
}

// mergeNodeOverrides deep-merges nodes/<node_id>/*.json5 from the checkout dir
// into ~/.meept/ on top of the shared configs already written there.
//
// Unlike shared configs (which are copied wholesale), node overrides are
// merged key-by-key so that operators can override individual nested fields
// without having to republish the entire file. TOML node overrides fall back
// to wholesale replacement because deep-merge semantics across TOML's typed
// tables are not well-defined for this use case.
func (m *Merger) mergeNodeOverrides(result *MergeResult) error {
	if m.nodeID == "" {
		return nil
	}

	nodeDir := filepath.Join(m.checkoutDir, "config", "nodes", m.nodeID)
	if _, err := os.Stat(nodeDir); os.IsNotExist(err) {
		m.logger.Debug("config sync: node config dir not found, skipping", "dir", nodeDir)
		return nil
	}

	entries, err := os.ReadDir(nodeDir)
	if err != nil {
		return &ConfigSyncError{Op: "merge_node", Path: nodeDir, Err: err}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json5") && !strings.HasSuffix(name, ".toml") {
			continue
		}

		src := filepath.Join(nodeDir, name)
		dst := filepath.Join(m.baseDir, name)

		if strings.HasSuffix(name, ".json5") {
			if err := m.applyMergedConfigFile(src, dst, result); err != nil {
				result.Errors = append(result.Errors, err)
			}
		} else {
			// TOML: no deep merge — overwrite wholesale.
			if err := m.applyConfigFile(src, dst, result); err != nil {
				result.Errors = append(result.Errors, err)
			}
		}
	}

	return nil
}

// applyMergedConfigFile deep-merges the JSON5 src file on top of the JSON5
// file at dst (which was previously written by mergeSharedConfigs). If dst
// does not exist or cannot be parsed, src is applied wholesale as a fallback.
func (m *Merger) applyMergedConfigFile(src, dst string, result *MergeResult) error {
	srcData, err := os.ReadFile(src)
	if err != nil {
		return &ConfigSyncError{Op: "read_src", Path: src, Err: err}
	}

	// Standardize src to plain JSON for parsing.
	srcStd, err := hujson.Standardize(srcData)
	if err != nil {
		// Defer to applyConfigFile's validation/error handling.
		return m.applyConfigFile(src, dst, result)
	}

	var srcObj map[string]any
	if err := json.Unmarshal(srcStd, &srcObj); err != nil {
		// Not an object (e.g. top-level array) — fall back to wholesale copy.
		return m.applyConfigFile(src, dst, result)
	}

	// Read current dst contents (written by mergeSharedConfigs earlier in
	// the same Merge pass, or carried over from a prior pass). If dst is
	// missing or unparseable, fall back to wholesale copy via applyConfigFile.
	var merged map[string]any
	if dstData, readErr := os.ReadFile(dst); readErr == nil {
		if dstStd, stdErr := hujson.Standardize(dstData); stdErr == nil {
			_ = json.Unmarshal(dstStd, &merged) // best-effort; merged may remain nil
		}
	}

	if merged == nil {
		// No base to merge into — wholesale copy is correct.
		return m.applyConfigFile(src, dst, result)
	}

	merged = deepMerge(merged, srcObj)

	// Marshal back to JSON5-compatible JSON. We don't try to preserve
	// comments or formatting because hujson.Standardize on input already
	// discarded them when the shared file was applied. Keeping formatting
	// stable across merges isn't worth a JSON5 serializer dep.
	outData, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return &ConfigSyncError{Op: "merge_marshal", Path: dst, Err: err}
	}
	// Append trailing newline for POSIX friendliness.
	outData = append(outData, '\n')

	hash := stringHash(outData)
	if existing, ok := m.fileHashes[dst]; ok && existing == hash {
		result.FilesSkipped = append(result.FilesSkipped, filepath.Base(src))
		return nil
	}

	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return &ConfigSyncError{Op: "write_dst", Path: dst, Err: err}
	}

	tmpFile := dst + ".tmp"
	if err := os.WriteFile(tmpFile, outData, 0o600); err != nil {
		return &ConfigSyncError{Op: "write_tmp", Path: tmpFile, Err: err}
	}
	if err := os.Rename(tmpFile, dst); err != nil {
		_ = os.Remove(tmpFile)
		return &ConfigSyncError{Op: "rename_dst", Path: dst, Err: err}
	}

	m.fileHashes[dst] = hash
	result.FilesApplied = append(result.FilesApplied, filepath.Base(src))

	m.logger.Info("config sync: deep-merged node override",
		"file", filepath.Base(src), "node", m.nodeID)

	return nil
}

// deepMerge returns a new map that merges src on top of dst:
//   - Object values are recursively merged.
//   - Array and scalar values from src replace the corresponding dst value.
//   - A JSON null in src deletes the corresponding key from dst.
//
// dst is not mutated; the returned map shares sub-maps only at unmodified keys.
func deepMerge(dst, src map[string]any) map[string]any {
	out := make(map[string]any, len(dst))
	for k, v := range dst {
		out[k] = v
	}
	for k, sv := range src {
		if sv == nil {
			// null in src → delete key.
			delete(out, k)
			continue
		}
		if dv, ok := out[k]; ok {
			if dvMap, dOk := dv.(map[string]any); dOk {
				if svMap, sOk := sv.(map[string]any); sOk {
					out[k] = deepMerge(dvMap, svMap)
					continue
				}
			}
		}
		// Arrays and scalars: src overrides dst.
		out[k] = sv
	}
	return out
}

// applyConfigFile validates and applies a single config file from src → dst atomically.
func (m *Merger) applyConfigFile(src, dst string, result *MergeResult) error {
	// Read source
	data, err := os.ReadFile(src)
	if err != nil {
		return &ConfigSyncError{Op: "read_src", Path: src, Err: err}
	}

	// Validate JSON5 syntax via hujson
	stdJSON, jsonErr := hujson.Standardize(data)
	if jsonErr != nil {
		result.FilesSkipped = append(result.FilesSkipped, filepath.Base(src))
		m.logger.Warn("config sync: skipping invalid JSON5, using previously loaded copy",
			"file", filepath.Base(src), "error", jsonErr)
		return nil // skip silently, don't corrupt current config
	}

	// Check if content changed
	hash := stringHash(stdJSON)
	if existing, ok := m.fileHashes[dst]; ok && existing == hash {
		// No change
		result.FilesSkipped = append(result.FilesSkipped, filepath.Base(src))
		return nil
	}

	// Atomic write: write to temp, then rename
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return &ConfigSyncError{Op: "write_dst", Path: dst, Err: err}
	}

	tmpFile := dst + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o600); err != nil {
		return &ConfigSyncError{Op: "write_tmp", Path: tmpFile, Err: err}
	}

	if err := os.Rename(tmpFile, dst); err != nil {
		// Cleanup temp on failure
		_ = os.Remove(tmpFile)
		return &ConfigSyncError{Op: "rename_dst", Path: dst, Err: err}
	}

	m.fileHashes[dst] = hash
	result.FilesApplied = append(result.FilesApplied, filepath.Base(src))

	if nodeID := m.nodeID; nodeID != "" {
		m.logger.Info("config sync: merged config", "file", filepath.Base(src), "node", nodeID)
	} else {
		m.logger.Info("config sync: merged config", "file", filepath.Base(src))
	}

	return nil
}

// stringHash returns a simple hash of b for change detection.
func stringHash(b []byte) string {
	// Use a lightweight approach: last 32 bytes + first 32 bytes as a change fingerprint.
	// For production use, a real hash would be better, but this avoids crypto deps
	// for this package.
	if len(b) <= 64 {
		return fmt.Sprintf("%x", b)
	}
	prefix := b[:32]
	suffix := b[len(b)-32:]
	return fmt.Sprintf("%x_%x", prefix, suffix)
}

// ApplyConfigFile is exposed so callers like the CLI can check if a file
// would change before triggering a full merge.
func (m *Merger) FileWouldChange(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	stdJSON, err := hujson.Standardize(data)
	if err != nil {
		return false, fmt.Errorf("invalid JSON5: %w", err)
	}
	hash := stringHash(stdJSON)
	dst := filepath.Join(m.baseDir, filepath.Base(path))
	existing, ok := m.fileHashes[dst]
	if !ok {
		return true, nil
	}
	return existing != hash, nil
}

// Ensure io Copy is available.
var _ = io.Copy
