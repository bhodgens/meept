// Intent-map engine (branch 03 leaf 03; design §3.3 in
// docs/research/dynamic-tools-design.md).
//
// IntentMap is the deterministic intent-category -> tool-set selection table
// that drives the "predicted" and "search" tool-exposure modes. The dispatcher
// already runs AnalyzeTrueIntent per turn and stores the category label
// (internal/agent/intent_analyzer.go:257 / :372-377); the exposure engine
// (leaf 05) keys the predicted subset off that SAME label through For. No LLM
// call is made here — the whole point is that selection is config-driven,
// auditable, and byte-deterministic for a given (label, budget, allowed)
// triple.
package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/config"
)

// IntentMap is the deterministic intent->toolset selection table. Loaded once
// at daemon start from a JSON5 file (default template
// config/tools_intent_map.json5, copied into $MEEPT_HOME by
// `make config-bootstrap`). Invalid tool names (not in the registry universe)
// are dropped with one warn at load; a malformed file falls back to a
// "_default"-only safe map, never to an LLM call.
type IntentMap struct {
	// entries maps intent category label -> tool names in config order.
	entries map[string][]string
	// universe is the set of names considered valid at load. Nil = accept any.
	universe map[string]struct{}
}

// labelDefault is the fall-through key: unknown or missing categories
// (including "quickplan", which has no dedicated entry by design) resolve to
// entry["_default"].
const labelDefault = "_default"

// safeDefaultContent is the pinned §3.3 "_default" entry: the conservative
// v1 prediction (memory lookup only). A malformed/absent map degrades to
// exactly this shape — usable, never an LLM fallback, never a panic.
var safeDefaultContent = []string{"memory_search"}

// loadUniverseStatic is the static known-tool universe used by LoadIntentMap
// (the pinned two-arg API). It is the union of the registered builtin tool
// names and the hand-curated design-intent names from §3.3 that have no
// standalone registration yet. Callers that have the live registry can
// re-validate against it via SetUniverse (leaf 05); the static list only
// gates the "drop invalid names with one warn" behaviour at load time.
var loadUniverseStatic = func() map[string]struct{} {
	names := []string{
		// Builtin core (file/shell/git).
		"shell", "file_read", "file_write", "file_edit", "file_delete", "file_find",
		"git_overview", "git_commit", "git_split", "git_validate",
		// Memory.
		"memory_search", "memory_recall", "memory_retain", "memory_store",
		"memory_fact_search",
		// Web + transcription.
		"web_search", "web_fetch", "transcript_fetch", "json_extract",
		// Skills + knowledge.
		"skills_create", "skills_patch", "skills_load", "knowledge_search",
		// Design-intent names (dynamic-tools §3.3) without a standalone
		// registration yet — pinned so the v1 table loads without warnings;
		// they map to real registrations once the owning tool leaves land.
		"grep_files", "build", "test",
	}
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}()

// newSafeIntentMap builds the "_default"-only degraded map.
func newSafeIntentMap(universe map[string]struct{}) *IntentMap {
	return &IntentMap{
		entries:  map[string][]string{labelDefault: safeDefaultContent},
		universe: universe,
	}
}

// LoadIntentMap loads a JSON5 intent map at path and validates every entry
// against the known-tool universe. It returns the parsed map AND a non-nil
// error when the file is malformed or the top level is not an object; in
// that degraded case the returned *IntentMap is a usable "_default"-only map
// so callers log one warn and continue — never an LLM fallback, never a
// panic. A clean parse returns a nil error. An ABSENT file is not an error:
// a fresh install before the template lands still gets the safe map (nil
// error, conservative content).
//
// Invalid tool names are dropped at load with one warn naming the entry.
// Malformed files are NOT logged here (the caller owns that one warn from
// the returned error, per the §3.3 contract).
func LoadIntentMap(path string, log *slog.Logger) (*IntentMap, error) {
	return LoadIntentMapWithUniverse(path, log, loadUniverseStatic)
}

// LoadIntentMapWithUniverse is LoadIntentMap with an explicit tool universe
// (the live registry's name set, once leaf 05 wires it). A nil universe
// disables invalid-name dropping (every entry accepted as-is).
func LoadIntentMapWithUniverse(path string, log *slog.Logger, universe map[string]struct{}) (*IntentMap, error) {
	if log == nil {
		log = slog.Default()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Absent file (fresh install, template not copied yet): safe
			// default-only map, no error.
			return newSafeIntentMap(universe), nil
		}
		return newSafeIntentMap(universe), fmt.Errorf("intent map read: %w", err)
	}

	// config.UnmarshalJSON5 is the in-repo JSON5 loader (hujson-based, the
	// same path internal/cluster uses for its config files). Top level must
	// be an object mapping category label -> []string.
	var raw map[string]any
	if err := config.UnmarshalJSON5(data, &raw); err != nil {
		return newSafeIntentMap(universe), fmt.Errorf("intent map parse %s: %w", path, err)
	}
	if raw == nil {
		// Empty / top-level null: not a usable table -> safe default.
		return newSafeIntentMap(universe),
			fmt.Errorf("intent map %s: top level must be a non-empty object", path)
	}

	entries := make(map[string][]string, len(raw))
	var dropped []string // "entry/name" pairs, for one warn total.
	for label, v := range raw {
		label = strings.ToLower(strings.TrimSpace(label))
		if label == "" {
			dropped = append(dropped, "<empty-label>")
			continue
		}
		list, ok := v.([]any)
		if !ok {
			// A non-array value under a category label is invalid: drop the
			// whole entry, naming it.
			dropped = append(dropped, label+"(non-array)")
			continue
		}
		names := make([]string, 0, len(list))
		for _, item := range list {
			name, ok := item.(string)
			if !ok {
				continue // non-string element: not a tool name.
			}
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			if universe != nil {
				if _, ok := universe[name]; !ok {
					dropped = append(dropped, label+"/"+name)
					continue
				}
			}
			names = append(names, name)
		}
		// Dedupe while preserving config order (stable for For determinism).
		seen := make(map[string]struct{}, len(names))
		deduped := make([]string, 0, len(names))
		for _, n := range names {
			if _, dup := seen[n]; dup {
				continue
			}
			seen[n] = struct{}{}
			deduped = append(deduped, n)
		}
		entries[label] = deduped
	}

	if len(dropped) > 0 {
		// Exactly ONE warn naming every dropped entry (design §3.3: "invalid
		// tool names are dropped with one warn naming the entry").
		log.Warn("intent map: dropping names outside the tool universe",
			"path", path, "dropped", strings.Join(sortedUnique(dropped), ", "))
	}

	return &IntentMap{entries: entries, universe: universe}, nil
}

// SetUniverse re-validates the loaded entries against a live registry
// universe (leaf 05 calls this at daemon boot with the real registry names,
// which is the authoritative "registry universe" the §3.3 drop rule refers
// to). It mutates in place, dropping out-of-universe names, and returns the
// "entry/name" pairs that were dropped — the caller logs one warn for a
// non-nil result (the load-time contract, applied after the fact). A nil
// universe resets to "accept any" and drops nothing.
func (m *IntentMap) SetUniverse(universe map[string]struct{}) []string {
	if m == nil {
		return nil
	}
	m.universe = universe
	if universe == nil {
		return nil
	}
	var dropped []string
	for label, names := range m.entries {
		kept := make([]string, 0, len(names))
		changed := false
		for _, n := range names {
			if _, ok := universe[n]; ok {
				kept = append(kept, n)
				continue
			}
			changed = true
			dropped = append(dropped, label+"/"+n)
		}
		if changed {
			m.entries[label] = kept
		}
	}
	if len(dropped) > 0 {
		return sortedUnique(dropped)
	}
	return nil
}

// For returns entry[label] (falling back to entry["_default"] when label is
// absent) intersected with allowed, truncated to budget. budget <= 0 yields
// a nil slice (in search mode the caller passes 0). Deterministic: same
// inputs give the same slice; stable order = the config order preserved
// after intersection (never re-sorted).
//
// The result never exceeds the allowed set (the agent's role ceiling) and
// never exceeds the budget. A miss on both label and "_default", or an empty
// intersection, yields a nil/empty slice — the caller then proceeds with
// "core + meta-tools only" (design §2.1): never an error, never an LLM call.
func (m *IntentMap) For(label string, budget int, allowed []string) []string {
	if m == nil || m.entries == nil {
		return nil
	}
	if budget <= 0 {
		// Search mode: no predicted tools. Nil slice, not an error.
		return nil
	}

	names := m.entries[strings.ToLower(strings.TrimSpace(label))]
	if len(names) == 0 {
		names = m.entries[labelDefault]
	}
	if len(names) == 0 {
		return nil
	}
	if len(allowed) == 0 {
		// No ceiling supplied -> nothing to intersect with. Safe nil, not a
		// panic.
		return nil
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		allowedSet[strings.ToLower(strings.TrimSpace(a))] = struct{}{}
	}

	// Preserve config order after intersection (deterministic, stable).
	out := make([]string, 0, len(names))
	for _, n := range names {
		if _, ok := allowedSet[n]; ok {
			out = append(out, n)
			if len(out) == budget {
				break // keep the first N in config order.
			}
		}
	}
	return out
}

// Names returns the sorted set of category labels present (diagnostics /
// audit surface). Deterministic.
func (m *IntentMap) Names() []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m.entries))
	for k := range m.entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedUnique returns in, deduped and sorted (deterministic warn wording).
func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
