package knowledge

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// dirSourcePrefix marks pages ingested from the watched directory. The
// source of such a page is "dir:<relpath>" (relative to the sync root,
// slash-separated), making the page identity stable across syncs and
// disjoint from tool/agent ingests.
const dirSourcePrefix = "dir:"

// SyncReport summarizes one SyncDir pass.
type SyncReport struct {
	// Ingested counts pages written because they were new or changed
	// (content hash differs from the stored body).
	Ingested int
	// Skipped counts unchanged files (same content hash already stored).
	Skipped int
	// Errors lists per-file failures; SyncDir never aborts the whole pass
	// on a single bad file.
	Errors []string
}

// SyncDir ingests new or changed *.md files under dir into the store
// (watched-dir ingestion, tree 04 leaf L2). Page title is the relative
// path without the .md suffix; source is "dir:<relpath>", so the page ID
// is deterministic and re-syncs upsert in place. Unchanged files (stored
// body byte-identical to the file content) are skipped. Missing/unreadable
// files are reported in SyncReport.Errors, never fatal. An absent dir is
// an error (misconfiguration), not a silent no-op.
//
// SyncDir holds no locks across file I/O: each file's read happens before
// the store's own transactional write. It is intended to be called from
// the periodic consolidation wiring (leaf T04-L4).
func SyncDir(ctx context.Context, s *Store, dir string) (SyncReport, error) {
	if s == nil {
		return SyncReport{}, errors.New("syncdir: nil store")
	}
	if strings.TrimSpace(dir) == "" {
		return SyncReport{}, errors.New("syncdir: empty dir")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return SyncReport{}, fmt.Errorf("syncdir: stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return SyncReport{}, fmt.Errorf("syncdir: %s is not a directory", dir)
	}

	var rep SyncReport
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("walk %s: %v", path, walkErr))
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("relpath %s: %v", path, relErr))
			return nil
		}
		rel = filepath.ToSlash(rel)

		//nolint:gosec // path is rooted at the user-configured ingest dir
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("read %s: %v", rel, readErr))
			return nil
		}

		title := strings.TrimSuffix(rel, ".md")
		source := dirSourcePrefix + rel
		id := PageID(title, source)

		// Content-hash diff: skip the write when the stored body is
		// byte-identical to the file content. Read's link healing does
		// not affect Body, so the comparison is stable.
		stored, readPageErr := s.Read(ctx, id)
		switch {
		case readPageErr == nil:
			sumNew := sha256.Sum256(content)
			sumOld := sha256.Sum256([]byte(stored.Body))
			if sumNew == sumOld {
				rep.Skipped++
				return nil
			}
		case errors.Is(readPageErr, ErrNotFound):
			// New page: fall through to ingest.
		default:
			rep.Errors = append(rep.Errors, fmt.Sprintf("lookup %s: %v", id, readPageErr))
			return nil
		}

		if _, ingErr := s.Ingest(ctx, Page{
			Title:  title,
			Body:   string(content),
			Source: source,
		}); ingErr != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("ingest %s: %v", rel, ingErr))
			return nil
		}
		rep.Ingested++
		return nil
	})
	if walkErr != nil {
		return rep, fmt.Errorf("syncdir: walk %s: %w", dir, walkErr)
	}
	return rep, nil
}
