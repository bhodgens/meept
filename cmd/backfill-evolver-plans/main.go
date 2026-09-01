// Backfills the evolver plan sink store with rows for the historical plan
// files migrated to ~/.meept/plans/evolver/ (they predate the sink store, so
// they have files but no rows — invisible to plan list/show/approve even
// after the RPC sink fallback landed).
//
// Usage:
//
//	go run ./cmd/backfill-evolver-plans -apply
//	go run ./cmd/backfill-evolver-plans -db <path> -dir <path> -apply
//
// Without -apply it is a dry run. Each file becomes a pending_approval row
// (FilePath points at the real file; the approval actuator reads provenance
// from the file's Meta block, so approval works once the row exists). Files
// whose id already exists in the store are skipped.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/plan"
)

func main() {
	dbPath := flag.String("db", filepath.Join(os.Getenv("HOME"), ".meept", "plans-evolver.db"), "sink store DB path")
	dir := flag.String("dir", filepath.Join(os.Getenv("HOME"), ".meept", "plans", "evolver"), "plan file directory")
	apply := flag.Bool("apply", false, "actually insert rows (default: dry run)")
	flag.Parse()

	entries, err := os.ReadDir(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read dir: %v\n", err)
		os.Exit(1)
	}

	store, err := plan.NewSQLiteStore(*dbPath, slog.Default())
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := store.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close store: %v\n", err)
		}
	}()

	inserted, skipped, failed := 0, 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(*dir, e.Name())
		meta := parseBackfillMeta(path)
		if meta.id == "" {
			fmt.Printf("  SKIP %s: no plan_id in Meta\n", e.Name())
			skipped++
			continue
		}
		p := &plan.Plan{
			ID:        meta.id,
			Title:     meta.title,
			FilePath:  path,
			State:     plan.StatePendingApproval,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		if _, getErr := store.GetPlan(context.Background(), meta.id); getErr == nil {
			fmt.Printf("  SKIP %s -> %s (already in store)\n", e.Name(), meta.id)
			skipped++
			continue
		}
		if !*apply {
			fmt.Printf("  DRY  %s -> %s (%s)\n", e.Name(), meta.id, meta.title)
			continue
		}
		if err := store.CreatePlan(context.Background(), p); err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				fmt.Printf("  SKIP %s -> %s (already in store)\n", e.Name(), meta.id)
				skipped++
				continue
			}
			fmt.Printf("  ERR  %s: %v\n", e.Name(), err)
			failed++
			continue
		}
		fmt.Printf("  OK   %s -> %s\n", e.Name(), meta.id)
		inserted++
	}
	fmt.Printf("done: inserted=%d skipped=%d failed=%d\n", inserted, skipped, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// parseBackfillMeta extracts plan_id, title, and status from a plan markdown
// file (the writer's ## Meta format).
func parseBackfillMeta(path string) backfillMeta {
	data, err := os.ReadFile(path)
	if err != nil {
		return backfillMeta{}
	}
	var m backfillMeta
	inMeta := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "## "):
			if inMeta && m.title != "" {
				return m
			}
			inMeta = line == "## Meta"
		case strings.HasPrefix(line, "# Plan: ") && m.title == "":
			m.title = strings.TrimPrefix(line, "# Plan: ")
		case inMeta:
			if v, ok := strings.CutPrefix(line, "- plan_id: "); ok {
				m.id = strings.TrimSpace(v)
			}
			if v, ok := strings.CutPrefix(line, "- status: "); ok {
				m.status = strings.TrimSpace(v)
			}
		}
	}
	return m
}

type backfillMeta struct {
	id     string
	title  string
	status string
}
