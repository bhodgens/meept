package kanbansvc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// newTempStore builds a TokenStore backed by a file inside t.TempDir().
func newTempStore(t *testing.T) *TokenStore {
	t.Helper()
	store, err := NewTokenStore(filepath.Join(t.TempDir(), "kanban", tokenFileName))
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	return store
}

// assertFilePerm checks the token file's mode has no group/other bits.
func assertFilePerm(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("token file %s mode %v has group/other bits, want 0600", path, info.Mode().Perm())
	}
}

func TestTokenStore_SingleUseExchange(t *testing.T) {
	store := newTempStore(t)
	code, err := store.GeneratePairCode(GrantRead, GrantMutate)
	if err != nil {
		t.Fatalf("GeneratePairCode: %v", err)
	}
	if len(code) != pairCodeLen {
		t.Fatalf("pair code %q length %d, want %d", code, len(code), pairCodeLen)
	}

	token, grants, err := store.ExchangePairCode(code)
	if err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	if len(token) != tokenBytes*2 {
		t.Fatalf("token %q length %d, want %d hex chars", token, len(token), tokenBytes*2)
	}
	if strings.Join(grants, ",") != GrantRead+","+GrantMutate {
		t.Fatalf("grants %v, want [%s %s]", grants, GrantRead, GrantMutate)
	}
	known, ok := store.Grants(token)
	if !ok || strings.Join(known, ",") != GrantRead+","+GrantMutate {
		t.Fatalf("Grants(token) = %v,%v after exchange", known, ok)
	}

	// Second exchange of the same code must fail — single use.
	if _, _, err := store.ExchangePairCode(code); !errors.Is(err, ErrInvalidPairCode) {
		t.Fatalf("second exchange err = %v, want ErrInvalidPairCode", err)
	}
}

func TestTokenStore_UnknownCodeRejected(t *testing.T) {
	store := newTempStore(t)
	if _, _, err := store.ExchangePairCode("NOPE1234"); !errors.Is(err, ErrInvalidPairCode) {
		t.Fatalf("unknown code err = %v, want ErrInvalidPairCode", err)
	}
	if _, _, err := store.ExchangePairCode("   "); !errors.Is(err, ErrInvalidPairCode) {
		t.Fatalf("blank code err = %v, want ErrInvalidPairCode", err)
	}
}

func TestTokenStore_CodeIsNormalized(t *testing.T) {
	store := newTempStore(t)
	code, err := store.GeneratePairCode(GrantRead)
	if err != nil {
		t.Fatalf("GeneratePairCode: %v", err)
	}
	mangled := strings.ToLower(code[:4]) + "-" + code[4:]
	if _, _, err := store.ExchangePairCode(mangled); err != nil {
		t.Fatalf("exchange with %q (dash + lowercase form of %q): %v", mangled, code, err)
	}
}

func TestTokenStore_GrantValidation(t *testing.T) {
	store := newTempStore(t)
	if _, err := store.GeneratePairCode("board.frobnicate"); err == nil {
		t.Fatal("GeneratePairCode with unknown grant: expected error")
	}
	if _, err := store.GeneratePairCode(); err == nil {
		t.Fatal("GeneratePairCode with no grants: expected error")
	}
	if err := store.Grant("", GrantRead); err == nil {
		t.Fatal("Grant with empty token: expected error")
	}
}

func TestTokenStore_RoundTripThroughDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kanban", tokenFileName)
	store, err := NewTokenStore(path)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	code, err := store.GeneratePairCode(GrantRead, GrantMutate, GrantTakeover)
	if err != nil {
		t.Fatalf("GeneratePairCode: %v", err)
	}
	token, _, err := store.ExchangePairCode(code)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	assertFilePerm(t, path)

	// Fresh store on the same file: the token is known with the same grants.
	reloaded, err := NewTokenStore(path)
	if err != nil {
		t.Fatalf("reload NewTokenStore: %v", err)
	}
	grants, ok := reloaded.Grants(token)
	if !ok {
		t.Fatal("reloaded store does not know the exchanged token")
	}
	if strings.Join(grants, ",") != strings.Join([]string{GrantRead, GrantMutate, GrantTakeover}, ",") {
		t.Fatalf("reloaded grants %v", grants)
	}

	// Pairing codes are in-memory only: a restart does NOT resurrect the
	// already-consumed code, and minting is per-process.
	if _, _, err := reloaded.ExchangePairCode(code); !errors.Is(err, ErrInvalidPairCode) {
		t.Fatalf("reloaded exchange of consumed code err = %v, want ErrInvalidPairCode", err)
	}

	// Revoke persists: a third load no longer knows the token.
	if err := reloaded.Revoke(token); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	third, err := NewTokenStore(path)
	if err != nil {
		t.Fatalf("third NewTokenStore: %v", err)
	}
	if _, ok := third.Grants(token); ok {
		t.Fatal("revoked token still known after reload")
	}
	// Revoke of an unknown token is a no-op success.
	if err := third.Revoke("nonexistent-token"); err != nil {
		t.Fatalf("Revoke unknown: %v", err)
	}
}

func TestTokenStore_RefusesGroupReadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, tokenFileName)
	body, err := json.Marshal(tokenFileRecord{Version: tokenFileSchemaVersion, Tokens: map[string][]string{"tok": {GrantRead}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("seed insecure file: %v", err)
	}
	if _, err := NewTokenStore(path); err == nil || !strings.Contains(err.Error(), "group/other") {
		t.Fatalf("NewTokenStore on 0644 file err = %v, want group/other refusal", err)
	}

	// Owner-only but corrupt JSON is also refused (fail closed, no silent
	// empty-store repair of a file that exists).
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	if _, err := NewTokenStore(path); err == nil {
		t.Fatal("NewTokenStore on corrupt 0600 file: expected decode error")
	}
}

func TestTokenStore_DefaultPathHonorsMeeptHome(t *testing.T) {
	t.Setenv(config.EnvMeeptHome, t.TempDir())
	if got := DefaultTokenStorePath(); got != filepath.Join(config.MeeptHome(), "kanban", tokenFileName) {
		t.Fatalf("DefaultTokenStorePath = %q", got)
	}
	store, err := NewTokenStore("")
	if err != nil {
		t.Fatalf("NewTokenStore(\"\"): %v", err)
	}
	if store.Path() != DefaultTokenStorePath() {
		t.Fatalf("store path %q, want %q", store.Path(), DefaultTokenStorePath())
	}
}

func TestTokenStore_GrantCheckerSeamWithGateway(t *testing.T) {
	// The store must drop into the SAME GrantChecker seam the gateway was
	// built against: exchanged tokens authorize, unknown ones get 401,
	// missing grants get 403.
	store := newTempStore(t)
	code, err := store.GeneratePairCode(GrantRead)
	if err != nil {
		t.Fatalf("GeneratePairCode: %v", err)
	}
	token, _, err := store.ExchangePairCode(code)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	rpc := &fakeRPC{}
	gateway, err := NewGateway(rpc, IdleSource{}, store, "")
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/board/list", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read with paired token: status %d body %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/card/create", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mutate with read-only token: status %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/board/list", nil)
	req.Header.Set("Authorization", "Bearer deadbeef")
	rec = httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token: status %d, want 401", rec.Code)
	}
}

func TestTokenStore_MiddlewareAttachesGrants(t *testing.T) {
	store := newTempStore(t)
	code, err := store.GeneratePairCode(GrantRead, GrantMutate)
	if err != nil {
		t.Fatalf("GeneratePairCode: %v", err)
	}
	token, _, err := store.ExchangePairCode(code)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	var gotGrants []string
	var gotOK bool
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotGrants, gotOK = GrantsFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/kanban/pair/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("authed request: status %d", rec.Code)
	}
	if !gotOK || strings.Join(gotGrants, ",") != GrantRead+","+GrantMutate {
		t.Fatalf("context grants = %v (ok=%v)", gotGrants, gotOK)
	}

	// Missing token: 401 before the handler runs.
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	// Unknown token: 401.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer nope")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token: status %d, want 401", rec.Code)
	}

	// GrantsFromContext on a bare (unauthenticated) context is honest
	// about absence.
	if _, ok := GrantsFromContext(context.Background()); ok {
		t.Fatal("GrantsFromContext on unauthenticated request reported grants")
	}
}

func TestTokenStore_PairCodesAreDistinct(t *testing.T) {
	store := newTempStore(t)
	seen := map[string]bool{}
	for i := range 50 {
		code, err := store.GeneratePairCode(GrantRead)
		if err != nil {
			t.Fatalf("GeneratePairCode #%d: %v", i, err)
		}
		if seen[code] {
			t.Fatalf("duplicate pair code %q", code)
		}
		seen[code] = true
	}
}

// TestTokenStore_PairCodeTTL_ExpiresRejects pins the L7 contract:
// an expired code is refused with ErrInvalidPairCode, exactly like an
// unknown code — no store-state oracle.
func TestTokenStore_PairCodeTTL_ExpiresRejects(t *testing.T) {
	store := newTempStore(t)
	code, err := store.GeneratePairCode(GrantRead)
	if err != nil {
		t.Fatalf("GeneratePairCode: %v", err)
	}

	// Advance time past the TTL to mark the code expired.
	store.mu.Lock()
	rec := store.codes[code]
	rec.mintedAt = rec.mintedAt.Add(-pairCodeTTL - time.Second)
	store.codes[code] = rec
	store.mu.Unlock()

	// Expired code: must be refused, not consumed.
	if _, _, err := store.ExchangePairCode(code); !errors.Is(err, ErrInvalidPairCode) {
		t.Fatalf("expired code exchange err = %v, want ErrInvalidPairCode", err)
	}
	// Second attempt on the same expired code: still refused (no oracle).
	if _, _, err := store.ExchangePairCode(code); !errors.Is(err, ErrInvalidPairCode) {
		t.Fatalf("second expired exchange err = %v, want ErrInvalidPairCode", err)
	}

	// Unknown code: same error, for the no-oracle contract.
	if _, _, err := store.ExchangePairCode("UNKNOWNCODE"); !errors.Is(err, ErrInvalidPairCode) {
		t.Fatalf("unknown code err = %v, want ErrInvalidPairCode", err)
	}

	// A fresh code still works.
	code2, err := store.GeneratePairCode(GrantRead)
	if err != nil {
		t.Fatalf("GeneratePairCode(fresh): %v", err)
	}
	token, grants, err := store.ExchangePairCode(code2)
	if err != nil {
		t.Fatalf("fresh code exchange: %v", err)
	}
	if len(token) < 32 {
		t.Fatalf("minted token too short: %q", token)
	}
	if !hasGrantName(grants, GrantRead) {
		t.Fatalf("grants %v lack GrantRead", grants)
	}
}

// TestTokenStore_PairCodeTTL_ValidWithinWindow proves a code minted less
// than pairCodeTTL ago is still accepted.
func TestTokenStore_PairCodeTTL_ValidWithinWindow(t *testing.T) {
	store := newTempStore(t)
	code, err := store.GeneratePairCode(GrantRead)
	if err != nil {
		t.Fatalf("GeneratePairCode: %v", err)
	}

	// Advance to just before expiry.
	store.mu.Lock()
	rec := store.codes[code]
	rec.mintedAt = rec.mintedAt.Add(-pairCodeTTL + time.Millisecond)
	store.codes[code] = rec
	store.mu.Unlock()

	token, _, err := store.ExchangePairCode(code)
	if err != nil {
		t.Fatalf("code minted < TTL ago was rejected: %v", err)
	}
	if len(token) < 32 {
		t.Fatalf("minted token too short: %q", token)
	}
}

// TestTokenStore_PairCodeTTL_DeletedAfterExpiry proves expired codes are
// removed so a replay attacker cannot keep retrying.
func TestTokenStore_PairCodeTTL_DeletedAfterExpiry(t *testing.T) {
	store := newTempStore(t)
	code, err := store.GeneratePairCode(GrantRead)
	if err != nil {
		t.Fatalf("GeneratePairCode: %v", err)
	}

	// Mark expired.
	store.mu.Lock()
	rec := store.codes[code]
	rec.mintedAt = rec.mintedAt.Add(-pairCodeTTL - time.Second)
	store.codes[code] = rec
	store.mu.Unlock()

	// First exchange deletes it.
	_, _, err = store.ExchangePairCode(code)
	if !errors.Is(err, ErrInvalidPairCode) {
		t.Fatalf("first expired exchange err = %v, want ErrInvalidPairCode", err)
	}

	// The code must no longer be in the map (replay attacker cannot retry).
	store.mu.Lock()
	_, known := store.codes[code]
	store.mu.Unlock()
	if known {
		t.Fatal("expired code still present after exchange")
	}
}
