// overrides.go — operator-managed time-boxed tool grants (agent-groups
// plan tree docs/plans/20261030-agent-groups, leaf 07, contract C8).
//
// The overrides FILE is $MEEPT_HOME/group_overrides.json5 (gitignore-able,
// operator-managed): a reviewable config artifact that re-invents the dead
// engine's overrides table. Entries:
//
//	{subject: "agent:x", allow: ["git_push"], reason: "deploy window",
//	 expires: "2026-11-05T17:00:00Z"}
//
// Semantics (C8 verbatim):
//   - overrides ADD allows ONLY — never deny, never widen memory/network
//     ceilings. This is enforced STRUCTURALLY: the Override type has no
//     deny field and no ceiling fields, so an entry cannot express them
//     (TestOverridesAddOnlyShape pins the field set by reflection).
//   - Expired entries are skipped with ONE Warn at load.
//   - A malformed entry is a NAMED error (ErrMalformedOverride), never a
//     silent skip — fail closed, same posture as the boot validator.
//
// Clock discipline: the reader's decision path NEVER calls time.Now —
// callers inject Now (OverrideLoadOptions.Now). A nil Now is a named
// error, so no call site can silently fall back to wall-clock.
//
// The daemon feeds the active allows into resolution through
// ResolverOptions.ExtraAllowsFor (leaf 04's seam, per-subject). Deny still
// beats override and the spec stays the ceiling — resolve.go owns those
// precedence rules; this file only decides WHICH entries are alive.

package groups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tailscale/hujson"
)

// OverridesFileName is the C8 file name under $MEEPT_HOME. The CLI and the
// daemon resolve the full path through config.MeeptPath (kept OUT of this
// package: internal/groups stays import-light).
const OverridesFileName = "group_overrides.json5"

// ErrMalformedOverride is wrapped by every malformed-entry error so callers
// can classify (errors.Is) without string matching.
var ErrMalformedOverride = errors.New("malformed group override")

// overrideFilePerm is the on-disk permission mode: owner-only (the file
// names security exceptions; same posture as the users store).
const overrideFilePerm = 0o600

// Override is ONE time-boxed allow grant (C8 entry shape). Add-only BY
// TYPE SHAPE: there is no deny field and no ceiling fields — an entry
// cannot express a denial or a memory/network widening, so the "add-only"
// rule needs no runtime enforcement and no reviewer vigilance.
//
// Expires is required (an override without an expiry is not time-boxed);
// encoding/json enforces the RFC3339 wire format.
type Override struct {
	// Subject the grant applies to: "agent:<id>", "group:<name>", or
	// "user:<id>" (same subject vocabulary as bindings). Note: the leaf
	// 07 daemon seam resolves per-AGENT subjects only ("agent:<id>") —
	// group/user-subject entries load and appear in list/check, but no
	// gate consumes them yet (documented limitation).
	Subject string `json:"subject"`
	// Allow lists tool names granted ADDITIONALLY to the subject's
	// policies. Never a deny; never a ceiling. Names are normalized by
	// the resolver's injected normalizer at resolution time.
	Allow []string `json:"allow"`
	// Reason is the human justification — required, because the file is
	// a reviewable artifact: an unexplained grant is a malformed entry.
	Reason string `json:"reason"`
	// Expires is the RFC3339 instant after which the grant is dead. An
	// entry active while now < expires; expired once now >= expires.
	Expires time.Time `json:"expires"`
}

// Validate reports whether the entry is well-formed. Every failure wraps
// ErrMalformedOverride and NAMES the offending field + subject.
func (o Override) Validate() error {
	if !wellFormedSubject(o.Subject) {
		return fmt.Errorf("%w: subject %q is not agent:<id>|group:<name>|user:<id>",
			ErrMalformedOverride, o.Subject)
	}
	if len(o.Allow) == 0 {
		return fmt.Errorf("%w: %s: allow list is empty (an override grants at least one tool)",
			ErrMalformedOverride, o.Subject)
	}
	for _, a := range o.Allow {
		if a == "" {
			return fmt.Errorf("%w: %s: allow list carries an empty tool name",
				ErrMalformedOverride, o.Subject)
		}
	}
	if o.Reason == "" {
		return fmt.Errorf("%w: %s: reason is required (the file is a reviewable artifact)",
			ErrMalformedOverride, o.Subject)
	}
	if o.Expires.IsZero() {
		return fmt.Errorf("%w: %s: expires is required (overrides are time-boxed only)",
			ErrMalformedOverride, o.Subject)
	}
	return nil
}

// Expired reports whether the entry is dead at the given instant
// (now >= expires; the entry is active up to but not including expires).
func (o Override) Expired(now time.Time) bool {
	return !now.Before(o.Expires)
}

// OverrideSet is a loaded overrides file: the entries split by the load
// clock. Active/Expired keep the file's order (review surface — never
// reorder the operator's artifact).
type OverrideSet struct {
	// Active entries survived the load-time expiry filter.
	Active []Override
	// Expired entries were SKIPPED (exactly one Warn covers the batch).
	Expired []Override
	// Warnings carries the load warnings (one per class). Populated even
	// when a Warn callback was supplied, so callers can re-log or count.
	Warnings []string
}

// AllowsFor returns the union of active grants for the exact subject
// string, filtered by the QUERY clock (not just the load clock): a set
// loaded mid-window answers correctly after entries expire. Sorted and
// deduplicated; nil when nothing applies.
func (s *OverrideSet) AllowsFor(subject string, now time.Time) []string {
	if s == nil {
		return nil
	}
	var allow []string
	for _, o := range s.Active {
		if o.Subject != subject || o.Expired(now) {
			continue
		}
		allow = append(allow, o.Allow...)
	}
	return sortDedup(allow)
}

// ActiveCount/ExpiredCount are the `groups check` report counts (leaf 07
// T4: diagnosis consumes these; the 06-diagnosis service calls them when
// it lands).
func (s *OverrideSet) ActiveCount() int {
	if s == nil {
		return 0
	}
	return len(s.Active)
}

func (s *OverrideSet) ExpiredCount() int {
	if s == nil {
		return 0
	}
	return len(s.Expired)
}

// OverrideLoadOptions tunes LoadOverrides. Now is REQUIRED (the reader's
// decision path has no clock of its own); Warn is optional.
type OverrideLoadOptions struct {
	// Now is the decision clock for expiry. Nil => named error (never a
	// silent time.Now fallback).
	Now func() time.Time
	// Warn, when non-nil, receives the single batch warning emitted for
	// skipped expired entries. Warnings are also collected on the set.
	Warn func(msg string)
}

// loadOverridesDoc is the accepted file shapes: a top-level array of
// entries (the C8 example form) or {"overrides": [...]} (the wrapper the
// writer produces).
type overridesDoc struct {
	Overrides []Override `json:"overrides"`
}

// LoadOverrides reads and filters an overrides file. A MISSING file is an
// empty set (first-use out of the box, users-store precedent); a
// MALFORMED file or entry is a named error (never a silent skip); expired
// entries are skipped with ONE batch warning.
func LoadOverrides(path string, opts OverrideLoadOptions) (*OverrideSet, error) {
	if opts.Now == nil {
		return nil, fmt.Errorf("load overrides %s: now func is required (inject the clock; the reader has none)", path)
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is resolved by the caller (config.MeeptPath / --file)
	if err != nil {
		if os.IsNotExist(err) {
			return &OverrideSet{}, nil
		}
		return nil, fmt.Errorf("load overrides %s: %w", path, err)
	}
	stdJSON, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("%w: parse %s: %v", ErrMalformedOverride, path, err)
	}

	// Accept both the C8 top-level array and the writer's wrapper form.
	var arr []Override
	if err := json.Unmarshal(stdJSON, &arr); err != nil {
		var wrapper overridesDoc
		if err2 := json.Unmarshal(stdJSON, &wrapper); err2 != nil {
			return nil, fmt.Errorf("%w: decode %s: %v", ErrMalformedOverride, path, err)
		}
		arr = wrapper.Overrides
	}

	now := opts.Now()
	set := &OverrideSet{}
	for i, o := range arr {
		if err := o.Validate(); err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		if o.Expired(now) {
			set.Expired = append(set.Expired, o)
			continue
		}
		set.Active = append(set.Active, o)
	}
	if len(set.Expired) > 0 {
		msg := expiredWarn(set.Expired)
		set.Warnings = append(set.Warnings, msg)
		if opts.Warn != nil {
			opts.Warn(msg)
		}
	}
	return set, nil
}

// expiredWarn renders the ONE batch warning for a load's skipped expired
// entries (names every subject — a silent expiry is a lost discovery).
func expiredWarn(expired []Override) string {
	subjects := make([]string, 0, len(expired))
	for _, o := range expired {
		subjects = append(subjects, o.Subject)
	}
	return fmt.Sprintf("skipped %d expired group_overrides entries: %v",
		len(expired), subjects)
}

// SaveOverrides persists entries atomically: indented JSON (a valid JSON5
// subset, wrapper form) written to a same-directory temp file at 0600 and
// renamed over the target (users-store saveStore precedent).
func SaveOverrides(path string, entries []Override) error {
	for i, o := range entries {
		if err := o.Validate(); err != nil {
			return fmt.Errorf("save overrides %s: entry %d: %v", path, i, err)
		}
	}
	doc := overridesDoc{Overrides: entries}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal overrides %s: %w", path, err)
	}
	data = append(data, '\n')

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create overrides directory: %w", err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".group-overrides-*.tmp")
	if err != nil {
		return fmt.Errorf("create overrides temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write overrides temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close overrides temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, overrideFilePerm); err != nil {
		cleanup()
		return fmt.Errorf("chmod overrides temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename overrides temp file to %s: %w", path, err)
	}
	return nil
}

// PruneOverrides removes expired entries from the file at path (load with
// the injected clock, rewrite the survivors atomically). When nothing
// expired the file is left untouched (no rewrite churn). Returns the
// number of removed entries.
func PruneOverrides(path string, now func() time.Time) (int, error) {
	set, err := LoadOverrides(path, OverrideLoadOptions{Now: now})
	if err != nil {
		return 0, err
	}
	if len(set.Expired) == 0 {
		return 0, nil
	}
	if err := SaveOverrides(path, set.Active); err != nil {
		return 0, err
	}
	return len(set.Expired), nil
}

// OverrideSetReader adapts a loaded OverrideSet to the leaf-06 diagnosis
// seam (OverridesReader, diagnose.go): `groups check` counts active and
// expired overrides THROUGH this interface. The set is loaded ONCE with
// the caller's clock and both methods answer from it; rows are expanded
// one per tool so check findings can name the tool. *OverrideSet thus
// satisfies groups.OverridesReader structurally.
func (s *OverrideSet) ActiveOverrides(context.Context) ([]OverrideRow, error) {
	if s == nil {
		return nil, nil
	}
	return overrideRows(s.Active), nil
}

// ExpiredOverrides returns the load-time expired split as seam rows.
func (s *OverrideSet) ExpiredOverrides(context.Context) ([]OverrideRow, error) {
	if s == nil {
		return nil, nil
	}
	return overrideRows(s.Expired), nil
}

// overrideRows expands entries into one row per allowed tool.
func overrideRows(entries []Override) []OverrideRow {
	var rows []OverrideRow
	for _, o := range entries {
		expires := o.Expires.Format(time.RFC3339)
		tools := o.Allow
		if len(tools) == 0 {
			tools = []string{""}
		}
		for _, tool := range tools {
			rows = append(rows, OverrideRow{
				Subject:   o.Subject,
				Tool:      tool,
				ExpiresAt: expires,
			})
		}
	}
	return rows
}
