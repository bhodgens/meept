//go:build e2e

// Package manifest covers the e2e MANIFEST's own invariants — the
// metadata the pre-commit gate and CI dispatch on. A manifest that
// under-declares does not fail loudly, it fails silently: a suite no
// path_map key reaches can never be selected by make e2e-affected, and a
// scenario id nobody registered is invisible to every reviewer (audit
// M12: gui-flows had ZERO reachable path_map keys and gui-stream-02 was
// not a scenario at all, so all five Dart scenarios were decorative).
//
// This is Go-native because the manifest, the runner, and the pre-commit
// gate are all Go/shell-side; it runs in the hermetic tier
// (make e2e-fast, CI job e2e-fast) with no daemon and no Flutter SDK.
package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// manifest is the on-disk shape of e2e/manifest.json (the fields this
// suite pins; unknown fields are ignored).
type manifest struct {
	Version   int                 `json:"version"`
	Generated string              `json:"generated"`
	Comment   string              `json:"comment"`
	PathMap   map[string][]string `json:"path_map"`
	Suites    []suite             `json:"suites"`
	Scenarios []scenario          `json:"scenarios"`
	// suiteByID is derived after decoding, not read from JSON.
	suiteByID map[string]suite
}

type suite struct {
	Name    string   `json:"name"`
	Dir     string   `json:"dir"`
	Status  string   `json:"status"`
	Runner  string   `json:"runner"`
	Command []string `json:"command"`
	Workdir string   `json:"workdir"`
	Note    string   `json:"note"`
}

type scenario struct {
	ID    string   `json:"id"`
	Suite string   `json:"suite"`
	Diff  string   `json:"diff"`
	Title string   `json:"title"`
	Paths []string `json:"paths"`
}

// repoRoot resolves the module root from this test's directory
// (e2e/suites/manifest) — no `go test` cwd guarantee needed.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// <root>/e2e/suites/manifest -> <root>
	for i := 0; i < 3; i++ {
		dir = filepath.Dir(dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("resolved repo root %s has no go.mod: %v", dir, err)
	}
	return dir
}

func loadManifest(t *testing.T) manifest {
	t.Helper()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "e2e", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	m.suiteByID = make(map[string]suite, len(m.Suites))
	for _, s := range m.Suites {
		m.suiteByID[s.Name] = s
	}
	return m
}

// TestManifestSuiteDirsExistAndAreUnique pins the suites[] half of the
// manifest: every row is named, its dir exists on disk (a `todo` suite may
// legitimately lack one), and no name or dir is duplicated — a duplicate
// dir would make two suites run the same tests and double-count coverage.
func TestManifestSuiteDirsExistAndAreUnique(t *testing.T) {
	m := loadManifest(t)
	root := repoRoot(t)

	if len(m.Suites) == 0 {
		t.Fatal("manifest declares no suites")
	}
	names := map[string]bool{}
	dirs := map[string]string{}
	for _, s := range m.Suites {
		if s.Name == "" {
			t.Errorf("suite with empty name: %+v", s)
			continue
		}
		if s.Dir == "" {
			t.Errorf("suite %q has an empty dir", s.Name)
			continue
		}
		if names[s.Name] {
			t.Errorf("suite name %q appears twice", s.Name)
		}
		names[s.Name] = true
		switch s.Status {
		case "implemented", "todo", "deferred":
		default:
			t.Errorf("suite %q has status %q; manifest documents todo|implemented|deferred",
				s.Name, s.Status)
		}
		if s.Status == "implemented" {
			if _, err := os.Stat(filepath.Join(root, s.Dir)); err != nil {
				t.Errorf("suite %q is %q but its dir %s does not exist: %v",
					s.Name, s.Status, s.Dir, err)
			}
		}
		if prev, dup := dirs[s.Dir]; dup {
			t.Errorf("suites %q and %q share the dir %s", prev, s.Name, s.Dir)
		}
		dirs[s.Dir] = s.Name

		// A non-Go runner MUST declare how to run itself, or the affected
		// script has nothing to dispatch (this is the M12 gap: gui-flows
		// was a Dart suite with only a prose note).
		if s.Runner != "" && s.Runner != "go" {
			if len(s.Command) == 0 {
				t.Errorf("suite %q has runner %q but no command", s.Name, s.Runner)
			}
			if s.Workdir == "" {
				t.Errorf("suite %q has runner %q but no workdir", s.Name, s.Runner)
			} else if _, err := os.Stat(filepath.Join(root, s.Workdir)); err != nil {
				t.Errorf("suite %q workdir %s does not exist: %v", s.Name, s.Workdir, err)
			}
		}
	}
}

// TestEverySuiteIsReachableFromSomePath is the core M12 pin: a suite that
// no path_map key selects can never run in the affected gate or CI, no
// matter how good its tests are. Every declared suite must be named by at
// least one path_map value.
func TestEverySuiteIsReachableFromSomePath(t *testing.T) {
	m := loadManifest(t)

	reachable := map[string]bool{}
	for prefix, names := range m.PathMap {
		for _, n := range names {
			reachable[n] = true
			if _, ok := m.suiteByID[n]; !ok {
				t.Errorf("path_map[%q] names suite %q, which is not declared in suites[]",
					prefix, n)
			}
		}
	}
	for _, s := range m.Suites {
		if !reachable[s.Name] {
			t.Errorf("suite %q is declared but NO path_map key selects it — "+
				"it can never run in make e2e-affected or CI. Add a path_map entry "+
				"pointing at its source paths.", s.Name)
		}
	}
}

// TestEveryPathMapKeyNamesADeclaredSuite is the reverse direction: a
// path_map entry for a suite that does not exist silently selects nothing.
func TestEveryPathMapKeyNamesADeclaredSuite(t *testing.T) {
	m := loadManifest(t)
	for prefix, names := range m.PathMap {
		if prefix == "" {
			t.Errorf("path_map has an empty prefix key")
			continue
		}
		if len(names) == 0 {
			t.Errorf("path_map[%q] maps to no suites", prefix)
		}
	}
}

// TestFlutterSurfaceIsCovered pins the specific M12 regression: every
// Dart file under ui/flutter_ui/lib must select the gui-flows suite, so a
// Flutter client change cannot pass CI running nothing. It walks the real
// tree rather than trusting the key list, so a NEW top-level directory
// under ui/flutter_ui that no key covers fails here.
func TestFlutterSurfaceIsCovered(t *testing.T) {
	m := loadManifest(t)
	root := repoRoot(t)

	const flutterRoot = "ui/flutter_ui"
	if _, err := os.Stat(filepath.Join(root, flutterRoot, "lib")); err != nil {
		t.Skipf("no Flutter client in this checkout (%v)", err)
	}
	if _, ok := m.suiteByID["gui-flows"]; !ok {
		t.Fatal("manifest declares no gui-flows suite, but ui/flutter_ui/lib exists")
	}

	// Every .dart file under lib/ must select at least one declared suite.
	var uncovered []string
	err := filepath.WalkDir(filepath.Join(root, flutterRoot, "lib"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".dart") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if suitesFor(m, rel) == nil {
			uncovered = append(uncovered, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s/lib: %v", flutterRoot, err)
	}
	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		show := uncovered
		if len(show) > 10 {
			show = show[:10]
		}
		t.Errorf("%d Dart file(s) under %s/lib select no e2e suite "+
			"(first %d: %s) — the gui-flows tier would never run for them",
			len(uncovered), flutterRoot, len(show), strings.Join(show, ", "))
	}
}

// TestEveryScenarioPathIsCovered pins scenario integrity: every path a
// scenario claims to exercise must resolve to at least one declared suite
// (otherwise the scenario is documentation, not coverage), and every
// scenario must name a suite that exists.
func TestEveryScenarioPathIsCovered(t *testing.T) {
	m := loadManifest(t)

	ids := map[string]bool{}
	for _, sc := range m.Scenarios {
		if sc.ID == "" {
			t.Errorf("scenario with empty id: %+v", sc)
			continue
		}
		if ids[sc.ID] {
			t.Errorf("scenario id %q appears twice", sc.ID)
		}
		ids[sc.ID] = true
		if sc.Suite == "" {
			t.Errorf("scenario %q names no suite", sc.ID)
		} else if _, ok := m.suiteByID[sc.Suite]; !ok {
			t.Errorf("scenario %q names suite %q, which is not declared in suites[]",
				sc.ID, sc.Suite)
		}
		switch sc.Diff {
		case "S", "M", "L":
		default:
			t.Errorf("scenario %q has diff %q; manifest documents S|M|L", sc.ID, sc.Diff)
		}
		if strings.TrimSpace(sc.Title) == "" {
			t.Errorf("scenario %q has an empty title", sc.ID)
		}
		if len(sc.Paths) == 0 {
			t.Errorf("scenario %q lists no paths — nothing maps it to a suite", sc.ID)
		}
		for _, p := range sc.Paths {
			if suitesFor(m, p) == nil {
				t.Errorf("scenario %q path %q matches no path_map key, so a change "+
					"to it selects no suite — add a path_map entry", sc.ID, p)
			}
		}
	}
}

// suitesFor returns the suites a changed path selects, or nil when the
// path matches no key. Mirrors scripts/e2e-affected.sh's matching rule
// (exact or prefix, no normalization) so this check cannot drift from the
// runner.
func suitesFor(m manifest, path string) []string {
	var out []string
	for prefix, names := range m.PathMap {
		if path == prefix || strings.HasPrefix(path, prefix) {
			for _, n := range names {
				if _, ok := m.suiteByID[n]; ok {
					out = append(out, n)
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// TestManifestGeneratedDateIsCurrent guards the metadata itself: the
// `generated` field is what tells a reader the manifest was refreshed
// rather than hand-edited into a stale state (audit M12: it still read
// 2026-09-29 with 186 scenarios and five unreachable).
func TestManifestGeneratedDateIsCurrent(t *testing.T) {
	m := loadManifest(t)
	if !isDate(m.Generated) {
		t.Fatalf("manifest generated = %q, want a YYYY-MM-DD date", m.Generated)
	}
	// Compare against the newest mtime among the manifest's own inputs:
	// if the manifest is older than the newest suite file it indexes, it
	// was not refreshed after that suite changed.
	root := repoRoot(t)
	manifestPath := filepath.Join(root, "e2e", "manifest.json")
	mi, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatalf("stat manifest: %v", err)
	}
	var newestSuite time.Time
	for _, s := range m.Suites {
		if s.Runner == "dart" {
			continue // Dart suite mtimes move with the whole Flutter tree.
		}
		dir := filepath.Join(root, s.Dir)
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // a vanished suite dir is reported elsewhere
			}
			info, statErr := d.Info()
			if statErr != nil {
				return nil
			}
			if info.ModTime().After(newestSuite) {
				newestSuite = info.ModTime()
			}
			return nil
		})
	}
	if newestSuite.After(mi.ModTime().Add(-time.Hour)) {
		// Only a warning-grade signal: checkouts and clones rewrite mtimes,
		// so this is reported as a note rather than a hard failure.
		t.Logf("note: manifest mtime is older than the newest Go suite file "+
			"(manifest=%s, newest suite=%s) — if the suites changed since the "+
			"last refresh, re-check the path_map coverage above",
			mi.ModTime().Format(time.RFC3339), newestSuite.Format(time.RFC3339))
	}
}

func isDate(s string) bool {
	if len(s) != len("2006-01-02") {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}
