package pii

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // sqlite driver registration
)

// ErrStoreClosed is returned by store operations after Close.
var ErrStoreClosed = errors.New("pii swap store is closed")

// SwapStore is a durable, session-scoped swap map:
// (session_id, token) -> (value, class), with a reverse index
// (session_id, class, value) -> token. Rows persist in a SQLite file
// under the caller-provided directory; an in-memory mirror serves the
// process lifetime. Values are in-process only and never appear in error
// strings (identifiers only).
//
// Lock discipline mirrors internal/security/secrets.go: one RWMutex, no
// I/O under the lock - writes snapshot state under the lock and execute
// after unlock, rolling the mirror back on failure.
type SwapStore struct {
	mu     sync.RWMutex
	mem    map[string]storeRow // storeKey(sessionID, token) -> row
	fwd    map[string]string   // storeKey(sessionID, class, value) -> token
	db     *sql.DB
	ins    *sql.Stmt
	closed bool
	// ttlSec is the restore window in seconds (research D4). 0 = session
	// lifetime (rows never expire). When > 0, Put stamps each row's
	// expiry and the Resolve paths refuse expired rows.
	ttlSec int64
}

// storeRow is one swap mapping.
type storeRow struct {
	Token  string
	Value  string
	Class  string
	Expiry int64 // unix seconds; 0 = session lifetime (leaf 02 owns TTL wiring)
}

// NewSwapStore opens (or creates) the durable store under dir. A missing
// directory is created with 0700 (leaf 02 passes a MEEPT_HOME path).
func NewSwapStore(dir string) (*SwapStore, error) {
	if dir == "" {
		return nil, errors.New("pii swap store: dir is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("pii swap store: creating dir: %w", err)
	}
	dbPath := filepath.Join(dir, "pii_swap.db")
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)" // modernc _pragma form (bughunt 2026-11-07)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("pii swap store: open: %w", err)
	}
	schema := `CREATE TABLE IF NOT EXISTS pii_swaps (
		session_id TEXT NOT NULL,
		token      TEXT NOT NULL,
		value      TEXT NOT NULL,
		class      TEXT NOT NULL,
		expiry     INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (session_id, token)
	);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("pii swap store: schema: %w", err)
	}
	ins, err := db.Prepare(`INSERT INTO pii_swaps (session_id, token, value, class, expiry)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(session_id, token) DO UPDATE SET value=excluded.value, class=excluded.class, expiry=excluded.expiry`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("pii swap store: prepare: %w", err)
	}

	st := &SwapStore{
		mem: make(map[string]storeRow),
		fwd: make(map[string]string),
		db:  db,
		ins: ins,
	}
	if err := st.loadFromDisk(); err != nil {
		ins.Close()
		db.Close()
		return nil, err
	}
	return st, nil
}

// loadFromDisk hydrates the in-memory mirrors from SQLite at open time.
// Rows are drained from the DB first and the maps filled after - no I/O
// under the lock (secrets.go discipline).
func (st *SwapStore) loadFromDisk() error {
	type diskRow struct {
		sessionID, token, value, class string
		expiry                         int64
	}
	var drained []diskRow
	rows, err := st.db.Query(`SELECT session_id, token, value, class, expiry FROM pii_swaps`)
	if err != nil {
		return fmt.Errorf("pii swap store: load: %w", err)
	}
	for rows.Next() {
		var r diskRow
		if err := rows.Scan(&r.sessionID, &r.token, &r.value, &r.class, &r.expiry); err != nil {
			rows.Close()
			return fmt.Errorf("pii swap store: scan row: %w", err)
		}
		drained = append(drained, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("pii swap store: load rows: %w", err)
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	for _, r := range drained {
		row := storeRow{Token: r.token, Value: r.value, Class: r.class, Expiry: r.expiry}
		st.mem[storeKey(r.sessionID, r.token)] = row
		st.fwd[forwardKey(r.sessionID, r.class, r.value)] = r.token
	}
	return nil
}

func storeKey(sessionID, token string) string {
	return sessionID + "\x00" + token
}

func forwardKey(sessionID, class, value string) string {
	return sessionID + "\x00" + class + "\x00" + value
}

func splitStoreKey(key string) (sessionID, token string, ok bool) {
	i := strings.IndexByte(key, 0)
	if i < 0 {
		return "", "", false
	}
	return key[:i], key[i+1:], true
}

// SetRestoreWindow enables the restore-window TTL (research D4): rows
// Put afterwards expire after the given window and the Resolve paths
// refuse them. A zero or negative window disables expiry (session
// lifetime). Called once at wiring time, before any Mask traffic.
func (st *SwapStore) SetRestoreWindow(d time.Duration) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if d <= 0 {
		st.ttlSec = 0
		return
	}
	st.ttlSec = int64(d / time.Second)
}

// expiredLocked reports whether a row is past its restore window. Rows
// carry a stamp = now + ttlSec at Put time; 0 = no expiry.
func expiredLocked(row storeRow, now int64) bool {
	return row.Expiry != 0 && now >= row.Expiry
}

// Put records a swap mapping for sessionID. Re-Putting the same
// (session, token) pair is an idempotent update.
func (st *SwapStore) Put(sessionID, token, value, class string) error {
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		return ErrStoreClosed
	}
	var expiry int64
	if st.ttlSec > 0 {
		expiry = time.Now().Unix() + st.ttlSec
	}
	mkey := storeKey(sessionID, token)
	fkey := forwardKey(sessionID, class, value)
	prev, hadPrev := st.mem[mkey]
	prevTok, hadFwd := st.fwd[fkey]
	st.mem[mkey] = storeRow{Token: token, Value: value, Class: class, Expiry: expiry}
	st.fwd[fkey] = token
	db := st.db
	st.mu.Unlock()

	if _, err := db.Exec(`INSERT INTO pii_swaps (session_id, token, value, class, expiry)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(session_id, token) DO UPDATE SET value=excluded.value, class=excluded.class, expiry=excluded.expiry`,
		sessionID, token, value, class, expiry); err != nil {
		// Roll the in-memory mirrors back so they never diverge from disk.
		st.mu.Lock()
		if hadPrev {
			st.mem[mkey] = prev
		} else {
			delete(st.mem, mkey)
		}
		if hadFwd {
			st.fwd[fkey] = prevTok
		} else {
			delete(st.fwd, fkey)
		}
		st.mu.Unlock()
		// Error wraps the session index length, never the value text.
		return fmt.Errorf("pii swap store: put (session_len=%d): %w", len(sessionID), err)
	}
	return nil
}

// Resolve returns the original value for (sessionID, token). Tokens from
// other sessions never resolve here; rows past the restore window (TTL,
// research D4) are refused exactly like foreign tokens.
func (st *SwapStore) Resolve(sessionID, token string) (string, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	row, ok := st.mem[storeKey(sessionID, token)]
	if !ok {
		return "", false
	}
	if expiredLocked(row, time.Now().Unix()) {
		return "", false
	}
	return row.Value, true
}

// TokenFor returns the existing token for (sessionID, class, value) if
// one was minted before - the in-session determinism the Swapper relies
// on (same value -> same token within a session, never across sessions).
func (st *SwapStore) TokenFor(sessionID, class, value string) (string, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	tok, ok := st.fwd[forwardKey(sessionID, class, value)]
	return tok, ok
}

// ResolveForRestore replaces ALL unexpired tokens belonging to sessionID
// in text with their original values and returns (restored, count).
// Tokens from other sessions, unknown tokens, and rows past the restore
// window (TTL, research D4) are left untouched as literals.
func (st *SwapStore) ResolveForRestore(sessionID, text string) (string, int) {
	// Snapshot the session's token set under the lock; replacement runs
	// after unlock (fixed-string matching, no I/O).
	now := time.Now().Unix()
	st.mu.RLock()
	tokens := make(map[string]string, len(st.mem))
	for key, row := range st.mem {
		if sid, _, ok := splitStoreKey(key); ok && sid == sessionID && !expiredLocked(row, now) {
			tokens[row.Token] = row.Value
		}
	}
	st.mu.RUnlock()

	if len(tokens) == 0 {
		return text, 0
	}

	count := 0
	var b strings.Builder
	i := 0
	for i < len(text) {
		best, bestVal := "", ""
		for tok, val := range tokens {
			if len(tok) > len(best) && strings.HasPrefix(text[i:], tok) {
				best, bestVal = tok, val
			}
		}
		if best == "" {
			b.WriteByte(text[i])
			i++
			continue
		}
		b.WriteString(bestVal)
		count++
		i += len(best)
	}
	return b.String(), count
}

// Close releases the SQLite handle. Subsequent Puts fail with
// ErrStoreClosed; the on-disk rows remain for reopening.
func (st *SwapStore) Close() error {
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		return nil
	}
	st.closed = true
	ins, db := st.ins, st.db
	st.ins, st.db = nil, nil
	st.mu.Unlock()
	// Close I/O OUTSIDE the lock (mutexio rule): the closed flag is
	// already set, so concurrent Put/Resolve see ErrStoreClosed while
	// the handles drain.
	if ins != nil {
		ins.Close()
	}
	if db != nil {
		return db.Close()
	}
	return nil
}
