package kanbansvc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gorillaws "github.com/gorilla/websocket"
)

// recordedCall is one forward the gateway made to the daemon.
type recordedCall struct {
	method string
	params any
}

// fakeRPC implements RPCCaller and records every forward so the REST-to-RPC
// map and the param shape are both asserted, not just the status code.
type fakeRPC struct {
	calls []recordedCall
	err   error // when set, every Call fails with it
}

func (f *fakeRPC) Call(method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, recordedCall{method: method, params: params})
	if f.err != nil {
		return nil, f.err
	}
	return json.RawMessage(`{"status":"ok"}`), nil
}
func (f *fakeRPC) Connect() error { return nil }
func (f *fakeRPC) Close() error   { return nil }

// newTestGateway builds a gateway over one grant store. readToken holds only
// board.read; mutateToken holds read+mutate; takeoverToken adds takeover;
// takeoverOnlyToken holds ONLY board.takeover. The grant ladder is what the
// tests exercise, so a route must fail for a token that lacks its specific
// grant — and takeover must fail for a token that lacks either of its two
// required grants.
func newTestGateway(t *testing.T, rpc *fakeRPC, staticDir string) (*Gateway, *StaticGrantStore) {
	t.Helper()
	authz := NewStaticGrantStore()
	authz.Grant("read-token", GrantRead)
	authz.Grant("mutate-token", GrantRead, GrantMutate)
	authz.Grant("takeover-token", GrantRead, GrantMutate, GrantTakeover)
	authz.Grant("takeover-only-token", GrantTakeover)
	gateway, err := NewGateway(rpc, IdleSource{}, authz, staticDir)
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	return gateway, authz
}

// TestNewGateway_RejectsNilDeps: a missing dependency is a boot-time
// misconfiguration, and the gateway must refuse to build rather than serve
// routes that would fall through to nil derefs or unauthenticated access.
func TestNewGateway_RejectsNilDeps(t *testing.T) {
	rpc := &fakeRPC{}
	authz := NewStaticGrantStore()
	if _, err := NewGateway(nil, IdleSource{}, authz, ""); err == nil {
		t.Error("NewGateway with nil rpc returned no error")
	}
	if _, err := NewGateway(rpc, nil, authz, ""); err == nil {
		t.Error("NewGateway with nil event source returned no error")
	}
	if _, err := NewGateway(rpc, IdleSource{}, nil, ""); err == nil {
		t.Error("NewGateway with nil grant checker returned no error")
	}
}

// TestGateway_EveryRouteForwardsToItsRPC walks the REST-to-C4 map and proves
// each path reaches the daemon method the route table names, with the right
// grant. The grant token is the highest one so a failure here is always the
// route table, never the authz fixture.
func TestGateway_EveryRouteForwardsToItsRPC(t *testing.T) {
	for path, spec := range restRoutes {
		t.Run(path+" "+spec.verb, func(t *testing.T) {
			rpc := &fakeRPC{}
			gateway, _ := newTestGateway(t, rpc, "")

			req := httptest.NewRequest(spec.verb, path, strings.NewReader(`{"id":"x"}`))
			req.Header.Set("Authorization", "Bearer takeover-token")
			rec := httptest.NewRecorder()
			gateway.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
			}
			if len(rpc.calls) != 1 {
				t.Fatalf("rpc forwards = %d, want exactly 1", len(rpc.calls))
			}
			if rpc.calls[0].method != spec.rpc {
				t.Errorf("forwarded %q, want %q", rpc.calls[0].method, spec.rpc)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}

// TestGateway_GrantLadder is the C5 contract: reads need board.read,
// mutations board.mutate, and takeover needs BOTH board.mutate AND
// board.takeover (a token holding only one of the two is refused). The
// child's version of this test asserted that reads with NO token return
// 200 — that is not a board, it is an open edit surface, so the pin now
// runs the other way.
func TestGateway_GrantLadder(t *testing.T) {
	tests := []struct {
		name   string
		verb   string
		path   string
		token  string // "" = send no Authorization header at all
		want   int
		wantOK bool // also assert a forward happened
	}{
		{"read without token is refused", http.MethodGet, "/api/v1/board/list", "", http.StatusUnauthorized, false},
		{"read with board.read", http.MethodGet, "/api/v1/board/list", "read-token", http.StatusOK, true},
		{"mutate with only board.read", http.MethodPost, "/api/v1/card/move", "read-token", http.StatusForbidden, false},
		{"mutate with board.mutate", http.MethodPost, "/api/v1/card/move", "mutate-token", http.StatusOK, true},
		{"takeover with board.mutate only", http.MethodPost, "/api/v1/card/takeover", "mutate-token", http.StatusForbidden, false},
		{"takeover with board.takeover only", http.MethodPost, "/api/v1/card/takeover", "takeover-only-token", http.StatusForbidden, false},
		{"takeover with board.mutate and board.takeover", http.MethodPost, "/api/v1/card/takeover", "takeover-token", http.StatusOK, true},
		{"unknown token is 401, not 403", http.MethodGet, "/api/v1/card/get?card_id=1", "never-issued", http.StatusUnauthorized, false},
		{"empty bearer is refused", http.MethodGet, "/api/v1/board/list", " ", http.StatusUnauthorized, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc := &fakeRPC{}
			gateway, _ := newTestGateway(t, rpc, "")

			req := httptest.NewRequest(tt.verb, tt.path, nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()
			gateway.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.want, rec.Body.String())
			}
			if got := len(rpc.calls); got != boolToInt(tt.wantOK) {
				t.Errorf("rpc forwards = %d, want %d", got, boolToInt(tt.wantOK))
			}
		})
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestGateway_WrongVerbIs405 proves a POST to a read route is a method error,
// not a missing route, and advertises the verb it accepts.
func TestGateway_WrongVerbIs405(t *testing.T) {
	gateway, _ := newTestGateway(t, &fakeRPC{}, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/board/list", nil)
	req.Header.Set("Authorization", "Bearer read-token")
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
		t.Errorf("Allow = %q, want %q", allow, http.MethodGet)
	}
}

// TestGateway_UnknownAPIPathIs404 keeps an unregistered /api/v1/ route from
// silently reaching the daemon.
func TestGateway_UnknownAPIPathIs404(t *testing.T) {
	rpc := &fakeRPC{}
	gateway, _ := newTestGateway(t, rpc, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/card/does-not-exist", nil)
	req.Header.Set("Authorization", "Bearer read-token")
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if len(rpc.calls) != 0 {
		t.Errorf("unknown path reached the daemon: %v", rpc.calls)
	}
}

// TestGateway_ErrorBodiesAreValidJSON pins the writeJSONError fix. The first
// pass hand-assembled error bodies, and the 403 body shipped an unterminated
// string the moment a grant name landed in it. Every error path must return
// parseable JSON so the browser client can read the reason.
func TestGateway_ErrorBodiesAreValidJSON(t *testing.T) {
	tests := []struct {
		name  string
		verb  string
		path  string
		token string
	}{
		{"401", http.MethodGet, "/api/v1/board/list", ""},
		{"403 names a grant", http.MethodPost, "/api/v1/card/takeover", "read-token"},
		{"405", http.MethodPost, "/api/v1/board/list", "read-token"},
		{"404", http.MethodGet, "/api/v1/nope/nope", "read-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway, _ := newTestGateway(t, &fakeRPC{}, "")
			req := httptest.NewRequest(tt.verb, tt.path, nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()
			gateway.ServeHTTP(rec, req)

			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("error body is not valid JSON: %v (body %q)", err, rec.Body.String())
			}
			if body["error"] == "" {
				t.Errorf("error body has no error field: %q", rec.Body.String())
			}
			// The 403 must name a grant it refused, or an operator
			// debugging a permission failure has nothing to act on. A
			// read-only token on takeover is missing board.mutate first
			// (route grants are checked in order: mutate, then takeover).
			if tt.name == "403 names a grant" && !strings.Contains(body["error"], GrantMutate) {
				t.Errorf("403 body %q does not name the refused grant %q", body["error"], GrantMutate)
			}
		})
	}
}

// TestGateway_ParamShapes: reads forward the query string as a string map,
// mutations forward the decoded JSON body untouched.
func TestGateway_ParamShapes(t *testing.T) {
	t.Run("read forwards query params", func(t *testing.T) {
		rpc := &fakeRPC{}
		gateway, _ := newTestGateway(t, rpc, "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/card/list?column_id=7&limit=10", nil)
		req.Header.Set("Authorization", "Bearer read-token")
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)

		if len(rpc.calls) != 1 {
			t.Fatalf("forwards = %d, want 1", len(rpc.calls))
		}
		params, ok := rpc.calls[0].params.(map[string]string)
		if !ok {
			t.Fatalf("params type = %T, want map[string]string", rpc.calls[0].params)
		}
		if params["column_id"] != "7" || params["limit"] != "10" {
			t.Errorf("params = %v, want column_id=7 limit=10", params)
		}
	})

	t.Run("mutation forwards body", func(t *testing.T) {
		rpc := &fakeRPC{}
		gateway, _ := newTestGateway(t, rpc, "")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/card/move",
			strings.NewReader(`{"card_id":"c1","column_id":"c2","order":3}`))
		req.Header.Set("Authorization", "Bearer mutate-token")
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)

		if len(rpc.calls) != 1 {
			t.Fatalf("forwards = %d, want 1", len(rpc.calls))
		}
		params, ok := rpc.calls[0].params.(map[string]any)
		if !ok {
			t.Fatalf("params type = %T, want a decoded JSON object", rpc.calls[0].params)
		}
		if params["card_id"] != "c1" {
			t.Errorf("card_id = %v, want c1", params["card_id"])
		}
	})

	t.Run("mutation with empty body is legal", func(t *testing.T) {
		rpc := &fakeRPC{}
		gateway, _ := newTestGateway(t, rpc, "")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/reconcile/now", nil)
		req.Header.Set("Authorization", "Bearer mutate-token")
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
	})

	t.Run("malformed body is 400 and never reaches the daemon", func(t *testing.T) {
		rpc := &fakeRPC{}
		gateway, _ := newTestGateway(t, rpc, "")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/card/move", strings.NewReader(`{"card_id":`))
		req.Header.Set("Authorization", "Bearer mutate-token")
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if len(rpc.calls) != 0 {
			t.Errorf("malformed body reached the daemon: %v", rpc.calls)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("400 body is not valid JSON: %v", err)
		}
	})
}

// TestGateway_RPCErrorMapping: an unknown method or missing record is the
// client's problem (404); anything else means the daemon failed (502).
func TestGateway_RPCErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"unknown method", errors.New("unknown method kanban.card.get"), http.StatusNotFound},
		{"no handler", errors.New("no handler registered"), http.StatusNotFound},
		{"missing record", errors.New("card not found"), http.StatusNotFound},
		{"daemon blew up", errors.New("w: database is locked"), http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc := &fakeRPC{err: tt.err}
			gateway, _ := newTestGateway(t, rpc, "")
			req := httptest.NewRequest(http.MethodGet, "/api/v1/card/get?card_id=1", nil)
			req.Header.Set("Authorization", "Bearer read-token")
			rec := httptest.NewRecorder()
			gateway.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("error body is not valid JSON: %v (body %q)", err, rec.Body.String())
			}
			if body["rpc"] == "" {
				t.Errorf("upstream error body does not name the rpc: %q", rec.Body.String())
			}
		})
	}
}

// TestGateway_Static: no bundle answers 503 with parseable JSON instead of a
// bare-text 503, /web canonicalises to /web/, and a present bundle serves its
// files.
func TestGateway_Static(t *testing.T) {
	t.Run("no bundle is 503", func(t *testing.T) {
		gateway, _ := newTestGateway(t, &fakeRPC{}, "")
		if gateway.HasStaticBundle() {
			t.Fatal("HasStaticBundle true with an empty staticDir")
		}
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/web/main.dart.js", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("503 body is not valid JSON: %v (body %q)", err, rec.Body.String())
		}
	})

	t.Run("index explains the missing bundle", func(t *testing.T) {
		gateway, _ := newTestGateway(t, &fakeRPC{}, "")
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "not installed") {
			t.Errorf("index body does not say the bundle is missing: %q", rec.Body.String())
		}
	})

	t.Run("serves files and canonicalises /web", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>board</h1>"), 0o600); err != nil {
			t.Fatalf("fixture write: %v", err)
		}
		gateway, _ := newTestGateway(t, &fakeRPC{}, dir)
		if !gateway.HasStaticBundle() {
			t.Fatal("HasStaticBundle false with a real bundle dir")
		}

		// http.FileServer 301-redirects /index.html to ./, so the
		// canonical directory request is the one clients make.
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/web/", nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "board") {
			t.Fatalf("bundle index: status %d body %q", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		gateway.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/web", nil))
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/web/" {
			t.Fatalf("/web canonicalisation: status %d location %q", rec.Code, rec.Header().Get("Location"))
		}

		rec = httptest.NewRecorder()
		gateway.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/web/" {
			t.Fatalf("index redirect: status %d location %q", rec.Code, rec.Header().Get("Location"))
		}
	})

	// http.FileServer owns traversal rejection; this proves the gateway did
	// not replace it with a hand-built request that could bypass it.
	t.Run("path traversal is rejected", func(t *testing.T) {
		dir := t.TempDir()
		outside := filepath.Join(dir, "..", "secret.txt")
		if err := os.WriteFile(outside, []byte("do-not-serve"), 0o600); err != nil {
			t.Fatalf("fixture write: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(outside) })

		gateway, _ := newTestGateway(t, &fakeRPC{}, dir)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/web/../../"+filepath.Base(outside), nil)
		req.URL.Path = "/web/../../" + filepath.Base(outside)
		gateway.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "do-not-serve") {
			t.Fatalf("served a file outside the bundle root: %s", rec.Body.String())
		}
	})
}

// TestPickSubprotocol pins the browser fix. A client that offered
// "bearer.<token>" gets no Sec-WebSocket-Protocol response header unless the
// server echoes an offered protocol, and the browser aborts the upgrade
// without one — the first pass echoed nothing.
func TestPickSubprotocol(t *testing.T) {
	tests := []struct {
		name    string
		offered string
		want    string
	}{
		{"bearer lane wins", "graphql-v1-ws, bearer.tok123", "bearer.tok123"},
		{"first offered when no bearer", "graphql-v1-ws, chat", "graphql-v1-ws"},
		{"nothing offered", "", ""},
		{"spaces are trimmed", "  bearer.tok  ", "bearer.tok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
			if tt.offered != "" {
				req.Header.Set("Sec-WebSocket-Protocol", tt.offered)
			}
			if got := pickSubprotocol(req); got != tt.want {
				t.Errorf("pickSubprotocol(%q) = %q, want %q", tt.offered, got, tt.want)
			}
		})
	}
}

// TestGateway_WS_Upgrade requires the token BEFORE the upgrade completes and
// proves a successful upgrade echoes the offered subprotocol back, which is
// what makes the browser keep the connection open.
func TestGateway_WS(t *testing.T) {
	server := httptest.NewServer(mustGateway(t, &fakeRPC{}, ""))
	defer server.Close()

	t.Run("no token never upgrades", func(t *testing.T) {
		conn, resp, err := dialWS(server)
		if err == nil {
			conn.Close()
			t.Fatal("handshake succeeded with no bearer token")
		}
		if resp != nil && resp.StatusCode == http.StatusOK {
			t.Errorf("refused handshake still answered 200")
		}
	})

	t.Run("read grant upgrades and echoes the subprotocol", func(t *testing.T) {
		conn, resp, err := dialWS(server, "bearer.read-token")
		if err != nil {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("handshake failed with a board.read token (status %d): %v", status, err)
		}
		defer conn.Close()
		if got := conn.Subprotocol(); got != "bearer.read-token" {
			t.Errorf("echoed subprotocol = %q, want bearer.read-token (a browser aborts on a missing echo)", got)
		}
	})

	t.Run("a token the checker does not know never upgrades", func(t *testing.T) {
		conn, _, err := dialWS(server, "bearer.never-issued")
		if err == nil {
			conn.Close()
			t.Fatal("handshake succeeded with a bearer lane the checker does not know")
		}
	})
}

// TestGateway_WS_RejectsForeignOrigin mirrors the daemon hub's allow-list.
func TestGateway_WS_RejectsForeignOrigin(t *testing.T) {
	server := httptest.NewServer(mustGateway(t, &fakeRPC{}, ""))
	defer server.Close()

	conn, _, err := dialWSWithOrigin(server, "http://evil.example", "bearer.read-token")
	if err == nil {
		conn.Close()
		t.Fatal("handshake succeeded from a foreign origin")
	}
}

func mustGateway(t *testing.T, rpc *fakeRPC, staticDir string) http.Handler {
	t.Helper()
	gateway, _ := newTestGateway(t, rpc, staticDir)
	return gateway
}

// wsTarget turns the httptest http:// host into the ws:// URL a client dials.
func wsTarget(server *httptest.Server, path string) *url.URL {
	target, err := url.Parse(server.URL)
	if err != nil {
		panic(err)
	}
	target.Scheme = "ws"
	target.Path = path
	return target
}

// dialWS dials the events endpoint with gorilla (the test-side client;
// gateway.go serves x/net on the other end). subprotocols are offered as-is,
// so passing "bearer.<token>" mirrors what the browser client sends.
// A refused handshake returns a non-nil *http.Response with the error.
func dialWS(server *httptest.Server, subprotocols ...string) (*gorillaws.Conn, *http.Response, error) {
	return dialWSWithOrigin(server, "", subprotocols...)
}

func dialWSWithOrigin(server *httptest.Server, origin string, subprotocols ...string) (*gorillaws.Conn, *http.Response, error) {
	dialer := gorillaws.DefaultDialer
	dialer.Subprotocols = subprotocols
	var header http.Header
	if origin != "" {
		// gorilla derives Origin from the URL unless the request header
		// overrides it — that override is how a foreign origin is faked.
		header = http.Header{"Origin": []string{origin}}
	}
	conn, resp, err := dialer.Dial(wsTarget(server, "/api/v1/events").String(), header)
	return conn, resp, err
}

// newPairGateway builds a gateway armed with a real TokenStore backed by a
// temp-dir token file, plus two minted pairing codes: readCode (board.read)
// and readMutateCode (board.read + board.mutate).
func newPairGateway(t *testing.T) (*Gateway, *TokenStore, string, string) {
	t.Helper()
	store, err := NewTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	gateway, err := NewGateway(&fakeRPC{}, IdleSource{}, store, "")
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	if err := gateway.SetPairing(store); err != nil {
		t.Fatalf("SetPairing: %v", err)
	}
	readCode, err := store.GeneratePairCode(GrantRead)
	if err != nil {
		t.Fatalf("GeneratePairCode(read): %v", err)
	}
	readMutateCode, err := store.GeneratePairCode(GrantRead, GrantMutate)
	if err != nil {
		t.Fatalf("GeneratePairCode(read+mutate): %v", err)
	}
	return gateway, store, readCode, readMutateCode
}

// exchange sends one pair/exchange POST and returns the status + body. An
// empty code sends NO body (the decode-failure shape), not {"code":""}.
func exchange(t *testing.T, gateway *Gateway, code string) (int, map[string]any) {
	t.Helper()
	var req *http.Request
	if code == "" {
		req = httptest.NewRequest(http.MethodPost, "/api/v1/pair/exchange", nil)
	} else {
		req = httptest.NewRequest(http.MethodPost, "/api/v1/pair/exchange",
			strings.NewReader(`{"code":"`+code+`"}`))
	}
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	var parsed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("pair/exchange body is not valid JSON: %v (body %q)", err, rec.Body.String())
	}
	return rec.Code, parsed
}

// TestGateway_PairExchange pins the pairing wiring: the route is reachable
// WITHOUT a bearer token (the code is the credential), a good code mints a
// token + grants, a wrong code is 401, the minted token authorizes a granted
// REST route, and the code is single-use.
func TestGateway_PairExchange(t *testing.T) {
	t.Run("good code returns 200 with token and grants", func(t *testing.T) {
		gateway, _, readCode, _ := newPairGateway(t)
		status, body := exchange(t, gateway, readCode)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %v)", status, body)
		}
		token, _ := body["token"].(string)
		if len(token) < 32 {
			t.Fatalf("minted token too short: %q", token)
		}
		grants, _ := body["grants"].([]any)
		if len(grants) != 1 || grants[0] != GrantRead {
			t.Fatalf("grants = %v, want [%s]", grants, GrantRead)
		}
	})

	t.Run("wrong code is 401 and names no store state", func(t *testing.T) {
		gateway, _, readCode, _ := newPairGateway(t)
		// Burn the real code first so this probes the consumed shape,
		// then a never-minted code; both must be indistinguishable 401s.
		if status, _ := exchange(t, gateway, readCode); status != http.StatusOK {
			t.Fatalf("pre-burn exchange: status = %d, want 200", status)
		}
		for _, code := range []string{readCode, "ZZZZZZZZ"} {
			status, body := exchange(t, gateway, code)
			if status != http.StatusUnauthorized {
				t.Fatalf("code %q: status = %d, want 401 (body %v)", code, status, body)
			}
			if msg, _ := body["error"].(string); msg == "" {
				t.Fatalf("401 body has no error field: %v", body)
			}
		}
	})

	t.Run("empty body is 400", func(t *testing.T) {
		gateway, _, _, _ := newPairGateway(t)
		status, _ := exchange(t, gateway, "")
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", status)
		}
	})

	t.Run("route needs no bearer token", func(t *testing.T) {
		// exchange() never sets an Authorization header; the good-code
		// case above already proves this, but pin the 405 verb error
		// path too: a GET (with no token anywhere) must surface the
		// method error, not an auth error.
		gateway, _, _, _ := newPairGateway(t)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/pair/exchange", nil)
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET pair/exchange: status = %d, want 405", rec.Code)
		}
		if allow := rec.Header().Get("Allow"); allow != http.MethodPost {
			t.Errorf("Allow = %q, want %q", allow, http.MethodPost)
		}
	})

	t.Run("minted token authorizes a granted route", func(t *testing.T) {
		rpc := &fakeRPC{}
		// The wired shape: the SAME TokenStore backs authz and pairing,
		// so a mint is instantly visible to authorize.
		store, err := NewTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
		if err != nil {
			t.Fatalf("NewTokenStore: %v", err)
		}
		gateway, err := NewGateway(rpc, IdleSource{}, store, "")
		if err != nil {
			t.Fatalf("NewGateway: %v", err)
		}
		if err := gateway.SetPairing(store); err != nil {
			t.Fatalf("SetPairing: %v", err)
		}
		code, err := store.GeneratePairCode(GrantRead, GrantMutate)
		if err != nil {
			t.Fatalf("GeneratePairCode: %v", err)
		}
		_, body := exchange(t, gateway, code)
		token, _ := body["token"].(string)
		if token == "" {
			t.Fatalf("no token minted: %v", body)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/card/move",
			strings.NewReader(`{"card_id":"c1","column_id":"c2"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("exchange-minted token on card/move: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		if len(rpc.calls) != 1 {
			t.Errorf("rpc forwards = %d, want 1", len(rpc.calls))
		}
	})

	t.Run("read-only mint cannot mutate", func(t *testing.T) {
		gateway, _, readCode, _ := newPairGateway(t)
		_, body := exchange(t, gateway, readCode)
		token, _ := body["token"].(string)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/card/move", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("read-only mint on card/move: status = %d, want 403", rec.Code)
		}
	})

	t.Run("code reuse is 401 and the token survives", func(t *testing.T) {
		gateway, _, readCode, _ := newPairGateway(t)
		_, first := exchange(t, gateway, readCode)
		token, _ := first["token"].(string)

		status, _ := exchange(t, gateway, readCode)
		if status != http.StatusUnauthorized {
			t.Fatalf("code reuse: status = %d, want 401", status)
		}

		// Single-use means the CODE dies, not the token it minted: the
		// first exchange's token keeps working.
		req := httptest.NewRequest(http.MethodGet, "/api/v1/board/list", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("minted token after code reuse: status = %d, want 200", rec.Code)
		}
	})

	t.Run("exchange-minted token reaches the WS handshake", func(t *testing.T) {
		gateway, _, readCode, _ := newPairGateway(t)
		_, body := exchange(t, gateway, readCode)
		token, _ := body["token"].(string)
		server := httptest.NewServer(gateway)
		defer server.Close()
		conn, _, err := dialWS(server, "bearer."+token)
		if err != nil {
			t.Fatalf("WS handshake with exchange-minted token: %v", err)
		}
		conn.Close()
	})

	t.Run("disarmed gateway answers 503 on the pair route", func(t *testing.T) {
		gateway, _ := newTestGateway(t, &fakeRPC{}, "")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/pair/exchange",
			strings.NewReader(`{"code":"ABCDEFGHIJ"}`))
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("disarmed pair route: status = %d, want 503", rec.Code)
		}
	})
}

// TestGateway_SetPairing_NilStoreRejected: arming the route with no store
// is a wiring bug, not a legitimate disarmed state.
func TestGateway_SetPairing_NilStoreRejected(t *testing.T) {
	gateway, _ := newTestGateway(t, &fakeRPC{}, "")
	if err := gateway.SetPairing(nil); err == nil {
		t.Fatal("SetPairing(nil) returned no error")
	}
}

// TestIdleSource closes its channel when the caller's context ends, which is
// what stops a relay from outliving its connection.
func TestIdleSource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out, err := IdleSource{}.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	select {
	case msg, ok := <-out:
		t.Fatalf("IdleSource yielded %v (ok=%v) before cancel", msg, ok)
	default:
	}
	cancel()
	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("IdleSource yielded after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("IdleSource did not close its channel on context cancel")
	}
}
