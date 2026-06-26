package memory

import (
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// MigrateToDualDB migrates from legacy single-DB storage (sessions.db,
// memory.db) into the dual-store layout (local.db, sync-gossip.db).
//
// Steps:
//
//	1. Snapshot existing .db files into migration-backup/.
//	2. Rename sessions.db → local.db (it already has the session schema).
//	3. Merge memory.db tables into local.db where tables differ.
//	4. Create an empty sync-gossip.db with gossip schema.
//
// All operations are destructive after step 2 (files are moved); a backup
// directory is created first so nothing is lost.
func MigrateToDualDB(dataDir string, nodeID string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	if dataDir == "" {
		return fmt.Errorf("dual store migration: dataDir must not be empty")
	}

	sessionsPath := filepath.Join(dataDir, "sessions.db")
	memoryPath := filepath.Join(dataDir, "memory.db")
	localPath := filepath.Join(dataDir, localDBName)
	gossipPath := filepath.Join(dataDir, gossipDBName)
	backupDir := filepath.Join(dataDir, "migration-backup")

	// Step 1: Create backup of existing DB files.
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return fmt.Errorf("migration: create backup dir %s: %w", backupDir, err)
	}

	if _, err := os.Stat(sessionsPath); err == nil {
		if err := copyFileTo(sessionsPath, filepath.Join(backupDir, "sessions.db.pre-migration")); err != nil {
			return fmt.Errorf("migration: backup sessions.db: %w", err)
		}
		logger.Info("migrated: backed up sessions.db")
	}

	if _, err := os.Stat(memoryPath); err == nil {
		if err := copyFileTo(memoryPath, filepath.Join(backupDir, "memory.db.pre-migration")); err != nil {
			return fmt.Errorf("migration: backup memory.db: %w", err)
		}
		logger.Info("migrated: backed up memory.db")
	}

	// Step 2: Rename sessions.db -> local.db.
	if _, err := os.Stat(sessionsPath); err == nil {
		if err := os.Rename(sessionsPath, localPath); err != nil {
			return fmt.Errorf("migration: rename sessions.db -> local.db: %w", err)
		}
		logger.Info("migrated: renamed sessions.db to local.db")
	}

	// Step 3: Merge memory.db into local.db (ATTACH + SELECT for tables that
	// exist in memory.db but not in local.db).
	_, localExistsErr := os.Stat(localPath)
	_, memExistsErr := os.Stat(memoryPath)
	localExists := localExistsErr == nil
	memExists := memExistsErr == nil

	if localExists && memExists {
		// Both existed; try to merge common tables.
		logger.Info("migrated: both local.db and memory.db existed, attempting merge")
		if err := mergeMemoryDbIntoLocal(memoryPath, localPath, logger); err != nil {
			logger.Warn("migration: memory merge failed (data kept in backup)", "error", err)
		}
	} else if !localExists && memExists {
		// Only memory.db exists; rename it to local.db.
		if err := os.Rename(memoryPath, localPath); err != nil {
			return fmt.Errorf("migration: rename memory.db -> local.db: %w", err)
		}
		logger.Info("migrated: renamed memory.db to local.db (no sessions.db existed)")
	}

	// Step 4: Create empty gossip DB with schema.
	gossipDB, err := sql.Open("sqlite", gossipPath)
	if err != nil {
		return fmt.Errorf("migration: create gossip DB: %w", err)
	}

	schema, err := gossipSchema.ReadFile("schema_gossip.sql")
	if err != nil {
		gossipDB.Close()
		return fmt.Errorf("migration: read gossip schema: %w", err)
	}
	if _, err := gossipDB.Exec(string(schema)); err != nil {
		gossipDB.Close()
		return fmt.Errorf("migration: gossip schema: %w", err)
	}
	gossipDB.Close()

	logger.Info("migration: dual-DB migration complete")
	return nil
}

// copyFileTo copies src to dst. Returns nil on success.
func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// mergeMemoryDbIntoLocal ATTACHes memory.db and copies tables from it into
// local.db. Best-effort: errors per-table are logged but don't abort.
func mergeMemoryDbIntoLocal(memoryPath, localPath string, logger *slog.Logger) error {
	localDB, err := sql.Open("sqlite", localPath)
	if err != nil {
		return fmt.Errorf("open local.db for merge: %w", err)
	}
	defer localDB.Close()

	memDB, err := sql.Open("sqlite", memoryPath)
	if err != nil {
		return fmt.Errorf("open memory.db for merge: %w", err)
	}
	defer memDB.Close()

	// Get table list from memory.db.
	rows, err := memDB.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return fmt.Errorf("list memory.db tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr == nil {
			tables = append(tables, name)
		}
	}
	rows.Close()

	safeMemPath := memoryPath // safe because it came from our own dataDir

	for _, tbl := range tables {
		if tbl == "sqlite_sequence" || tbl == "sqlite_stat1" {
			continue
		}
		// Check if table already exists in local.db.
		var exists int
		localDB.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`,
			tbl).Scan(&exists)
		if exists > 0 {
			logger.Debug("migration: skip merge, table already exists", "table", tbl)
			continue
		}
		// ATTACH memory.db, CREATE TABLE AS SELECT, DETACH.
		stmt := fmt.Sprintf(`ATTACH '%s' AS src; CREATE TABLE "%s" AS SELECT * FROM src."%s"; DETACH src;`,
			safeMemPath, tblName(tbl), tblName(tbl))
		if _, err := localDB.Exec(stmt); err != nil {
			logger.Warn("migration: failed to copy table", "table", tbl, "error", err)
		} else {
			logger.Info("migration: merged table", "table", tbl)
		}
	}
	return nil
}

// tblName sanitizes a table name to alphanumeric + underscore only.
func tblName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		if ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z') || b == '_' || ('0' <= b && b <= '9') {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		return "table_" + s
	}
	return string(out)
}
