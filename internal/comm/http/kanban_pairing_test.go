package http

// Tests for POST /kanban/pair/exchange (kanban-board follow-up gap:
// literal mux hook). The dev-key pairing precedent's posture is pinned:
// loopback-only, code-as-credential (no bearer), no oracle for the
// failure shape, secrets never in the response on failure, 4KB body cap.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubKanbanExchanger struct {
	token  string
	grants []string
	err    error
	codes  []string // presented codes, in order
}

func (s *stubKanbanExchanger) Exchange(code string) (string, []string, error) {
	s.codes = append(s.codes, code)
	if s.err != nil {
		return "", nil, s.err
	}
	return s.token, s.grants, nil
}

func newKanbanPairHandler(t *testing.T, exchanger KanbanPairExchanger) http.Handler {
	t.Helper()
	if exchanger == nil {
		exchanger = &stubKanbanExchanger{token: "tok-123", grants: []string{"board.read"}}
	}
	return &kanbanPairingHandler{exchanger: exchanger, logger: discardLogger()}
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

type exchangeResp struct {
	Token   string   `json:"token"`
	Grants  []string `json:"grants"`
	Error   string   `json:"error"`
	Message string   `json:"message"`
}

func doKanbanExchange(t *testing.T, h http.Handler, remoteAddr string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, KanbanPairExchangePath, bytes.NewBufferString(body))
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestKanbanPairExchange_PinnedPath pins the documented literal path —
// keep in sync with the mux registration and docs/workflows/kanban.md.
func TestKanbanPairExchange_PinnedPath(t *testing.T) {
	if KanbanPairExchangePath != "/kanban/pair/exchange" {
		t.Fatalf("path drifted: %q", KanbanPairExchangePath)
	}
}

func TestKanbanPairExchange_Success(t *testing.T) {
	ex := &stubKanbanExchanger{token: "tok-123", grants: []string{"board.read", "board.mutate"}}
	h := newKanbanPairHandler(t, ex)

	w := doKanbanExchange(t, h, "127.0.0.1:51234", `{"code":"ABCD-EFGH"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body)
	}
	var got exchangeResp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if got.Token != "tok-123" || len(got.Grants) != 2 {
		t.Fatalf("reply = %+v, want the token + both grants", got)
	}
	if len(ex.codes) != 1 || ex.codes[0] != "ABCD-EFGH" {
		t.Fatalf("exchanger saw %v, want the presented code", ex.codes)
	}
	// The response must NOT echo the code back.
	if strings.Contains(w.Body.String(), "ABCD") {
		t.Fatalf("reply echoes the pairing code: %s", w.Body)
	}
}

func TestKanbanPairExchange_LoopbackOnly(t *testing.T) {
	h := newKanbanPairHandler(t, nil)
	for _, remote := range []string{"10.0.0.5:1234", "[::ffff:10.0.0.5]:1", "example.com:80"} {
		w := doKanbanExchange(t, h, remote, `{"code":"ABCD-EFGH"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("remote %q: status = %d, want 403 loopback refusal", remote, w.Code)
		}
	}
	// localhost resolves via the name check too.
	w := doKanbanExchange(t, h, "127.0.0.1:9999", `{"code":"x"}`)
	if w.Code == http.StatusForbidden {
		t.Fatalf("loopback caller got 403: %s", w.Body)
	}
}

func TestKanbanPairExchange_NoOracleOnFailure(t *testing.T) {
	// Wrong, consumed, expired, missing: ONE uniform 401 shape (the
	// exchanger collapses all four into ErrInvalidPairCode-style errors;
	// the handler must not widen the answer).
	ex := &stubKanbanExchanger{err: errors.New("kanban pair exchange: invalid or already-used code")}
	h := newKanbanPairHandler(t, ex)

	bodies := []string{`{"code":"WRONG-CODE"}`, `{"code":""}`, `{}`}
	var shapes []string
	for _, body := range bodies {
		w := doKanbanExchange(t, h, "127.0.0.1:1", body)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("body %q: status = %d, want the uniform 401", body, w.Code)
		}
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("body %q: reply not JSON: %s", body, w.Body)
		}
		if _, hasToken := got["token"]; hasToken {
			t.Fatalf("failure reply carries a token key: %s", w.Body)
		}
		shapes = append(shapes, fmt.Sprintf("%d|%s", w.Code, w.Body.String()))
	}
	// Malformed JSON is the precedent's 400 (a transport-shape answer,
	// not store state) — checked separately from the oracle uniformity.
	w := doKanbanExchange(t, h, "127.0.0.1:1", "not json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON: status = %d, want 400", w.Code)
	}
	for _, s := range shapes[1:] {
		if s != shapes[0] {
			t.Fatalf("failure answer is an oracle: %q vs %q (bodies leak store state)", shapes[0], s)
		}
	}
}

func TestKanbanPairExchange_MethodAndBodyCap(t *testing.T) {
	h := newKanbanPairHandler(t, nil)

	req := httptest.NewRequest(http.MethodGet, KanbanPairExchangePath, http.NoBody)
	req.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET: status %d Allow %q, want 405 + Allow: POST", w.Code, w.Header().Get("Allow"))
	}

	// Oversized body: the 4KB LimitReader truncates, so the JSON decode
	// fails as a 400 — never a hang or an OOM-sized read.
	huge := `{"code":"` + strings.Repeat("A", 8192) + `"}`
	w = doKanbanExchange(t, h, "127.0.0.1:1", huge)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusUnauthorized {
		t.Fatalf("huge body: status %d, want a bounded 400/401 answer", w.Code)
	}
}

func TestKanbanPairExchange_WithOptionNilSafe(t *testing.T) {
	s := &Server{}
	WithKanbanPairing(nil, nil)(s)
	if s.kanbanPairing != nil {
		t.Fatal("WithKanbanPairing(nil) armed the handler — the disabled boot must stay inert")
	}
	WithKanbanPairing(&stubKanbanExchanger{}, nil)(s)
	if s.kanbanPairing == nil {
		t.Fatal("WithKanbanPairing(exchanger) did not arm the handler")
	}
}
