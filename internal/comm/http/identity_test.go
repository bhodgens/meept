package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auth"
)

// setupStoreAuth builds an auth store with two users and returns it plus
// the raw keys. The expired key carries ExpiresAt one hour in the past.
func setupStoreAuth(t *testing.T) (store *auth.Store, aliceValid, aliceExpired, bobValid string) {
	t.Helper()
	store, err := auth.NewStore(filepath.Join(t.TempDir(), "users.json5"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	alice, err := store.AddUser("alice")
	if err != nil {
		t.Fatalf("AddUser(alice): %v", err)
	}
	bob, err := store.AddUser("bob")
	if err != nil {
		t.Fatalf("AddUser(bob): %v", err)
	}
	aliceValid, err = store.AddKey(alice.ID, "laptop", nil)
	if err != nil {
		t.Fatalf("AddKey(alice): %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	aliceExpired, err = store.AddKey(alice.ID, "old", &past)
	if err != nil {
		t.Fatalf("AddKey(alice expired): %v", err)
	}
	bobValid, err = store.AddKey(bob.ID, "phone", nil)
	if err != nil {
		t.Fatalf("AddKey(bob): %v", err)
	}
	return store, aliceValid, aliceExpired, bobValid
}

// doAuthedRequest runs req through the given auth handler chain.
func doAuthedRequest(t *testing.T, h http.Handler, method, target, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, http.NoBody)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestStoreAuthMiddleware(t *testing.T) {
	store, aliceValid, aliceExpired, _ := setupStoreAuth(t)

	// Probe handler surfaces the context identity as JSON.
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFromContext(r.Context())
		if !ok {
			_, _ = w.Write([]byte(`{"identity":null}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"user_id":   id.UserID,
			"user_name": id.UserName,
			"key_id":    id.KeyID,
		})
	})
	h := NewStoreAuth(store).Middleware(probe)

	t.Run("valid key returns 200 and identity in context", func(t *testing.T) {
		w := doAuthedRequest(t, h, http.MethodGet, "/api/v1/sessions", aliceValid)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["user_name"] != "alice" || body["user_id"] == "" || body["key_id"] == "" {
			t.Errorf("identity = %v, want populated alice identity", body)
		}
	})

	t.Run("unknown key returns 418 invalid message", func(t *testing.T) {
		w := doAuthedRequest(t, h, http.MethodGet, "/api/v1/sessions", "not-a-real-key")
		if w.Code != http.StatusTeapot {
			t.Fatalf("status = %d, want 418", w.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["message"] != "invalid API key" {
			t.Errorf("message = %q, want invalid API key", body["message"])
		}
	})

	t.Run("expired key returns 418 with expiry message", func(t *testing.T) {
		w := doAuthedRequest(t, h, http.MethodGet, "/api/v1/sessions", aliceExpired)
		if w.Code != http.StatusTeapot {
			t.Fatalf("status = %d, want 418", w.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["message"] != "API key expired" {
			t.Errorf("message = %q, want API key expired", body["message"])
		}
	})

	t.Run("missing key returns 401", func(t *testing.T) {
		w := doAuthedRequest(t, h, http.MethodGet, "/api/v1/sessions", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})

	t.Run("health endpoint exempt from auth", func(t *testing.T) {
		for _, path := range []string{"/health", "/api/v1/health"} {
			w := doAuthedRequest(t, h, http.MethodGet, path, "")
			if w.Code != http.StatusOK {
				t.Errorf("GET %s status = %d, want 200 (exempt)", path, w.Code)
			}
			if _, ok := IdentityFromContext(context.Background()); ok {
				t.Error("background context unexpectedly carries identity")
			}
		}
	})

	t.Run("OPTIONS preflight exempt from auth", func(t *testing.T) {
		w := doAuthedRequest(t, h, http.MethodOptions, "/api/v1/sessions", "")
		if w.Code != http.StatusOK {
			t.Errorf("OPTIONS status = %d, want 200 (exempt)", w.Code)
		}
	})

	t.Run("websocket subprotocol key accepted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ws", http.NoBody)
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Protocol", "bearer."+aliceValid)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
		}
	})
}

func TestLegacyAPIKeyAuthUnchangedByStoreAuth(t *testing.T) {
	_, aliceValid, _, bobValid := setupStoreAuth(t)
	_ = bobValid

	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFromContext(r.Context())
		if ok && id != nil {
			t.Error("legacy middleware must not attach a multi-user identity")
		}
		if _, ok := APIKeyFromContext(r.Context()); !ok {
			t.Error("legacy middleware must still set the API key in context")
		}
	})
	h := NewAPIKeyAuth([]string{aliceValid}).Middleware(probe)

	if w := doAuthedRequest(t, h, http.MethodGet, "/api/v1/sessions", aliceValid); w.Code != http.StatusOK {
		t.Fatalf("valid legacy key rejected: status = %d", w.Code)
	}
	if w := doAuthedRequest(t, h, http.MethodGet, "/api/v1/sessions", "wrong-key"); w.Code != http.StatusTeapot {
		t.Fatalf("invalid legacy key: status = %d, want 418", w.Code)
	}
}

func TestServerMiddlewarePrefersStoreOverFlatKeys(t *testing.T) {
	store, aliceValid, _, _ := setupStoreAuth(t)

	s := NewServer(ServerConfig{
		RequireAuth: true,
		APIKeys:     []string{"flat-legacy-key"},
		AuthStore:   store,
	}, nil, nil, nil, nil, nil)

	handler := s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFromContext(r.Context())
		switch {
		case !ok:
			_, _ = w.Write([]byte(`{"auth":"none"}`))
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{"user": id.UserName})
		}
	}))

	// A store key authenticates even though flat keys are also configured —
	// store-backed validation REPLACES the legacy path in multi-user mode.
	w := doAuthedRequest(t, handler, http.MethodGet, "/api/v1/sessions", aliceValid)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"user":"alice"`) {
		t.Fatalf("store key via server middleware: status = %d body = %s", w.Code, w.Body.String())
	}

	// The flat legacy key is NOT accepted once the store is wired.
	w = doAuthedRequest(t, handler, http.MethodGet, "/api/v1/sessions", "flat-legacy-key")
	if w.Code != http.StatusTeapot {
		t.Errorf("flat key accepted while AuthStore configured: status = %d, want 418", w.Code)
	}

	// Health check passes through the full composed chain without auth.
	w = doAuthedRequest(t, handler, http.MethodGet, "/health", "")
	if w.Code != http.StatusOK {
		t.Errorf("/health through composed chain: status = %d, want 200", w.Code)
	}
}
