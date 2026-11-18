// Package kanbansvc: the production bearer-token store + single-use pairing
// codes for the kanban sidecar (master.md C7, leaf 04-sidecar/03).
//
// # WHY THIS EXISTS
//
// The gateway (04/02) authenticates browsers with a board-scoped bearer token
// mapped to a grant set (C5). Until this leaf the only implementation of the
// GrantChecker seam was StaticGrantStore, which authorizes nothing until a
// caller grants in-process. Here the grant set survives restarts in a JSON
// file under $MEEPT_HOME, and tokens are minted through a pairing handshake:
//
//   - GeneratePairCode(grants...) mints a single-use crypto/rand code. The
//     CALLER prints it once to the daemon console (the designed delivery
//     channel, same as internal/comm/http/pairing.go); it must never be
//     passed to the structured logger at info level and never returned over
//     any authenticated surface. The code expires after pairCodeTTL (10m)
//     and cannot be replayed.
//   - ExchangePairCode(code) consumes the code on first use — reuse always
//     fails — and returns a fresh 256-bit crypto/rand bearer token carrying
//     the grant set the code was minted with. Expired codes are refused with
//     the same ErrInvalidPairCode response shape as an unknown code.
//
// Threat model / scope (mirrors the daemon pairing precedent, C7 binary
// invariants):
//   - The token file is 0600 in a 0700 directory under $MEEPT_HOME, and a
//     file with ANY group/other access bits is refused at load (fail closed,
//     same posture as the github token-file check in internal/daemon).
//   - No TLS, no sqlite, no OS-env secrets in this package — the sidecar
//     binary contract.
//   - The bind guard (non-loopback refusal + loud warning) lives in
//     cmd/meept-kanban/main.go guardBind; this leaf deliberately does not
//     duplicate it.
//   - The POST /kanban/pair/exchange + RPC registration is leaf 07/01; the
//     Middleware here is the sidecar-middleware piece that wiring mounts.
//   - Codes have a 10-minute TTL (pairCodeTTL): expired codes are refused
//     with the same uniform 401 as an unknown code, and deleted so a
//     replay attacker cannot keep retrying (L7). The rate limiter (M3)
//     lives at the gateway HTTP layer, not here.
package kanbansvc

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// Contract pins: TokenStore is the production GrantChecker the gateway
// consults, so it must keep satisfying the same seam the gateway was built
// against — no gateway.go changes needed.
var _ GrantChecker = (*TokenStore)(nil)

const (
	// tokenFileName is the store's default file under $MEEPT_HOME/kanban.
	tokenFileName = "tokens.json"

	// tokenFilePerm is owner-only, matching the $MEEPT_HOME secret-file
	// convention (dev_key, github token files, cluster keys).
	tokenFilePerm = os.FileMode(0o600)
	// tokenDirPerm is owner-only-execute so the file stays private even if
	// its mode were ever loosened.
	tokenDirPerm = os.FileMode(0o700)

	// pairCodeLen is the minted code length (uppercase A–Z0–9). 8 chars of
	// a 36-char alphabet ≈ 42 bits from crypto/rand, single-use and
	// short-lived by consumption — enough for a local console handshake.
	pairCodeLen = 8
	// pairCodeAlphabet avoids lowercase so the console-printed code reads
	// unambiguously; comparison normalizes case anyway.
	pairCodeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	// tokenBytes is 256 bits of entropy per bearer token (hex-encoded).
	tokenBytes = 32

	// pairCodeTTL is how long a pairing code remains valid after minting.
	// Codes older than this are rejected as invalid (same uniform 401 as an
	// unknown code, per the no-oracle uniform-401 contract).
	pairCodeTTL = 10 * time.Minute
)

// tokenFileSchemaVersion is the on-disk format tag; unknown versions refuse
// to load rather than silently misreading grants.
const tokenFileSchemaVersion = 1

// ErrInvalidPairCode is returned when an exchange presents a code that was
// never minted, already used, or is empty. Wrong, consumed, and missing share
// one error so callers cannot oracle the store's state (same shape as the
// daemon pairing handler).
var ErrInvalidPairCode = errors.New("kanbansvc: invalid or already-used pairing code")

// tokenFileRecord is the on-disk JSON shape.
type tokenFileRecord struct {
	Version int                 `json:"version"`
	Tokens  map[string][]string `json:"tokens"`
}

// pairCodeRecord is the in-memory shape of an outstanding pairing code:
// the grant set it arms plus the time it was minted (codes older than
// pairCodeTTL are refused as invalid, not consumed, to keep the no-oracle
// contract intact).
type pairCodeRecord struct {
	grants   []string
	mintedAt time.Time
}

// TokenStore is the file-backed bearer-token store: token -> C5 grant set.
// It satisfies GrantChecker (in-memory lookup, no I/O under the lock) and
// mints tokens through single-use pairing codes. Safe for concurrent use.
type TokenStore struct {
	path string

	// mu guards codes (outstanding pairing codes, in-memory only: a
	// restart re-mints, exactly like the daemon pairing service).
	mu    sync.Mutex
	codes map[string]pairCodeRecord

	// grantsMu guards tokens. Grants() takes only a read lock and touches
	// no I/O, so the hot auth path never blocks on disk.
	grantsMu sync.RWMutex
	tokens   map[string][]string
}

// NewTokenStore loads (or lazily creates) the token store at path. An empty
// path resolves to $MEEPT_HOME/kanban/tokens.json. A missing file is an
// empty store; an EXISTING file whose mode has any group/other access bits,
// whose format tag is unknown, or whose body is not valid JSON is refused —
// the store never runs on a token file it cannot trust.
func NewTokenStore(path string) (*TokenStore, error) {
	if path == "" {
		path = config.MeeptPath("kanban", tokenFileName)
	}
	s := &TokenStore{
		path:   path,
		codes:  make(map[string]pairCodeRecord),
		tokens: make(map[string][]string),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// DefaultTokenStorePath is the $MEEPT_HOME location the daemon and the
// sidecar both agree on (leaf 07/01 wires it).
func DefaultTokenStorePath() string {
	return config.MeeptPath("kanban", tokenFileName)
}

// Path reports the backing file (used by boot logs and doctor output; it is
// not secret — the CONTENTS are).
func (s *TokenStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Grants implements GrantChecker: the grant set for a token and whether the
// token is known at all. Purely in-memory; the disk is read only at load.
func (s *TokenStore) Grants(token string) ([]string, bool) {
	if s == nil || token == "" {
		return nil, false
	}
	s.grantsMu.RLock()
	defer s.grantsMu.RUnlock()
	grants, ok := s.tokens[token]
	if !ok {
		return nil, false
	}
	return append([]string(nil), grants...), true
}

// Grant associates a token with a grant set and persists the store. It
// replaces any previous set for that token; an empty token or grant set is
// rejected rather than silently authorizing nothing under a valid-looking
// entry.
func (s *TokenStore) Grant(token string, grants ...string) error {
	if s == nil {
		return errors.New("kanbansvc: token store is nil")
	}
	if token == "" || len(grants) == 0 {
		return errors.New("kanbansvc: token and at least one grant are required")
	}
	clean, err := normalizeGrants(grants)
	if err != nil {
		return err
	}
	s.grantsMu.Lock()
	s.tokens[token] = clean
	s.grantsMu.Unlock()
	return s.persist()
}

// Revoke drops a token entirely and persists the store. Revoking an unknown
// token is a no-op that skips the write (no disk churn on typos).
func (s *TokenStore) Revoke(token string) error {
	if s == nil {
		return errors.New("kanbansvc: token store is nil")
	}
	s.grantsMu.Lock()
	_, known := s.tokens[token]
	delete(s.tokens, token)
	s.grantsMu.Unlock()
	if !known {
		return nil
	}
	return s.persist()
}

// GeneratePairCode mints a single-use pairing code carrying the given C5
// grants and returns it. The caller prints it ONCE to the daemon console —
// never to the structured logger at info level — and the code is consumed by
// the first successful ExchangePairCode (provided it has not expired). Codes
// older than pairCodeTTL are refused as invalid, not consumed, so a replay
// attacker never learns whether a code was seen.
func (s *TokenStore) GeneratePairCode(grants ...string) (string, error) {
	if s == nil {
		return "", errors.New("kanbansvc: token store is nil")
	}
	clean, err := normalizeGrants(grants)
	if err != nil {
		return "", err
	}
	code, err := randomPairCode()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	// An 8-char collision with an outstanding code is ~2^-42 per event;
	// overwriting replaces the grant set of the (unused) earlier code,
	// which is the least surprising outcome for the operator who has not
	// typed either yet.
	s.codes[code] = pairCodeRecord{grants: clean, mintedAt: time.Now()}
	s.mu.Unlock()
	return code, nil
}

// ExchangePairCode consumes a pairing code and mints the bearer token it
// arms: crypto/rand 256-bit token, stored with the grants the code was
// minted with. Single-use — the code is deleted before the token is minted,
// so a second exchange of the same code returns ErrInvalidPairCode even if
// minting or persisting then fails (fail-closed: a half-used code never
// grants a second chance). Codes older than pairCodeTTL are refused as
// invalid (same uniform 401 as an unknown code, per the no-oracle contract);
// the code is deleted so an attacker cannot keep retrying with it.
func (s *TokenStore) ExchangePairCode(presented string) (string, []string, error) {
	if s == nil {
		return "", nil, errors.New("kanbansvc: token store is nil")
	}
	normalized := normalizePairCode(presented)
	if normalized == "" {
		return "", nil, ErrInvalidPairCode
	}
	var grants []string
	found := false
	s.mu.Lock()
	for code, rec := range s.codes {
		// Drop expired codes silently so an attacker cannot distinguish
		// expired from unknown via timing — they all answer the same
		// uniform 401.
		if time.Since(rec.mintedAt) > pairCodeTTL {
			delete(s.codes, code)
			continue
		}
		// Constant-time compare so a local attacker timing the loop
		// learns nothing about the outstanding code (mirrors the
		// daemon pairing exchange).
		if subtle.ConstantTimeCompare([]byte(code), []byte(normalized)) == 1 {
			grants = append([]string(nil), rec.grants...)
			found = true
			delete(s.codes, code)
			break
		}
	}
	s.mu.Unlock()
	if !found {
		return "", nil, ErrInvalidPairCode
	}

	token, err := randomBearerToken()
	if err != nil {
		return "", nil, err
	}
	if err := s.Grant(token, grants...); err != nil {
		return "", nil, fmt.Errorf("exchange pairing code: %w", err)
	}
	return token, grants, nil
}

// load reads the on-disk store. Missing file => empty store. Anything that
// would make the file untrustworthy is an error, not a repair.
func (s *TokenStore) load() error {
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("kanbansvc: stat token store: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("kanbansvc: token store %s is not a regular file", s.path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		// Fail closed, same posture as the github token-file check: a
		// board-token file readable by others is refused, not used.
		return fmt.Errorf("kanbansvc: token store %s has group/other access bits; chmod it to 0600", s.path)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("kanbansvc: read token store: %w", err)
	}
	var rec tokenFileRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return fmt.Errorf("kanbansvc: decode token store %s: %w", s.path, err)
	}
	if rec.Version != tokenFileSchemaVersion {
		return fmt.Errorf("kanbansvc: token store %s has unknown schema version %d (want %d)",
			s.path, rec.Version, tokenFileSchemaVersion)
	}
	for token, grants := range rec.Tokens {
		if token == "" || len(grants) == 0 {
			return fmt.Errorf("kanbansvc: token store %s contains an empty entry", s.path)
		}
		clean, err := normalizeGrants(grants)
		if err != nil {
			return fmt.Errorf("kanbansvc: token store %s: %w", s.path, err)
		}
		s.tokens[token] = clean
	}
	return nil
}

// persist atomically rewrites the token file: snapshot under the lock, then
// temp-file + rename OUTSIDE the lock (mutex scope rule — no disk I/O held
// under grantsMu; the rename keeps readers on a complete old-or-new file).
func (s *TokenStore) persist() error {
	if s.path == "" {
		return errors.New("kanbansvc: token store has no backing path")
	}
	s.grantsMu.RLock()
	snapshot := make(map[string][]string, len(s.tokens))
	for token, grants := range s.tokens {
		snapshot[token] = append([]string(nil), grants...)
	}
	s.grantsMu.RUnlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, tokenDirPerm); err != nil { //nolint:gosec // G703: s.path is $MEEPT_HOME-derived (operator-controlled), same posture as every config root
		return fmt.Errorf("kanbansvc: create token dir: %w", err)
	}
	data, err := json.MarshalIndent(tokenFileRecord{Version: tokenFileSchemaVersion, Tokens: snapshot}, "", "  ")
	if err != nil {
		return fmt.Errorf("kanbansvc: encode token store: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tokens-*.tmp") //nolint:gosec // G703: dir is the $MEEPT_HOME token dir (operator path), leaf name fixed by CreateTemp
	if err != nil {
		return fmt.Errorf("kanbansvc: create temp token file: %w", err)
	}
	tmpName := tmp.Name()
	// Any failure after this point removes the partial temp file so the
	// directory never accumulates 0600 litter.
	if err := writeAndCloseTemp(tmp, data); err != nil {
		os.Remove(tmpName) //nolint:gosec // G703: tmpName came from CreateTemp in the operator dir
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil { //nolint:gosec // G703: both endpoints are the operator-controlled token-store path
		os.Remove(tmpName) //nolint:gosec // G703: tmpName came from CreateTemp in the operator dir
		return fmt.Errorf("kanbansvc: rename token store into place: %w", err)
	}
	return nil
}

// writeAndCloseTemp writes the payload, pins the mode to 0600 (CreateTemp
// already uses 0600; the explicit Chmod makes the contract independent of
// the stdlib default), and closes.
func writeAndCloseTemp(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("kanbansvc: write temp token file: %w", err)
	}
	if err := f.Chmod(tokenFilePerm); err != nil {
		f.Close()
		return fmt.Errorf("kanbansvc: chmod temp token file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("kanbansvc: close temp token file: %w", err)
	}
	return nil
}

// normalizeGrants validates a grant set against the C5 strings and copies it
// (caller slices are never retained). A grant outside the C5 contract is a
// caller bug and fails loudly rather than minting a dead token.
func normalizeGrants(grants []string) ([]string, error) {
	if len(grants) == 0 {
		return nil, errors.New("kanbansvc: at least one grant is required")
	}
	known := map[string]bool{GrantRead: true, GrantMutate: true, GrantTakeover: true}
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		if !known[g] {
			return nil, fmt.Errorf("kanbansvc: unknown grant %q (want one of %s, %s, %s)", g, GrantRead, GrantMutate, GrantTakeover)
		}
		out = append(out, g)
	}
	return out, nil
}

// randomPairCode mints pairCodeLen characters from crypto/rand with
// rejection sampling so every alphabet character is equiprobable (no modulo
// bias).
func randomPairCode() (string, error) {
	// 36 does not divide 256: accept bytes < 252 (36*7) and re-roll the
	// rest.
	const limit = 256 - 256%len(pairCodeAlphabet)
	out := make([]byte, 0, pairCodeLen)
	buf := make([]byte, 16)
	for len(out) < pairCodeLen {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("kanbansvc: mint pairing code: %w", err)
		}
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, pairCodeAlphabet[int(b)%len(pairCodeAlphabet)])
			if len(out) == pairCodeLen {
				break
			}
		}
	}
	return string(out), nil
}

// randomBearerToken mints a 256-bit crypto/rand token, hex-encoded (same
// convention as pkg/id and the dev key — no OS-env generation).
func randomBearerToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("kanbansvc: mint bearer token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// normalizePairCode canonicalizes a presented code: trimmed, uppercased,
// with dash/space separators stripped, so "abcd-ef01" and "ABCD EF01" both
// match a minted code (mirrors the daemon pairing normalizer).
func normalizePairCode(s string) string {
	var b strings.Builder
	for _, c := range strings.ToUpper(strings.TrimSpace(s)) {
		switch c {
		case '-', ' ', '\t':
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// ---- Sidecar bearer middleware (04/03's "sidecar middleware" half of C7) ----

// grantsContextKey keys the grant set attached to a request context by
// Middleware.
type grantsContextKey struct{}

// Middleware authenticates every request against the token store and places
// the resolved C5 grant set on the request context (readable via
// GrantsFromContext). Unknown/missing tokens are answered 401 here, exactly
// the gateway's own authorize() semantics, so a pairing-exchange route can
// hang off a grant-aware handler chain. The gateway's own REST/WS surfaces
// keep authorizing through the GrantChecker seam — this middleware is for
// handlers mounted IN FRONT OF the gateway (the 07/01 registration of
// POST /kanban/pair/exchange composes it), not a replacement for gateway
// auth.
func (s *TokenStore) Middleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		grants, known := s.Grants(token)
		if !known {
			writeJSONError(w, http.StatusUnauthorized, "unknown bearer token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), grantsContextKey{}, grants)))
	})
}

// GrantsFromContext returns the grant set Middleware attached to the request
// context. The bool distinguishes "attached, empty set" (impossible today —
// Grant rejects empty sets — but honest typing costs nothing) from "never
// authenticated".
func GrantsFromContext(ctx context.Context) ([]string, bool) {
	if ctx == nil {
		return nil, false
	}
	raw, ok := ctx.Value(grantsContextKey{}).([]string)
	if !ok {
		return nil, false
	}
	return append([]string(nil), raw...), true
}
