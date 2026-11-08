package session

// Session format versioning + forward-only migration catalog (contract S1,
// .hermes/contracts/03-session-storage.md; leaf 03-session-storage/01).
//
// Ported semantics from dsh session-format/src/chain.ts:
//   - every migration is adjacent (ToVersion == FromVersion+1),
//   - the catalog is build-static and validated at construction: for every
//     version v in [0, SessionFormatVersion) there is EXACTLY ONE adjacent
//     entry; duplicates, unnamed entries, and entries whose FromVersion is
//     at or beyond the current version reject,
//   - Plan refuses stored-newer formats (ErrSessionFormatNewer) and missing
//     intervening hops (ErrSessionFormatGap); it never returns a partial
//     chain.
//
// This branch ships a single no-op backfill hop (0 -> 1): rows written
// before the schema_version column existed are treated as version 0 and
// migrate forward without a row transform. The value of the catalog is the
// fail-closed guard (refuse newer/gapped formats), not a rewrite ladder.

import (
	"errors"
	"fmt"
	"sort"
)

// SessionFormatVersion is the session format version of THIS build
// (dsh SESSION_FORMAT_VERSION). Stored rows stamped with a higher version
// were written by a future build and are refused, never rewritten.
const SessionFormatVersion = 1

// ErrSessionFormatNewer is returned when a stored session carries a format
// version ahead of this build (dsh chain.ts plan(): "stored Session uses
// newer format").
var ErrSessionFormatNewer = errors.New("session: stored format newer than this build")

// ErrSessionFormatGap is returned when the catalog cannot build a complete
// adjacent chain from the stored version to the current version.
var ErrSessionFormatGap = errors.New("session: missing adjacent migration in chain")

// SessionVersionedRow is the per-row view handed to a migration's
// MigrateUp transform. This branch ships no row-transforming migrations;
// the shape exists so future branches add hops without changing the
// catalog contract.
type SessionVersionedRow struct {
	SessionID   string
	FromVersion int
}

// SessionFormatMigration is one adjacent forward hop in the catalog.
// MigrateUp nil = no-op hop (version stamp only).
type SessionFormatMigration struct {
	Name        string
	FromVersion int
	ToVersion   int // must equal FromVersion+1 (adjacent)
	MigrateUp   func(row SessionVersionedRow) error
}

// SessionFormatCatalog is the build-static, validated set of adjacent
// forward migrations. Construct via NewSessionFormatCatalog; a catalog
// that fails validation is never partially usable.
type SessionFormatCatalog struct {
	byFrom map[int]SessionFormatMigration
}

// defaultSessionFormatCatalog is the catalog this build ships: a single
// no-op 0 -> 1 backfill hop (pre-column legacy rows). Package-level so
// store accessors share one validated instance.
var defaultSessionFormatCatalog = mustNewSessionFormatCatalog([]SessionFormatMigration{
	{
		Name:        "v0-to-v1-backfill",
		FromVersion: 0,
		ToVersion:   1,
		MigrateUp:   nil, // no-op: stamp only
	},
})

func mustNewSessionFormatCatalog(migrations []SessionFormatMigration) *SessionFormatCatalog {
	c, err := NewSessionFormatCatalog(migrations)
	if err != nil {
		// A build-static catalog that fails validation is a programming
		// error discoverable at init time, not a runtime condition.
		panic(fmt.Sprintf("session: invalid default format catalog: %v", err))
	}
	return c
}

// NewSessionFormatCatalog validates the chain and returns the catalog.
// Validation (dsh chain.ts:64-77): adjacent-only (To == From+1), unique
// FromVersion (duplicates reject), non-empty Name, FromVersion within
// [0, SessionFormatVersion), and exactly one entry for every version in
// [0, SessionFormatVersion) — a missing intervening hop rejects here
// (never at plan time for a build-static gap).
func NewSessionFormatCatalog(migrations []SessionFormatMigration) (*SessionFormatCatalog, error) {
	byFrom := make(map[int]SessionFormatMigration, len(migrations))
	for _, m := range migrations {
		if m.Name == "" {
			return nil, fmt.Errorf("session: unnamed migration %d->%d", m.FromVersion, m.ToVersion)
		}
		if m.ToVersion != m.FromVersion+1 {
			return nil, fmt.Errorf("session: migration %q is not adjacent (%d->%d)", m.Name, m.FromVersion, m.ToVersion)
		}
		if m.FromVersion < 0 || m.FromVersion >= SessionFormatVersion {
			return nil, fmt.Errorf("session: migration %q from version %d outside [0,%d)", m.Name, m.FromVersion, SessionFormatVersion)
		}
		if _, dup := byFrom[m.FromVersion]; dup {
			return nil, fmt.Errorf("session: duplicate migration from version %d", m.FromVersion)
		}
		byFrom[m.FromVersion] = m
	}
	for v := range SessionFormatVersion {
		if _, ok := byFrom[v]; !ok {
			return nil, fmt.Errorf("session: %w: no migration from version %d", ErrSessionFormatGap, v)
		}
	}
	return &SessionFormatCatalog{byFrom: byFrom}, nil
}

// Plan returns the ordered adjacent migrations that carry a session stored
// at version `from` up to SessionFormatVersion. Refusals:
//   - from > SessionFormatVersion -> ErrSessionFormatNewer (never rewrite a
//     future build's rows),
//   - from == SessionFormatVersion -> empty plan, nil error (no-op),
//   - a missing intervening hop -> ErrSessionFormatGap.
//
// The returned slice is ordered by FromVersion ascending and is safe to
// apply in sequence.
func (c *SessionFormatCatalog) Plan(from int) ([]SessionFormatMigration, error) {
	if from > SessionFormatVersion {
		return nil, fmt.Errorf("%w: stored %d, build %d", ErrSessionFormatNewer, from, SessionFormatVersion)
	}
	if from < 0 {
		return nil, fmt.Errorf("session: negative stored format version %d", from)
	}
	if from == SessionFormatVersion {
		return nil, nil
	}
	var plan []SessionFormatMigration
	for v := from; v < SessionFormatVersion; v++ {
		m, ok := c.byFrom[v]
		if !ok {
			return nil, fmt.Errorf("%w: no migration from version %d", ErrSessionFormatGap, v)
		}
		plan = append(plan, m)
	}
	sort.Slice(plan, func(i, j int) bool { return plan[i].FromVersion < plan[j].FromVersion })
	return plan, nil
}
