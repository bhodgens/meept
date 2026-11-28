package memory

import (
	"time"

	"github.com/caimlas/meept/internal/migrate"
)

// isoTimeLayout is the fixed-width RFC3339 layout used for every timestamp
// column written by the memory stores: always 9 fraction digits, always the
// UTC 'Z' suffix, always 30 characters.
//
// Why not time.RFC3339Nano: it TRIMS trailing zeros of the fraction, so
// .183300 serializes as ".1833Z" (24 chars) while .183388 serializes as
// ".183388Z". Every query-side comparison of these columns is a lexical
// string comparison (WHERE created_at < / >=, ORDER BY created_at), and at
// the first differing character 'Z' (0x5A) sorts ABOVE any digit — the older
// ".1833Z" row reads as NEWER than ".183388Z". Issue #196 reproduced exactly
// this inversion in TestEpisodicMemoryGetOldMemories.
//
// A numeric SQL comparison is NOT the fix: unixepoch(x,'subsec') on the
// modernc.org/sqlite driver in use truncates to milliseconds (.1833 -> .183),
// which breaks strict boundary equality (a cutoff at ".183388" must still
// compare EXACTLY against a stored ".183388000"). Fixed-width text keeps
// both lexical ordering and exact equality.
const isoTimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// IsoTime formats t as the fixed-width, lexically-sortable UTC RFC3339
// timestamp used by all memory store timestamp columns. Readers keep
// time.Parse(time.RFC3339Nano, ...): that layout accepts both the padded
// (".183300000Z") and the legacy trimmed (".1833Z") shapes.
func IsoTime(t time.Time) string {
	return t.UTC().Format(isoTimeLayout)
}

// isoTimeLen is the fixed width of IsoTime output — the migration's
// "already padded" predicate compares against it. Computed from the writer,
// never hardcoded blindly: TestIsoTimeWidthMatchesMigrate pins it to the
// shared SQL-side constant internal/migrate uses (FixedWidthTimeLen), so a
// layout edit breaks that pin loudly instead of silently disabling padding.
var isoTimeLen = len(IsoTime(time.Unix(0, 0)))

// padTimestampsStep builds the registry migration step that rewrites legacy
// variable-width timestamps to fixed width for this store's table. Runs
// AFTER the column-adding steps so created_at/last_accessed_at are present.
// Idempotent twice over: the guard skips when nothing is pending, and the
// WHERE clause never matches an already-padded value.
func (s *SQLiteFTSStore) padTimestampsStep(version int) migrate.Migration {
	cols := s.config.TimestampColumns
	return migrate.Migration{
		Version: version,
		Name:    "pad legacy timestamps to fixed width (#196)",
		// #nosec G201 -- table name from FTSConfig, not user input
		SQL:   migrate.PadLegacyTimestampsSQL(s.config.TableName, cols...),
		Guard: migrate.LegacyPaddingPending(s.config.TableName, cols...),
	}
}
