//go:build e2e

// Suite hermes-client, session-model capability: session creation and
// per-session model changes exercised from OUTSIDE the packages over their
// exported surfaces, against an httptest stub speaking the real upstream
// shapes (gateway/platforms/api_server.py _handle_create_session /
// _handle_session_model_lock / _handle_list_sessions, verified 2026-10-19).
//
// Pins the removal of the v1 "chat REQUIRES an explicit session_id"
// limitation: the `hermes` tool auto-creates a session when session_id is
// empty, `create_session` mints one directly, `set_model` changes a
// session's model gated on the session_model_lock capability flag, and a
// 409 code=session_exists classifies distinctly from the never-retry
// foreign-session 409.
package hermesclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/hermes"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// sessionModelStub is a stateful session-aware stub: POST /api/sessions
// inserts a row keyed by id (explicit or minted), and chat / model / 409
// behaviors read that table. It answers in the shapes upstream actually
// uses ({"object":"hermes.session","session":{...}}, {"object":"list",
// "data":[...]}).
type sessionModelStub struct {
	mu       sync.Mutex
	sessions map[string]map[string]any // id -> session row
	modelSet map[string]string         // id -> locked model
	capsOn   bool                      // session_model_lock feature flag
	hits     []string
}

func newSessionModelStub(t *testing.T) (*sessionModelStub, *hermes.Client) {
	t.Helper()
	g := &sessionModelStub{
		sessions: map[string]map[string]any{},
		modelSet: map[string]string{},
		capsOn:   true,
	}
	mux := http.NewServeMux()

	record := func(what string) {
		g.mu.Lock()
		g.hits = append(g.hits, what)
		g.mu.Unlock()
	}

	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		record("capabilities")
		features := map[string]bool{"session_chat": true, "session_model_lock": g.flag()}
		fmt.Fprintf(w, `{"features":%s,"endpoints":{}}`, mustJSON(features))
	})

	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			record("list-sessions")
			g.mu.Lock()
			rows := make([]any, 0, len(g.sessions))
			for _, s := range g.sessions {
				rows = append(rows, s)
			}
			g.mu.Unlock()
			// Upstream list shape: {"object":"list","data":[...]}.
			fmt.Fprintf(w, `{"object":"list","data":%s,"limit":50,"offset":0,"has_more":false}`, mustJSON(rows))
			return
		}
		record("create-session")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		id, _ := body["id"].(string)
		if id == "" {
			id = fmt.Sprintf("api_minted_%d", len(g.sessions)+1)
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if _, exists := g.sessions[id]; exists {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":{"message":"Session already exists: `+id+`","type":"invalid_request_error","code":"session_exists"}}`)
			return
		}
		model, _ := body["model"].(string)
		row := map[string]any{"id": id, "source": "api_server", "model": model, "started_at": 1760000000.0}
		if title, ok := body["title"].(string); ok && title != "" {
			row["title"] = title
		}
		if lock, _ := body["require_model_lock"].(bool); lock && model == "unroutable" {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":{"message":"Requested Browser model lock cannot be routed; refusing silent global fallback","type":"invalid_request_error","code":"model_lock_unavailable"}}`)
			return
		}
		g.sessions[id] = row
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"object":"hermes.session","session":%s}`, mustJSON(row))
	})

	// /api/sessions/{id}/chat and /api/sessions/{id}/model via prefix.
	mux.HandleFunc("/api/sessions/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
		switch {
		case strings.HasSuffix(rest, "/chat"):
			id := strings.TrimSuffix(rest, "/chat")
			record("chat:" + id)
			g.mu.Lock()
			_, known := g.sessions[id]
			g.mu.Unlock()
			if id == "foreign" {
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"error":{"message":"permission_denied","code":"permission_denied"}}`)
				return
			}
			if !known {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":{"message":"Session not found"}}`)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			reply := "ack " + fmt.Sprint(body["input"])
			if m, ok := body["model"].(string); ok && m != "" {
				reply += " via " + m
			}
			fmt.Fprintf(w, `{"object":"hermes.session.chat.completion","type":"hermes.session.chat.completion","session_id":%s,"message":{"role":"assistant","content":%s},"usage":{"input_tokens":3,"output_tokens":4}}`,
				mustJSON(id), mustJSON(reply))
		case strings.HasSuffix(rest, "/model"):
			id := strings.TrimSuffix(rest, "/model")
			record("model:" + id)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			lock, _ := body["require_model_lock"].(bool)
			if !lock {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":{"message":"missing require_model_lock"}}`)
				return
			}
			model, _ := body["model"].(string)
			provider, _ := body["provider"].(string)
			g.mu.Lock()
			g.modelSet[id] = model
			_, known := g.sessions[id]
			g.mu.Unlock()
			if !known {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":{"message":"Session not found"}}`)
				return
			}
			fmt.Fprintf(w, `{"object":"hermes.session.model_lock","session_id":%s,"runtime":{"provider":%s,"model":%s,"route_source":"model_routes","model_lock":"accepted"}}`,
				mustJSON(id), mustJSON(provider), mustJSON(model))
		default:
			record("other:" + rest)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return g, hermes.NewClient(srv.URL, "e2e-gw-key", 5*time.Second)
}

func (g *sessionModelStub) flag() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.capsOn
}

func (g *sessionModelStub) setFlag(on bool) {
	g.mu.Lock()
	g.capsOn = on
	g.mu.Unlock()
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (g *sessionModelStub) hitCount(substr string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, h := range g.hits {
		if strings.Contains(h, substr) {
			n++
		}
	}
	return n
}

// --- client layer -----------------------------------------------------------

func TestHermesSessionModel_CreateSession_E2E(t *testing.T) {
	g, c := newSessionModelStub(t)
	ctx := context.Background()

	// Auto-minted id (no explicit id in opts).
	detail, err := c.CreateSession(ctx, hermes.CreateSessionOptions{Title: "e2e"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if detail.ID == "" {
		t.Fatal("201 response must carry the minted session id")
	}

	// Explicit id + model + lock rides the require_model_lock body field.
	d2, err := c.CreateSession(ctx, hermes.CreateSessionOptions{
		ID: "mine", Model: "glm-5.3", Provider: "zai", RequireModelLock: true,
	})
	if err != nil || d2.ID != "mine" || d2.Model != "glm-5.3" {
		t.Fatalf("explicit create: %v %+v", err, d2)
	}

	// Re-using a taken id: 409 code=session_exists -> ErrHermesSessionExists,
	// NOT the never-retry foreign sentinel.
	_, err = c.CreateSession(ctx, hermes.CreateSessionOptions{ID: "mine"})
	if !errors.Is(err, hermes.ErrHermesSessionExists) {
		t.Fatalf("session_exists must classify ErrHermesSessionExists, got %v", err)
	}
	if errors.Is(err, hermes.ErrHermesSessionForeign) {
		t.Fatalf("session_exists must not classify as foreign-session: %v", err)
	}

	// Locked model the gateway cannot route: model_lock_unavailable is a
	// DIFFERENT 409 family — it reuses the conflict sentinel (pinned
	// contract) but must never be mistaken for "id taken".
	_, err = c.CreateSession(ctx, hermes.CreateSessionOptions{ID: "x2", Model: "unroutable", RequireModelLock: true})
	if errors.Is(err, hermes.ErrHermesSessionExists) {
		t.Fatalf("model_lock_unavailable must not classify as session_exists, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "model_lock_unavailable") {
		t.Fatalf("error text must carry the upstream code, got %v", err)
	}
	_ = g
}

func TestHermesSessionModel_SetModel_E2E(t *testing.T) {
	g, c := newSessionModelStub(t)
	ctx := context.Background()

	d, err := c.CreateSession(ctx, hermes.CreateSessionOptions{ID: "lockme"})
	if err != nil || d.ID != "lockme" {
		t.Fatalf("seed session: %v", err)
	}
	res, err := c.SetSessionModel(ctx, "lockme", "zai", "glm-5.3")
	if err != nil {
		t.Fatalf("set model: %v", err)
	}
	if res.SessionID != "lockme" || res.Runtime.Model != "glm-5.3" || res.Runtime.ModelLock != "accepted" {
		t.Fatalf("lock ack = %+v", res)
	}
	g.mu.Lock()
	got := g.modelSet["lockme"]
	g.mu.Unlock()
	if got != "glm-5.3" {
		t.Fatalf("stub state: model = %q, want glm-5.3", got)
	}
}

func TestHermesSessionModel_ListMessages_UpstreamDataShape_E2E(t *testing.T) {
	g, c := newSessionModelStub(t)
	ctx := context.Background()
	if _, err := c.CreateSession(ctx, hermes.CreateSessionOptions{ID: "shaped", Model: "glm"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// The stub answers the list in upstream's {"object":"list","data":[...]}
	// shape; the client must decode it (the leaf-01 {"sessions":[...]} shape
	// also stays accepted — dual decode).
	list, err := c.ListSessions(ctx, 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, s := range list {
		if s.ID == "shaped" {
			found = true
			if s.Model != "glm" || s.Created() == "" {
				t.Fatalf("row = %+v, want model + started_at passthrough", s)
			}
		}
	}
	if !found {
		t.Fatalf("shaped session missing from %#v", list)
	}
	if g.hitCount("list-sessions") != 1 {
		t.Fatalf("expected exactly one GET /api/sessions")
	}
}

// --- tool layer (the user-facing capability) --------------------------------

func executeHermesTool(t *testing.T, tool *builtin.HermesTool, args map[string]any) (map[string]any, error) {
	t.Helper()
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		return nil, err
	}
	raw, merr := json.Marshal(out)
	if merr != nil {
		t.Fatalf("marshal: %v", merr)
	}
	var env struct {
		Success bool           `json:"success"`
		Result  map[string]any `json:"result"`
		Error   string         `json:"error"`
	}
	if uerr := json.Unmarshal(raw, &env); uerr != nil {
		t.Fatalf("unmarshal %s: %v", raw, uerr)
	}
	if !env.Success {
		return env.Result, fmt.Errorf("tool result unsuccessful: %s", env.Error)
	}
	return env.Result, nil
}

// TestHermesToolChatAutoCreatesSession_E2E is the headline pin of the v1
// limitation removal: chat with NO session_id creates a session on the
// gateway and runs the turn on it.
func TestHermesToolChatAutoCreatesSession_E2E(t *testing.T) {
	g, c := newSessionModelStub(t)
	tool := builtin.NewHermesTool(c, false)

	res, err := executeHermesTool(t, tool, map[string]any{
		"verb": "chat", "input": "hello hermes",
	})
	if err != nil {
		t.Fatalf("chat auto-create: %v", err)
	}
	sid, _ := res["session_id"].(string)
	if sid == "" {
		t.Fatalf("payload must carry the auto-created session id: %v", res)
	}
	if res["session_created"] != true {
		t.Fatalf("session_created flag missing: %v", res)
	}
	if !strings.HasPrefix(sid, "api_minted_") {
		t.Fatalf("session id %q should be the gateway-minted one", sid)
	}
	if reply, _ := res["reply"].(string); !strings.Contains(reply, "hello hermes") {
		t.Fatalf("reply = %q", reply)
	}
	if g.hitCount("create-session") != 1 || g.hitCount("chat:"+sid) != 1 {
		t.Fatalf("expected create+chat once, hits = %v", g.hits)
	}

	// Follow-up turn on the SAME id must NOT create again.
	if _, err := executeHermesTool(t, tool, map[string]any{
		"verb": "chat", "input": "again", "session_id": sid,
	}); err != nil {
		t.Fatalf("second chat: %v", err)
	}
	if g.hitCount("create-session") != 1 {
		t.Fatalf("explicit session chat must not create: hits = %v", g.hits)
	}
}

func TestHermesToolCreateAndSetModelVerbs_E2E(t *testing.T) {
	_, c := newSessionModelStub(t)
	tool := builtin.NewHermesTool(c, false)

	// create_session verb: explicit id + model locks against routing.
	res, err := executeHermesTool(t, tool, map[string]any{
		"verb": "create_session", "session_id": "e2e-sess",
		"title": "e2e", "model": "glm-5.3", "provider": "zai",
	})
	if err != nil {
		t.Fatalf("create_session: %v", err)
	}
	if res["session_id"] != "e2e-sess" || res["model"] != "glm-5.3" {
		t.Fatalf("payload = %v", res)
	}

	// set_model verb: capability on -> accepted lock.
	res, err = executeHermesTool(t, tool, map[string]any{
		"verb": "set_model", "session_id": "e2e-sess", "model": "qwen-max", "provider": "qwen",
	})
	if err != nil {
		t.Fatalf("set_model: %v", err)
	}
	if res["model"] != "qwen-max" || res["locked"] != true {
		t.Fatalf("lock payload = %v", res)
	}

	// Capability OFF -> readable gate error, and NO model POST leaves.
	// Fresh tool + fresh stub so the capabilities cache (60s TTL) is cold.
	g2, c2 := newSessionModelStub(t)
	g2.setFlag(false)
	tool2 := builtin.NewHermesTool(c2, false)
	_, err = executeHermesTool(t, tool2, map[string]any{
		"verb": "set_model", "session_id": "whatever", "model": "m",
	})
	if err == nil || !strings.Contains(err.Error(), "does not advertise the session_model_lock capability") {
		t.Fatalf("gate error = %v", err)
	}
	if g2.hitCount("model:") != 0 {
		t.Fatalf("capability gate must not call the model route: hits = %v", g2.hits)
	}
}

func TestHermesToolSessionExistsNeverRetriedAsForeign_E2E(t *testing.T) {
	_, c := newSessionModelStub(t)
	tool := builtin.NewHermesTool(c, false)
	ctx := context.Background()
	_ = ctx

	if _, err := c.CreateSession(ctx, hermes.CreateSessionOptions{ID: "dup"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Duplicate explicit id via the tool: the message must tell the model
	// to pick another id, NOT "belongs to another surface — never retry".
	_, err := executeHermesTool(t, tool, map[string]any{
		"verb": "create_session", "session_id": "dup",
	})
	if err == nil {
		t.Fatal("want duplicate-id error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want retry-with-another-id guidance", err)
	}
	if strings.Contains(err.Error(), "another hermes surface") {
		t.Fatalf("session_exists must not map to the never-retry message: %v", err)
	}
	if !errors.Is(err, hermes.ErrHermesSessionExists) {
		t.Fatalf("typed sentinel must survive tool translation: %v", err)
	}

	// A foreign-owned session chat keeps the never-retry message intact.
	_, err = executeHermesTool(t, tool, map[string]any{
		"verb": "chat", "session_id": "foreign", "input": "hi",
	})
	if err == nil || !strings.Contains(err.Error(), "another hermes surface") {
		t.Fatalf("foreign 409 error = %v", err)
	}
}
