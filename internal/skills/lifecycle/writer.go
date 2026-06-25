package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/caimlas/meept/internal/skills"
)

// shaIndexPath is the filename (relative to skillsDir) used to persist the
// content SHA-256 → skill name index used for duplicate detection.
const shaIndexPath = ".sha-index.json"

// Writer provides atomic skill file operations. It writes to a skills
// directory layout of <skillsDir>/<name>/SKILL.md.
//
// All write operations use the atomic-write pattern (write to .tmp file, then
// os.Rename) to prevent partial writes from corrupting skill files.
//
// The Writer holds a small mutex (shaMu) that protects ONLY the in-memory
// shaIndex map. Per CLAUDE.md mutex-scope rule: callers snapshot the map under
// the lock, release it, then perform any file I/O. No file operation is ever
// performed while holding shaMu.
//
// Concurrent writes to the same skill file are serialized by the filesystem
// rename(2) syscall (atomic on POSIX). Callers that need cross-operation
// atomicity (e.g. snapshot-then-write) should coordinate at a higher level.
type Writer struct {
	skillsDir string
	registry  *skills.Registry
	versioner *Versioner
	logger    *slog.Logger

	shaMu     sync.Mutex
	shaIndex  map[string]string // content_sha → skill_name
	shaLoaded bool              // tracks whether shaIndex has been loaded from disk
}

// NewWriter creates a new Writer rooted at skillsDir. The registry is used to
// keep the in-memory registry in sync with disk state when archiving or
// restoring skills. A nil registry is allowed (disk-only mode, for testing).
func NewWriter(skillsDir string, logger *slog.Logger) *Writer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Writer{
		skillsDir: skillsDir,
		logger:    logger,
		shaIndex:  make(map[string]string),
	}
}

// SetRegistry injects a skills.Registry so ArchiveSkill and RestoreSkill can
// keep the in-memory registry synchronized with disk state.
// Nil guard per CLAUDE.md setter rule.
func (w *Writer) SetRegistry(r *skills.Registry) {
	if r != nil {
		w.registry = r
	}
}

// SetVersioner injects a Versioner so WriteSkill captures a versioned snapshot
// of the existing content before overwriting. Without a Versioner, WriteSkill
// silently skips snapshotting (backwards-compatible with Phase 1 behavior).
// Nil guard per CLAUDE.md setter rule.
func (w *Writer) SetVersioner(v *Versioner) {
	if v != nil {
		w.versioner = v
	}
}

// skillPath returns the path to <skillsDir>/<name>/SKILL.md.
func (w *Writer) skillPath(name string) string {
	return filepath.Join(w.skillsDir, name, "SKILL.md")
}

// archivePath returns the path to <skillsDir>.archived/<name>/SKILL.md.
func (w *Writer) archivePath(name string) string {
	return filepath.Join(w.skillsDir+".archived", name, "SKILL.md")
}

// WriteSkill writes skill content to <skillsDir>/<name>/SKILL.md atomically.
// It creates the skill directory if it does not exist. The write is atomic:
// content is first written to a .tmp file, then renamed into place.
//
// If a Versioner is set (via SetVersioner) and the skill already exists on
// disk, a versioned snapshot of the current content is captured BEFORE the
// overwrite.
//
// Duplicate detection: the SHA-256 of the new content is checked against a
// persistent index (skillsDir/.sha-index.json). If the hash matches a different
// skill that already has identical content, WriteSkill returns
// ErrDuplicateContent and does not write.
func (w *Writer) WriteSkill(name, content string) error {
	if name == "" {
		return fmt.Errorf("writer: skill name is required")
	}

	targetPath := w.skillPath(name)

	// Snapshot existing content before overwriting, if a versioner is wired
	// and the skill already exists on disk.
	if w.versioner != nil {
		if _, err := os.Stat(targetPath); err == nil {
			if _, snapErr := w.versioner.Snapshot(name); snapErr != nil {
				w.logger.Warn("Version snapshot before write failed",
					"name", name, "error", snapErr)
				// Continue with write — best-effort snapshot.
			}
		}
	}

	// Duplicate detection: compute SHA of new content, check index.
	contentSHA := sha256HexBytes([]byte(content))
	if existingName, isDup := w.checkDuplicate(contentSHA, name); isDup {
		w.logger.Info("WriteSkill skipped: duplicate content",
			"name", name,
			"matches_existing", existingName,
			"content_sha", contentSHA,
		)
		return fmt.Errorf("%w: skill %q has identical content to %q",
			ErrDuplicateContent, name, existingName)
	}

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("writer: create skill dir: %w", err)
	}

	// Atomic write: write to .tmp then rename.
	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writer: write tmp file: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writer: rename tmp to target: %w", err)
	}

	// Update SHA index after successful write.
	if err := w.recordSHA(contentSHA, name); err != nil {
		w.logger.Warn("Failed to persist skill SHA index",
			"name", name, "error", err)
	}

	w.logger.Info("Skill written", "name", name, "path", targetPath)
	return nil
}

// checkDuplicate returns (existingSkillName, true) if the content SHA matches
// an entry in the SHA index belonging to a DIFFERENT skill. Same-name matches
// are not duplicates (it's a legitimate re-write of the same skill). The index
// is loaded lazily on first call.
func (w *Writer) checkDuplicate(contentSHA, skillName string) (string, bool) {
	if err := w.ensureSHALoaded(); err != nil {
		w.logger.Warn("SHA index load failed; duplicate detection skipped",
			"error", err)
		return "", false
	}

	w.shaMu.Lock()
	existing := w.shaIndex[contentSHA]
	w.shaMu.Unlock()

	if existing == "" || existing == skillName {
		return "", false
	}
	return existing, true
}

// recordSHA updates the in-memory SHA index and persists it to disk. Per
// CLAUDE.md mutex-scope rule: the map mutation happens under shaMu, the file
// write happens after release.
func (w *Writer) recordSHA(contentSHA, skillName string) error {
	if err := w.ensureSHALoaded(); err != nil {
		return err
	}

	// Snapshot the map under the lock, mutate, then release before I/O.
	w.shaMu.Lock()
	w.shaIndex[contentSHA] = skillName
	snapshot := make(map[string]string, len(w.shaIndex))
	for k, v := range w.shaIndex {
		snapshot[k] = v
	}
	w.shaMu.Unlock()

	// Persist outside the lock.
	indexDir := w.skillsDir
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return fmt.Errorf("writer: create sha index dir: %w", err)
	}
	indexBytes, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("writer: marshal sha index: %w", err)
	}
	indexPath := filepath.Join(w.skillsDir, shaIndexPath)
	tmpPath := indexPath + ".tmp"
	if err := os.WriteFile(tmpPath, indexBytes, 0o644); err != nil {
		return fmt.Errorf("writer: write sha index tmp: %w", err)
	}
	if err := os.Rename(tmpPath, indexPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writer: rename sha index: %w", err)
	}
	return nil
}

// removeSHABySkillName removes all SHA index entries whose value matches
// skillName. Called after archiving a skill so that identical content written
// later under a different name is not falsely flagged as a duplicate of the
// archived skill. Per CLAUDE.md mutex-scope rule: the map is mutated and
// snapshotted under shaMu, then the snapshot is persisted outside the lock.
func (w *Writer) removeSHABySkillName(skillName string) error {
	if err := w.ensureSHALoaded(); err != nil {
		return err
	}

	// Mutate under lock, snapshot for persistence, then release before I/O.
	w.shaMu.Lock()
	for sha, name := range w.shaIndex {
		if name == skillName {
			delete(w.shaIndex, sha)
		}
	}
	snapshot := make(map[string]string, len(w.shaIndex))
	for k, v := range w.shaIndex {
		snapshot[k] = v
	}
	w.shaMu.Unlock()

	// Persist outside the lock.
	indexDir := w.skillsDir
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return fmt.Errorf("writer: create sha index dir: %w", err)
	}
	indexBytes, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("writer: marshal sha index: %w", err)
	}
	indexPath := filepath.Join(w.skillsDir, shaIndexPath)
	tmpPath := indexPath + ".tmp"
	if err := os.WriteFile(tmpPath, indexBytes, 0o644); err != nil {
		return fmt.Errorf("writer: write sha index tmp: %w", err)
	}
	if err := os.Rename(tmpPath, indexPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writer: rename sha index: %w", err)
	}
	return nil
}

// ensureSHALoaded lazily reads the SHA index from disk on first use. The load
// is guarded by shaMu so concurrent callers only trigger one read.
func (w *Writer) ensureSHALoaded() error {
	w.shaMu.Lock()
	if w.shaLoaded {
		w.shaMu.Unlock()
		return nil
	}
	w.shaMu.Unlock()

	// Read file outside the lock.
	indexPath := filepath.Join(w.skillsDir, shaIndexPath)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			// First run — no index yet. Initialize empty and mark loaded.
			w.shaMu.Lock()
			if w.shaIndex == nil {
				w.shaIndex = make(map[string]string)
			}
			w.shaLoaded = true
			w.shaMu.Unlock()
			return nil
		}
		return fmt.Errorf("writer: read sha index: %w", err)
	}

	var loaded map[string]string
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("writer: parse sha index: %w", err)
	}

	w.shaMu.Lock()
	if w.shaIndex == nil {
		w.shaIndex = make(map[string]string)
	}
	for k, v := range loaded {
		w.shaIndex[k] = v
	}
	w.shaLoaded = true
	w.shaMu.Unlock()
	return nil
}

// sha256HexBytes computes SHA-256 of data and returns its lowercase hex.
func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ArchiveSkill moves a skill from <skillsDir>/<name>/ to
// <skillsDir>.archived/<name>/. The archive directory is created if needed.
// The skill is unregistered from the registry (if set).
//
// If a Versioner is wired, a snapshot of the current content is captured BEFORE
// the move (best-effort: on snapshot failure, logs a warning and continues).
// After the successful move, the skill's entries are pruned from the SHA index
// so that future writes with identical content are not falsely flagged as
// duplicates of an archived skill.
func (w *Writer) ArchiveSkill(name string) error {
	if name == "" {
		return fmt.Errorf("writer: skill name is required")
	}

	sourcePath := w.skillPath(name)
	sourceDir := filepath.Dir(sourcePath)

	// Check that the skill exists on disk.
	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("writer: skill not found on disk: %w", err)
	}

	// Snapshot existing content before archiving, if a versioner is wired.
	// Best-effort: on snapshot failure, log and continue with archive.
	if w.versioner != nil {
		if _, snapErr := w.versioner.Snapshot(name); snapErr != nil {
			w.logger.Warn("Version snapshot before archive failed",
				"name", name, "error", snapErr)
		}
	}

	archivePath := w.archivePath(name)
	archiveDir := filepath.Dir(archivePath)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return fmt.Errorf("writer: create archive dir: %w", err)
	}

	// Move the entire skill directory (may contain more than just SKILL.md).
	destDir := filepath.Join(w.skillsDir+".archived", name)
	if err := os.Rename(sourceDir, destDir); err != nil {
		// If rename fails (cross-device), fall back to copy + remove.
		if err := copyDir(sourceDir, destDir); err != nil {
			return fmt.Errorf("writer: archive skill (copy fallback): %w", err)
		}
		_ = os.RemoveAll(sourceDir)
	}

	// Prune SHA index entries for the archived skill so that identical content
	// written later under a different name is not falsely flagged as duplicate.
	if err := w.removeSHABySkillName(name); err != nil {
		w.logger.Warn("Failed to prune SHA index for archived skill",
			"name", name, "error", err)
	}

	// Unregister from the in-memory registry.
	if w.registry != nil {
		w.registry.Unregister(name)
	}

	w.logger.Info("Skill archived", "name", name, "archive_path", destDir)
	return nil
}

// RestoreSkill moves a skill from <skillsDir>.archived/<name>/ back to
// <skillsDir>/<name>/. After moving, the skill is re-parsed and registered
// in the registry (if set).
//
// If a Versioner is wired AND a skill with the same name currently exists at
// the primary path (which would be overwritten by the restore), a snapshot of
// that existing content is captured BEFORE the move (best-effort: on snapshot
// failure, logs a warning and continues).
func (w *Writer) RestoreSkill(name string) error {
	if name == "" {
		return fmt.Errorf("writer: skill name is required")
	}

	archivePath := w.archivePath(name)
	archiveDir := filepath.Dir(archivePath)

	// Check that the skill exists in the archive.
	if _, err := os.Stat(archivePath); err != nil {
		return fmt.Errorf("writer: skill not found in archive: %w", err)
	}

	sourcePath := w.skillPath(name)
	destDir := filepath.Dir(sourcePath)

	// If a skill already exists at the primary path, snapshot it before the
	// restore overwrites it (best-effort).
	if w.versioner != nil {
		if _, err := os.Stat(sourcePath); err == nil {
			if _, snapErr := w.versioner.Snapshot(name); snapErr != nil {
				w.logger.Warn("Version snapshot before restore failed",
					"name", name, "error", snapErr)
			}
		}
	}

	// Ensure the skills directory exists.
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("writer: create skills dir: %w", err)
	}

	// Move back.
	if err := os.Rename(archiveDir, destDir); err != nil {
		if err := copyDir(archiveDir, destDir); err != nil {
			return fmt.Errorf("writer: restore skill (copy fallback): %w", err)
		}
		_ = os.RemoveAll(archiveDir)
	}

	// Re-parse and register in the in-memory registry.
	if w.registry != nil {
		skill, err := skills.ParseSkillFile(sourcePath)
		if err != nil {
			w.logger.Warn("Failed to re-parse restored skill",
				"name", name,
				"path", sourcePath,
				"error", err,
			)
		} else {
			w.registry.Register(skill)
		}
	}

	w.logger.Info("Skill restored", "name", name, "path", destDir)
	return nil
}

// ReadSkill reads and returns the content of <skillsDir>/<name>/SKILL.md.
func (w *Writer) ReadSkill(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("writer: skill name is required")
	}

	content, err := os.ReadFile(w.skillPath(name))
	if err != nil {
		return "", fmt.Errorf("writer: read skill: %w", err)
	}
	return string(content), nil
}

// copyDir copies a directory tree from src to dst. Used as a fallback when
// os.Rename fails due to cross-device links.
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}
