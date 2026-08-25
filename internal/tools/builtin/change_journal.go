package builtin

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/pkg/id"
	_ "modernc.org/sqlite" //nolint:revive // sqlite driver registration
)

// DefaultJournalDBPath is the fallback location for the change journal when
// JournalConfig.DBPath is empty: <state dir>/changes.db.
const DefaultJournalDBPath = "~/.meept/changes.db"

// defaultMaxEntryBytes caps how large a pre-image the journal will store.
// Entries whose pre-image exceeds this are still journaled (so drift history
// is preserved) but without PreImage bytes — revert refuses them.
const defaultMaxEntryBytes = 1 << 20 // 1 MiB

// ErrEntryNotFound is returned by Revert when no journal entry matches id.
var ErrEntryNotFound = errors.New("journal entry not found")

// JournalEntry is one applied (accepted) file change with enough state to
// revert it later. Stored in the change_journal SQLite table.
type JournalEntry struct {
	ID        string
	SessionID string
	FilePath  string
	// PreImage is the file content BEFORE the change was applied.
	// nil means the pre-image was too large to journal (> MaxEntryBytes);
	// such entries can be listed but never reverted.
	PreImage []byte
	// PostSHA is the lowercase hex SHA256 of the applied content, used to
	// detect drift between apply time and revert time.
	PostSHA   string
	AppliedAt time.Time
	ChangeIDs []string
}

// JournalConfig configures a Journal.
type JournalConfig struct {
	DBPath        string `json:"db_path"         toml:"db_path"`        // default ~/.meept/changes.db
	MaxEntryBytes int64  `json:"max_entry_bytes" toml:"max_entry_bytes"` // default 1MiB
}

// Journal persists applied file changes so they can be reverted via
// `meept changes revert`. It follows the internal/llm.RoutingLogger pattern:
// WAL journal mode, MaxOpenConns(1) to avoid SQLITE_BUSY under concurrent
// writes, schema initialization at construction.
//
// Concurrency: all database access happens through *sql.DB's internal
// connection pool serialized by SetMaxOpenConns(1); Journal holds no mutex
// of its own (mutexio-safe by construction).
type Journal struct {
	db            *sql.DB
	logger        *slog.Logger
	maxEntryBytes int64
}

// NewJournal opens (creating if necessary) the change journal database at
// cfg.DBPath and migrates its schema. A leading `~` in DBPath expands to the
// user's home directory; an empty DBPath falls back to ~/.meept/changes.db.
// A non-positive MaxEntryBytes falls back to the 1 MiB default. If logger is
// nil, slog.Default() is used.
func NewJournal(cfg JournalConfig, logger *slog.Logger) (*Journal, error) {
	if logger == nil {
		logger = slog.Default()
	}

	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = DefaultJournalDBPath
	}
	if len(dbPath) > 0 && dbPath[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("change journal: failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(home, dbPath[1:])
	}
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("change journal: failed to create database directory: %w", err)
		}
	}

	maxBytes := cfg.MaxEntryBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxEntryBytes
	}

	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("change journal: failed to open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	j := &Journal{
		db:            db,
		logger:        logger.With("component", "change-journal"),
		maxEntryBytes: maxBytes,
	}

	if err := j.migrate(); err != nil {
		db.Close() // best-effort close on teardown; primary error already returned
		return nil, fmt.Errorf("change journal: migration failed: %w", err)
	}

	return j, nil
}

func (j *Journal) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS change_journal (
	id          TEXT PRIMARY KEY,
	session_id  TEXT NOT NULL,
	file_path   TEXT NOT NULL,
	pre_image   BLOB,
	post_sha    TEXT NOT NULL,
	applied_at  TEXT NOT NULL,
	change_ids  TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_change_journal_session ON change_journal(session_id, applied_at DESC);
`
	_, err := j.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("change journal: create schema: %w", err)
	}
	return nil
}

// Record stores an applied-change entry. ID and AppliedAt are filled in if
// zero. When entry.PreImage exceeds maxEntryBytes it is dropped (stored as
// NULL) before insert — the row remains for history/listing but revert will
// refuse it. ChangeIDs is serialized as JSON.
func (j *Journal) Record(entry *JournalEntry) error {
	if entry == nil {
		return fmt.Errorf("change journal: record requires an entry")
	}
	if entry.FilePath == "" {
		return fmt.Errorf("change journal: record requires file_path")
	}
	if entry.ID == "" {
		entry.ID = id.Generate("change-")
	}
	if entry.AppliedAt.IsZero() {
		entry.AppliedAt = time.Now().UTC()
	}

	var preImage any // nil -> SQL NULL
	if int64(len(entry.PreImage)) > j.maxEntryBytes {
		j.logger.Warn("change journal: pre-image exceeds size cap; storing entry without pre-image (not revertible)",
			"entry_id", entry.ID, "path", entry.FilePath, "pre_image_bytes", len(entry.PreImage), "cap_bytes", j.maxEntryBytes)
	} else if len(entry.PreImage) > 0 {
		preImage = entry.PreImage
	}

	changeIDs := entry.ChangeIDs
	if changeIDs == nil {
		changeIDs = []string{}
	}
	changeIDsJSON, err := marshalChangeIDs(changeIDs)
	if err != nil {
		return fmt.Errorf("change journal: encode change_ids: %w", err)
	}

	_, err = j.db.Exec(
		`INSERT INTO change_journal (id, session_id, file_path, pre_image, post_sha, applied_at, change_ids)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.SessionID, entry.FilePath, preImage, entry.PostSHA,
		entry.AppliedAt.UTC().Format(time.RFC3339Nano), changeIDsJSON,
	)
	if err != nil {
		return fmt.Errorf("change journal: insert entry: %w", err)
	}
	return nil
}

// List returns up to limit entries for sessionID, newest first. PreImage
// bytes are deliberately NOT populated (they may be megabytes); use Revert
// to restore content. A non-positive limit defaults to 100. An empty
// sessionID lists across all sessions.
func (j *Journal) List(sessionID string, limit int) ([]JournalEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT id, session_id, file_path, post_sha, applied_at, change_ids FROM change_journal`
	args := []any{}
	if sessionID != "" {
		query += ` WHERE session_id = ?`
		args = append(args, sessionID)
	}
	query += ` ORDER BY applied_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := j.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("change journal: list entries: %w", err)
	}
	defer rows.Close()

	entries := make([]JournalEntry, 0, limit)
	for rows.Next() {
		var (
			e           JournalEntry
			appliedAtS  string
			changeIDsJS string
		)
		if err := rows.Scan(&e.ID, &e.SessionID, &e.FilePath, &e.PostSHA, &appliedAtS, &changeIDsJS); err != nil {
			return nil, fmt.Errorf("change journal: scan entry: %w", err)
		}
		if t, perr := time.Parse(time.RFC3339Nano, appliedAtS); perr == nil {
			e.AppliedAt = t
		}
		ids, perr := unmarshalChangeIDs(changeIDsJS)
		if perr == nil {
			e.ChangeIDs = ids
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("change journal: iterate entries: %w", err)
	}
	return entries, nil
}

// Revert restores the file recorded in entry id to its pre-image content.
//
// Three-way checksum semantics:
//   - entry has no journaled pre-image (size cap)      -> error
//   - current == sha(PreImage)                         -> already reverted; success, no rewrite
//   - current != PostSHA AND current != sha(PreImage)  -> drift refusal ("changed since apply")
//   - current == PostSHA                               -> clean revert; atomic write of PreImage
//
// fence (optional) is checked BEFORE any write; a refusal leaves the file
// untouched. Returns the restored path.
func (j *Journal) Revert(id_ string, fence FenceChecker) (string, error) {
	if id_ == "" {
		return "", fmt.Errorf("change journal: revert requires an entry id")
	}

	var (
		filePath  string
		preImage  []byte
		postSHA   string
		appliedAt string
	)
	err := j.db.QueryRow(
		`SELECT file_path, pre_image, post_sha, applied_at FROM change_journal WHERE id = ?`,
		id_,
	).Scan(&filePath, &preImage, &postSHA, &appliedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", fmt.Errorf("change journal: %w: %s", ErrEntryNotFound, id_)
	case err != nil:
		return "", fmt.Errorf("change journal: load entry: %w", err)
	}

	if len(preImage) == 0 {
		return filePath, fmt.Errorf("change journal: %s: pre-image not journaled (size cap)", filePath)
	}

	currentData, err := os.ReadFile(filePath) //nolint:gosec // path comes from our own journal rows
	if err != nil {
		return filePath, fmt.Errorf("change journal: read current file: %w", err)
	}

	currentSHA := sha256.Sum256(currentData)
	currentHex := hex.EncodeToString(currentSHA[:])
	preSHA := sha256.Sum256(preImage)
	preHex := hex.EncodeToString(preSHA[:])

	switch {
	case currentHex == preHex:
		// Already reverted (or was never actually changed after apply).
		j.logger.Info("change journal: file already at pre-image state; skipping rewrite",
			"entry_id", id_, "path", filePath)
		return filePath, nil

	case currentHex == postSHA:
		// Clean revert path below.

	default:
		return filePath, fmt.Errorf(
			"change journal: %s: file changed since apply (applied %s… != current %s…) — refusing to overwrite",
			filePath, shortHash(postSHA), shortHash(currentHex))
	}

	if fence != nil {
		if err := fence.CheckPath(filePath, "write"); err != nil {
			return filePath, fmt.Errorf("change journal: fence refused revert of %s: %w", filePath, err)
		}
	}

	if err := writeFileAtomic(filePath, preImage); err != nil {
		return filePath, fmt.Errorf("change journal: write pre-image: %w", err)
	}

	j.logger.Info("change journal: reverted",
		"entry_id", id_, "path", filePath, "applied_at", appliedAt)
	return filePath, nil
}

// writeFileAtomic writes data to path via a temp file in the same directory
// followed by rename, so readers never observe partial content.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".meept-revert-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close() // cleanup path: original write error takes precedence
		os.Remove(tmpName) // temp-file cleanup is best-effort
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close() // cleanup path: original write error takes precedence
		os.Remove(tmpName) // temp-file cleanup is best-effort
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName) // temp-file cleanup is best-effort
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName) // temp-file cleanup is best-effort
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName) // temp-file cleanup is best-effort
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}

// Close releases the underlying database connection. Safe on a nil receiver.
func (j *Journal) Close() error {
	if j == nil || j.db == nil {
		return nil
	}
	return j.db.Close()
}

// PreImageSize returns the byte size of the journaled pre-image for entry id
// (0 when absent — either no such row or the size cap dropped it). Lets the
// CLI show size/revertable columns without transferring pre-image bytes.
func (j *Journal) PreImageSize(entryID string) (int64, error) {
	var stored sql.NullInt64
	err := j.db.QueryRow(`SELECT LENGTH(pre_image) FROM change_journal WHERE id = ?`, entryID).Scan(&stored)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("change journal: pre-image size: %w", err)
	}
	if !stored.Valid {
		return 0, nil
	}
	return stored.Int64, nil
}

// marshalChangeIDs serializes the change_ids list as JSON via encoding/json.
func marshalChangeIDs(ids []string) (string, error) {
	b, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalChangeIDs parses a JSON array of strings back into a slice.
func unmarshalChangeIDs(s string) ([]string, error) {
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("change journal: parse change_ids: %w", err)
	}
	return out, nil
}
