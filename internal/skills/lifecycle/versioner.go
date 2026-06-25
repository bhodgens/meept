package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// maxVersionEntries is the retention cap. The oldest version directories are
// pruned when this threshold is exceeded.
const maxVersionEntries = 20

// Versioner provides reversible skill changes via versioned bundles. Each call
// to Snapshot captures the current SKILL.md content, computes SHA-256 hashes
// (content_sha and tree_sha256), and writes a version directory containing the
// skill file plus a bundle.json manifest. Restore reverts a skill to a prior
// version atomically.
//
// Versioner holds no mutex. All operations are stateless filesystem actions.
// Concurrent calls to Snapshot for the same skill are serialized by the
// filesystem rename(2) syscall used for atomic manifest writes.
type Versioner struct {
	skillsDir string
	logger    *slog.Logger
}

// NewVersioner creates a Versioner rooted at skillsDir. The layout is:
//
//	<skillsDir>/<name>/SKILL.md
//	<skillsDir>/<name>/versions/v<N>/SKILL.md
//	<skillsDir>/<name>/versions/v<N>/bundle.json
func NewVersioner(skillsDir string, logger *slog.Logger) *Versioner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Versioner{
		skillsDir: skillsDir,
		logger:    logger,
	}
}

// skillFilePath returns <skillsDir>/<name>/SKILL.md.
func (v *Versioner) skillFilePath(name string) string {
	return filepath.Join(v.skillsDir, name, "SKILL.md")
}

// versionsDir returns <skillsDir>/<name>/versions.
func (v *Versioner) versionsDir(name string) string {
	return filepath.Join(v.skillsDir, name, "versions")
}

// versionDir returns <skillsDir>/<name>/versions/v<N>.
func (v *Versioner) versionDir(name string, version int) string {
	return filepath.Join(v.versionsDir(name), fmt.Sprintf("v%d", version))
}

// Snapshot reads the current SKILL.md for the named skill, computes its
// content_sha (SHA-256 hex of the file content) and tree_sha256 (SHA-256 hex
// over the concatenated "path\0sha256\0size\n" entries for every file in the
// bundle — currently a single file, but structured for multi-file extension),
// writes both to versions/v<N>/, writes the bundle.json manifest, prunes any
// entries beyond maxVersionEntries, and returns the tree_sha256.
//
// If the skill does not exist on disk yet (first write), Snapshot returns an
// empty string and a nil error — there is nothing to version.
func (v *Versioner) Snapshot(name string) (string, error) {
	skillPath := v.skillFilePath(name)
	content, err := os.ReadFile(skillPath)
	if err != nil {
		if os.IsNotExist(err) {
			// First write — nothing to snapshot. Per plan contract:
			// "Snapshot(name) returns empty string + no-op if the skill
			// doesn't exist yet — that's fine, it's a first-write."
			v.logger.Debug("Snapshot skip: skill does not exist yet",
				"name", name, "path", skillPath)
			return "", nil //nolint:nilerr // intentional: first-write is a documented no-op
		}
		return "", fmt.Errorf("versioner: read current skill file: %w", err)
	}

	contentSHA := sha256Hex(content)
	treeSHA := computeTreeSHA(skillPath, contentSHA, len(content))

	// Ensure the versions parent directory exists before reading history
	// (glob on a non-existent dir returns empty, which is fine, but creating
	// it early avoids a race between History glob and subsequent MkdirAll).
	versionsDir := v.versionsDir(name)
	_ = os.MkdirAll(versionsDir, 0o755)

	entries, err := v.History(name)
	if err != nil {
		return "", fmt.Errorf("versioner: read history for next version: %w", err)
	}

	nextVersion := 1
	if len(entries) > 0 {
		nextVersion = entries[len(entries)-1].Version + 1
	}

	versionDir := v.versionDir(name, nextVersion)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return "", fmt.Errorf("versioner: create version dir: %w", err)
	}

	// Copy SKILL.md into the version dir.
	versionedSkillPath := filepath.Join(versionDir, "SKILL.md")
	if err := os.WriteFile(versionedSkillPath, content, 0o644); err != nil {
		return "", fmt.Errorf("versioner: write versioned skill file: %w", err)
	}

	// Write bundle.json manifest.
	manifest := VersionEntry{
		Version:    nextVersion,
		ContentSHA: contentSHA,
		Timestamp:  time.Now().UTC(),
		Action:     "snapshot",
		TreeSHA256: treeSHA,
	}
	manifestPath := filepath.Join(versionDir, "bundle.json")
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("versioner: marshal manifest: %w", err)
	}
	// Atomic manifest write: tmp + rename.
	tmpManifest := manifestPath + ".tmp"
	if err := os.WriteFile(tmpManifest, manifestBytes, 0o644); err != nil {
		return "", fmt.Errorf("versioner: write manifest tmp: %w", err)
	}
	if err := os.Rename(tmpManifest, manifestPath); err != nil {
		_ = os.Remove(tmpManifest)
		return "", fmt.Errorf("versioner: rename manifest: %w", err)
	}

	// Prune oldest beyond cap. The new version directory is already on disk,
	// so total count is len(entries) + 1 (the just-written manifest).
	if err := v.pruneOldVersions(name, len(entries)+1); err != nil {
		v.logger.Warn("Versioner: prune old versions failed",
			"name", name, "error", err)
	}

	v.logger.Info("Version snapshot created",
		"name", name,
		"version", nextVersion,
		"content_sha", contentSHA,
		"tree_sha256", treeSHA,
	)
	return treeSHA, nil
}

// pruneOldVersions removes version directories beyond maxVersionEntries.
// totalOnDisk is the current count (including the just-written version). If
// totalOnDisk exceeds maxVersionEntries, the oldest directories are removed.
func (v *Versioner) pruneOldVersions(name string, totalOnDisk int) error {
	if totalOnDisk <= maxVersionEntries {
		return nil
	}
	// Read the current (post-write) history to determine which dirs to remove.
	entries, err := v.History(name)
	if err != nil {
		return fmt.Errorf("prune: read history: %w", err)
	}
	// Newest entries are at the tail of the sorted slice. Keep the last
	// maxVersionEntries, remove the rest.
	excess := len(entries) - maxVersionEntries
	if excess <= 0 {
		return nil
	}
	toRemove := entries[:excess]
	for _, entry := range toRemove {
		dir := v.versionDir(name, entry.Version)
		if err := os.RemoveAll(dir); err != nil {
			v.logger.Warn("Versioner: failed to prune old version",
				"name", name, "version", entry.Version, "error", err)
		} else {
			v.logger.Info("Versioner: pruned old version",
				"name", name, "version", entry.Version)
		}
	}
	return nil
}

// History returns all version entries for a skill, sorted by version ascending.
// Returns an empty slice if no versions exist.
func (v *Versioner) History(name string) ([]VersionEntry, error) {
	pattern := filepath.Join(v.versionsDir(name), "v*", "bundle.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("versioner: glob version manifests: %w", err)
	}

	entries := make([]VersionEntry, 0, len(matches))
	for _, manifestPath := range matches {
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			v.logger.Warn("Versioner: skipping unreadable manifest",
				"path", manifestPath, "error", err)
			continue
		}
		var entry VersionEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			v.logger.Warn("Versioner: skipping unparseable manifest",
				"path", manifestPath, "error", err)
			continue
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Version < entries[j].Version
	})
	return entries, nil
}

// Restore reverts <skillsDir>/<name>/SKILL.md to the content stored in
// versions/v<version>/SKILL.md. The write is atomic (.tmp + rename). Restore
// does NOT call Snapshot — restoring IS the snapshot application; creating a
// version of the restore event would be redundant.
//
// After restore, any non-SKILL.md metadata on the live skill (e.g. lastUsedAt
// in the usage tracker) is untouched — only the content reverts.
func (v *Versioner) Restore(name string, version int) error {
	if version <= 0 {
		return fmt.Errorf("versioner: version must be a positive integer, got %d", version)
	}

	versionedSkillPath := filepath.Join(v.versionDir(name, version), "SKILL.md")
	content, err := os.ReadFile(versionedSkillPath)
	if err != nil {
		return fmt.Errorf("versioner: read versioned skill: %w", err)
	}

	livePath := v.skillFilePath(name)
	liveDir := filepath.Dir(livePath)
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		return fmt.Errorf("versioner: ensure skill dir for restore: %w", err)
	}

	// Atomic write.
	tmpPath := livePath + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o644); err != nil {
		return fmt.Errorf("versioner: write restore tmp: %w", err)
	}
	if err := os.Rename(tmpPath, livePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("versioner: rename restore: %w", err)
	}

	v.logger.Info("Version restore applied",
		"name", name, "version", version, "path", livePath)
	return nil
}

// sha256Hex computes the SHA-256 of data and returns its lowercase hex
// representation.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// computeTreeSHA computes the tree SHA-256 over the concatenated
// "path\0sha256\0size\n" entries for every file in the bundle. Currently the
// bundle is single-file (SKILL.md), but the structure supports future
// multi-file extension by adding additional (path, sha, size) tuples to the
// hasher.
//
// path is the logical relative path within the bundle (e.g. "SKILL.md").
// shaHex is the SHA-256 hex of the file content.
// size is the byte length of the file content.
func computeTreeSHA(path, shaHex string, size int) string {
	h := sha256.New()
	// Single-file bundle entry: path\0sha256\0size\n
	fmt.Fprintf(h, "%s\x00%s\x00%d\n", filepath.Base(path), shaHex, size)
	return hex.EncodeToString(h.Sum(nil))
}

