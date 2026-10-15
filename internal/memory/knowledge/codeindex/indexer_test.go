package codeindex

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/memory/knowledge"
)

// newTestStore opens a knowledge store in a fresh temp dir.
func newTestStore(t *testing.T) *knowledge.Store {
	t.Helper()
	s, err := knowledge.New(t.TempDir(), 0, slog.Default())
	if err != nil {
		t.Fatalf("knowledge.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// fixtureDir resolves the checked-in fixture repo.
func fixtureDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "fixture"))
	if err != nil {
		t.Fatalf("abs fixture dir: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("fixture dir missing: %s", dir)
	}
	return dir
}

// TestIndexFixture exercises the full happy path: index the fixture, assert
// symbol count, the known call edges, ImpactedBy, page shape.
func TestIndexFixture(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	rep, err := IndexGoDirs(ctx, store, []string{fixtureDir(t)}, slog.Default())
	if err != nil {
		t.Fatalf("IndexGoDirs: %v", err)
	}

	// Indexed exactly the 2 real files; skipped _test.go. vendor/ and
	// testdata/ are pruned at the dir level so they never count.
	if rep.FilesIndexed != 2 {
		t.Errorf("FilesIndexed = %d, want 2 (errors: %v)", rep.FilesIndexed, rep.Errors)
	}
	if len(rep.Errors) != 0 {
		t.Errorf("unexpected errors: %v", rep.Errors)
	}

	// Symbol table (package-qualified):
	//   pkgone: MaxDepth, Alpha, Server, Server.Start, defaultName
	//   pkgtwo: Helper, Counter, Counter.Next
	wantSymbols := []string{
		"pkgone.MaxDepth", "pkgone.Alpha", "pkgone.Server", "pkgone.Server.Start", "pkgone.defaultName",
		"pkgtwo.Helper", "pkgtwo.Counter", "pkgtwo.Counter.Next",
	}
	if rep.SymbolsFound != len(wantSymbols) {
		t.Errorf("SymbolsFound = %d, want %d", rep.SymbolsFound, len(wantSymbols))
	}
	probeRep := IndexReport{}
	_, symTable, _, err := parseTrees(ctx, []string{fixtureDir(t)}, &probeRep)
	if err != nil {
		t.Fatalf("parseTrees: %v", err)
	}
	gotNames := map[string]bool{}
	for name := range symTable {
		gotNames[name] = true
	}
	for _, s := range wantSymbols {
		if !gotNames[s] {
			t.Errorf("symbol %q missing from symbol table (got %d symbols)", s, len(symTable))
		}
	}

	// Known cross-package edge: pkgone.Alpha -> pkgtwo.Helper.
	callers, err := ImpactedBy(ctx, store, "pkgtwo.Helper")
	if err != nil {
		t.Fatalf("ImpactedBy(Helper): %v", err)
	}
	if len(callers) != 1 || callers[0] != "pkgone.Alpha" {
		t.Errorf("ImpactedBy(pkgtwo.Helper) = %v, want [pkgone.Alpha]", callers)
	}

	// Intra-package method edge: pkgone.Server.Start -> pkgone.Alpha.
	callers, err = ImpactedBy(ctx, store, "Alpha")
	if err != nil {
		t.Fatalf("ImpactedBy(Alpha): %v", err)
	}
	if len(callers) != 1 || callers[0] != "pkgone.Server.Start" {
		t.Errorf("ImpactedBy(Alpha) = %v, want [pkgone.Server.Start]", callers)
	}

	// Method edge through a pointer receiver: pkgone.Alpha -> Counter.Next.
	callers, err = ImpactedBy(ctx, store, "pkgtwo.Counter.Next")
	if err != nil {
		t.Fatalf("ImpactedBy(Counter.Next): %v", err)
	}
	if len(callers) != 1 || callers[0] != "pkgone.Alpha" {
		t.Errorf("ImpactedBy(pkgtwo.Counter.Next) = %v, want [pkgone.Alpha]", callers)
	}

	// Skip rules: nothing from vendor/testdata/_test.go is indexed.
	for _, banned := range []string{"pkgone.Skipped", "vendored.Hidden", "testdatainner.Hidden"} {
		if gotNames[banned] {
			t.Errorf("symbol %q from a skipped location leaked into the index", banned)
		}
	}
}

// TestIndexUpsertIdempotent re-indexes an unchanged fixture: no new
// ingests, same counts.
func TestIndexUpsertIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first, err := IndexGoDirs(ctx, store, []string{fixtureDir(t)}, slog.Default())
	if err != nil {
		t.Fatalf("first IndexGoDirs: %v", err)
	}

	pagesBefore, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}

	second, err := IndexGoDirs(ctx, store, []string{fixtureDir(t)}, slog.Default())
	if err != nil {
		t.Fatalf("second IndexGoDirs: %v", err)
	}

	if second.FilesIndexed != 0 {
		t.Errorf("second pass FilesIndexed = %d, want 0 (all unchanged)", second.FilesIndexed)
	}
	if second.FilesSkipped != first.FilesIndexed {
		t.Errorf("second pass FilesSkipped = %d, want %d", second.FilesSkipped, first.FilesIndexed)
	}
	if second.SymbolsFound != first.SymbolsFound {
		t.Errorf("symbol count drifted: %d -> %d", first.SymbolsFound, second.SymbolsFound)
	}

	pagesAfter, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count after: %v", err)
	}
	if pagesAfter != pagesBefore {
		t.Errorf("page count drifted: %d -> %d", pagesBefore, pagesAfter)
	}

	// Callers survive the re-index (edges rewritten, not duplicated).
	callers, err := ImpactedBy(ctx, store, "pkgtwo.Helper")
	if err != nil {
		t.Fatalf("ImpactedBy after re-index: %v", err)
	}
	if len(callers) != 1 || callers[0] != "pkgone.Alpha" {
		t.Errorf("ImpactedBy(pkgtwo.Helper) after re-index = %v, want [pkgone.Alpha]", callers)
	}
}

// TestIndexChangedFileReingests edits a fixture file (copy to temp) and
// asserts the changed file is re-ingested and stale edges updated.
func TestIndexChangedFileReingests(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store := newTestStore(t)

	// Copy the fixture into tmp so the original stays untouched.
	fixture := fixtureDir(t)
	if err := filepath.WalkDir(fixture, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixture, path)
		if rel == "." {
			return nil
		}
		target := filepath.Join(tmp, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	if _, err := IndexGoDirs(ctx, store, []string{tmp}, slog.Default()); err != nil {
		t.Fatalf("first index: %v", err)
	}

	// Change helper.go: rename Helper → RenamedHelper, and alpha.go's
	// call site with it (a real refactor moves both).
	helperPath := filepath.Join(tmp, "pkgtwo", "helper.go")
	changed := strings.ReplaceAll(
		mustRead(t, helperPath),
		"func Helper(n int) int {",
		"func RenamedHelper(n int) int {",
	)
	if changed == mustRead(t, helperPath) {
		t.Fatal("fixture edit did not apply")
	}
	if err := os.WriteFile(helperPath, []byte(changed), 0o644); err != nil {
		t.Fatalf("write changed helper: %v", err)
	}
	alphaPath := filepath.Join(tmp, "pkgone", "alpha.go")
	alphaChanged := strings.ReplaceAll(
		mustRead(t, alphaPath),
		"pkgtwo.Helper(depth)",
		"pkgtwo.RenamedHelper(depth)",
	)
	if alphaChanged == mustRead(t, alphaPath) {
		t.Fatal("alpha call-site edit did not apply")
	}
	if err := os.WriteFile(alphaPath, []byte(alphaChanged), 0o644); err != nil {
		t.Fatalf("write changed alpha: %v", err)
	}

	rep, err := IndexGoDirs(ctx, store, []string{tmp}, slog.Default())
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	// Only helper.go's PAGE is re-ingested: alpha.go's rendered body
	// (signatures) is unchanged by a body-internal call-site rename.
	// alpha.go's call EDGES are still rewritten (ReplaceCallEdges runs
	// for every parsed file) — asserted via ImpactedBy below.
	if rep.FilesIndexed != 1 {
		t.Errorf("changed-pass FilesIndexed = %d, want 1", rep.FilesIndexed)
	}

	callers, err := ImpactedBy(ctx, store, "pkgtwo.Helper")
	if err != nil {
		t.Fatalf("ImpactedBy(Helper) after rename: %v", err)
	}
	if len(callers) != 0 {
		t.Errorf("stale callers of renamed Helper = %v, want empty", callers)
	}
	callers, err = ImpactedBy(ctx, store, "pkgtwo.RenamedHelper")
	if err != nil {
		t.Fatalf("ImpactedBy(RenamedHelper): %v", err)
	}
	if len(callers) != 1 || callers[0] != "pkgone.Alpha" {
		t.Errorf("ImpactedBy(RenamedHelper) = %v, want [pkgone.Alpha]", callers)
	}
}

// TestIndexEmptyDirsIsNoop covers the code_dirs-disabled default.
func TestIndexEmptyDirsIsNoop(t *testing.T) {
	store := newTestStore(t)
	rep, err := IndexGoDirs(context.Background(), store, nil, slog.Default())
	if err != nil {
		t.Fatalf("IndexGoDirs(nil dirs): %v", err)
	}
	if rep.FilesIndexed != 0 || rep.FilesSkipped != 0 || rep.SymbolsFound != 0 || rep.EdgesFound != 0 || len(rep.Errors) != 0 {
		t.Errorf("expected zero report, got %+v", rep)
	}
	if _, err := IndexGoDirs(context.Background(), store, []string{filepath.Join("does", "not", "exist")}, slog.Default()); err == nil {
		t.Error("expected error for missing dir")
	}
}

// TestIndexDropsDeletedFiles pins the bughunt 2026-10-14 stale-file fix: a
// file deleted from an indexed dir must lose its CODE page AND its call
// edges on the next pass, so knowledge_impact stops naming callers that no
// longer exist. The old pass only ingested — deleted files persisted
// forever.
func TestIndexDropsDeletedFiles(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tmp := t.TempDir()
	if err := filepath.WalkDir(fixtureDir(t), func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(fixtureDir(t), path)
		if rel == "." {
			return nil
		}
		target := filepath.Join(tmp, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	if _, err := IndexGoDirs(ctx, store, []string{tmp}, slog.Default()); err != nil {
		t.Fatalf("first index: %v", err)
	}
	if callers, err := ImpactedBy(ctx, store, "pkgtwo.Helper"); err != nil || len(callers) != 1 {
		t.Fatalf("precondition: ImpactedBy(Helper) = %v (%v), want [pkgone.Alpha]", callers, err)
	}

	// Delete alpha.go (the caller) from the tree and re-index.
	if err := os.Remove(filepath.Join(tmp, "pkgone", "alpha.go")); err != nil {
		t.Fatalf("remove alpha.go: %v", err)
	}
	rep, err := IndexGoDirs(ctx, store, []string{tmp}, slog.Default())
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if rep.FilesRemoved != 1 {
		t.Errorf("FilesRemoved = %d, want 1 (errors: %v)", rep.FilesRemoved, rep.Errors)
	}
	// The deleted file's page is gone…
	if sources, err := store.ListSources(ctx, "code:pkgone/alpha.go"); err != nil || len(sources) != 0 {
		t.Errorf("alpha.go page survived deletion: sources=%v err=%v", sources, err)
	}
	// …and its edges no longer name it as a caller of Helper.
	callers, err := ImpactedBy(ctx, store, "pkgtwo.Helper")
	if err != nil {
		t.Fatalf("ImpactedBy(Helper) after delete: %v", err)
	}
	if len(callers) != 0 {
		t.Errorf("callers from a deleted file = %v, want empty", callers)
	}
}

// TestFromLineIsReal pins the bughunt 2026-10-14 line-number fix: call
// edges must carry the call site's actual line, not 0 (a fresh FileSet
// cannot resolve positions parsed onto the pass-1 FileSet).
func TestFromLineIsReal(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := IndexGoDirs(ctx, store, []string{fixtureDir(t)}, slog.Default()); err != nil {
		t.Fatalf("IndexGoDirs: %v", err)
	}
	callers, err := knowledgeCallersWithLines(t, store, "pkgtwo.Helper")
	if err != nil {
		t.Fatalf("callers-with-lines: %v", err)
	}
	if len(callers) != 1 {
		t.Fatalf("callers = %v, want 1", callers)
	}
	if callers[0].FromFile != "pkgone/alpha.go" {
		t.Errorf("FromFile = %q, want pkgone/alpha.go", callers[0].FromFile)
	}
	if callers[0].FromLine <= 0 {
		t.Errorf("FromLine = %d, want the real call-site line (>0)", callers[0].FromLine)
	}
}

// TestPageShape asserts one CODE page per file with the contracted
// title/source/body shape.
func TestPageShape(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := IndexGoDirs(ctx, store, []string{fixtureDir(t)}, slog.Default()); err != nil {
		t.Fatalf("IndexGoDirs: %v", err)
	}

	id := knowledge.PageID("code:pkgtwo:helper.go", "code:pkgtwo/helper.go")
	page, err := store.Read(ctx, id)
	if err != nil {
		t.Fatalf("Read helper.go page: %v", err)
	}
	if page.Title != "code:pkgtwo:helper.go" {
		t.Errorf("Title = %q", page.Title)
	}
	if page.Source != "code:pkgtwo/helper.go" {
		t.Errorf("Source = %q", page.Source)
	}
	for _, want := range []string{"package pkgtwo", "func Helper(n int) int", "type Counter struct", "func (c *Counter) Next() int"} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("body missing %q;\nbody:\n%s", want, page.Body)
		}
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// knowledgeCallersWithLines reads the raw call-edge rows naming callers of
// symbol, with their line numbers (Callers returns names only). Uses the
// store's exported DB access seam via CallersEdgesTo when present; falls
// back to a direct query through the store's exported test hook.
func knowledgeCallersWithLines(t *testing.T, store *knowledge.Store, symbol string) ([]knowledge.CallEdge, error) {
	t.Helper()
	return store.CallersEdges(t.Context(), symbol)
}
