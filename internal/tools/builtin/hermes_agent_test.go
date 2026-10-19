package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/hermes"
)

const hermesTestKey = "test-key-do-not-leak-9f2a"

// newHermesTestTool builds a hermes tool pointed at an httptest stub server
// exercising the real leaf-01 client wire path.
func newHermesTestTool(t *testing.T, handler http.HandlerFunc) (*HermesTool, *httptest.Server) {
	t.Helper()
	return newHermesTestToolOpts(t, handler, false)
}

// newHermesTestToolAutoApprove builds the tool with hermes.auto_approve=true.
func newHermesTestToolAutoApprove(t *testing.T, handler http.HandlerFunc) (*HermesTool, *httptest.Server) {
	t.Helper()
	return newHermesTestToolOpts(t, handler, true)
}

func newHermesTestToolOpts(t *testing.T, handler http.HandlerFunc, autoApprove bool) (*HermesTool, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := hermes.NewClient(srv.URL, hermesTestKey, 5_000_000_000) // 5s
	return NewHermesTool(client, autoApprove), srv
}

// hermesAllCaps serves a /v1/capabilities document with every flag the tool
// gates on set true; false skips the named flag.
func hermesCapsBody(skip ...string) string {
	skipSet := map[string]bool{}
	for _, s := range skip {
		skipSet[s] = true
	}
	flags := []string{
		"session_chat", "run_submission", "run_steer", "run_stop",
		"run_approval_response", "session_model_lock",
	}
	parts := make([]string, 0, len(flags))
	for _, f := range flags {
		parts = append(parts, fmt.Sprintf("%q:%t", f, !skipSet[f]))
	}
	return `{"features":{` + strings.Join(parts, ",") + `},"endpoints":{}}`
}

func TestHermesTool_VerbDispatch(t *testing.T) {
	type hit struct {
		method string
		path   string
	}
	cases := []struct {
		verb      string
		args      map[string]any
		wantHits  []hit
		writeBody func(w http.ResponseWriter, r *http.Request)
		check     func(t *testing.T, res any)
	}{
		{
			verb: "status",
			args: map[string]any{},
			wantHits: []hit{
				{http.MethodGet, "/health"},
				{http.MethodGet, "/v1/capabilities"},
			},
			writeBody: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/health":
					w.WriteHeader(http.StatusOK)
				case "/v1/capabilities":
					fmt.Fprint(w, `{"features":{"chat":true},"endpoints":{"chat":{"method":"POST","path":"/api/sessions/{id}/chat"}}}`)
				}
			},
			check: func(t *testing.T, res any) {
				m, ok := res.(map[string]any)
				if !ok {
					t.Fatalf("status result type %T, want map", res)
				}
				if m["reachable"] != true {
					t.Fatalf("status reachable = %v, want true", m["reachable"])
				}
				if m["base_url"] == "" {
					t.Fatal("status payload missing base_url")
				}
			},
		},
		{
			verb: "chat",
			args: map[string]any{"session_id": "sess1", "input": "hello"},
			wantHits: []hit{
				{http.MethodPost, "/api/sessions/sess1/chat"},
			},
			writeBody: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"type":"hermes.session.chat.completion","message":{"role":"assistant","content":"hi there"}}`)
			},
			check: func(t *testing.T, res any) {
				m := res.(map[string]any)
				if m["reply"] != "hi there" {
					t.Fatalf("chat reply = %v, want 'hi there'", m["reply"])
				}
				if m["session_id"] != "sess1" {
					t.Fatalf("chat session_id = %v", m["session_id"])
				}
			},
		},
		{
			verb: "run",
			args: map[string]any{"input": "do a thing"},
			wantHits: []hit{
				{http.MethodPost, "/v1/runs"},
			},
			writeBody: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				fmt.Fprint(w, `{"run_id":"r1"}`)
			},
			check: func(t *testing.T, res any) {
				m := res.(map[string]any)
				if m["run_id"] != "r1" {
					t.Fatalf("run run_id = %v, want r1", m["run_id"])
				}
			},
		},
		{
			verb: "runs",
			args: map[string]any{"run_id": "r1"},
			wantHits: []hit{
				{http.MethodGet, "/v1/runs/r1"},
			},
			writeBody: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"run_id":"r1","status":"completed","result":"done"}`)
			},
			check: func(t *testing.T, res any) {
				m := res.(map[string]any)
				if m["status"] != "completed" || m["result"] != "done" {
					t.Fatalf("runs result = %v", m)
				}
			},
		},
		{
			verb: "steer",
			args: map[string]any{"run_id": "r1", "message": "focus on tests"},
			wantHits: []hit{
				{http.MethodPost, "/v1/runs/r1/steer"},
			},
			writeBody: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			check: func(t *testing.T, res any) {
				m := res.(map[string]any)
				if m["run_id"] != "r1" || m["steered"] != true {
					t.Fatalf("steer result = %v", m)
				}
			},
		},
		{
			verb: "stop",
			args: map[string]any{"run_id": "r1"},
			wantHits: []hit{
				{http.MethodPost, "/v1/runs/r1/stop"},
			},
			writeBody: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			check: func(t *testing.T, res any) {
				m := res.(map[string]any)
				if m["run_id"] != "r1" || m["stopped"] != true {
					t.Fatalf("stop result = %v", m)
				}
			},
		},
		{
			verb: "sessions",
			args: map[string]any{"limit": 5, "offset": 0},
			wantHits: []hit{
				{http.MethodGet, "/api/sessions"},
			},
			writeBody: func(w http.ResponseWriter, r *http.Request) {
				if q := r.URL.Query().Get("limit"); q != "5" {
					t.Errorf("sessions limit query = %q, want 5", q)
				}
				fmt.Fprint(w, `{"sessions":[{"id":"s1","title":"t"}],"total":1}`)
			},
			check: func(t *testing.T, res any) {
				m := res.(map[string]any)
				sessions, ok := m["sessions"].([]any)
				if !ok || len(sessions) != 1 {
					t.Fatalf("sessions payload = %#v", m["sessions"])
				}
				s0, ok := sessions[0].(map[string]any)
				if !ok || s0["id"] != "s1" {
					t.Fatalf("sessions[0] = %#v", sessions[0])
				}
			},
		},
		{
			verb: "messages",
			args: map[string]any{"session_id": "s1", "limit": 10},
			wantHits: []hit{
				{http.MethodGet, "/api/sessions/s1/messages"},
			},
			writeBody: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"messages":[{"role":"user","content":"hello"}]}`)
			},
			check: func(t *testing.T, res any) {
				m := res.(map[string]any)
				msgs, ok := m["messages"].([]any)
				if !ok || len(msgs) != 1 {
					t.Fatalf("messages payload = %#v", m["messages"])
				}
				m0, ok := msgs[0].(map[string]any)
				if !ok || m0["role"] != "user" {
					t.Fatalf("messages[0] = %#v", msgs[0])
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			var got []hit
			tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
				// Gated verbs probe /v1/capabilities first; serve it
				// out-of-band except for the status verb, which asserts
				// the probe as one of its own hits.
				if r.URL.Path == "/v1/capabilities" && tc.verb != "status" {
					fmt.Fprint(w, hermesCapsBody())
					return
				}
				got = append(got, hit{r.Method, r.URL.Path})
				tc.writeBody(w, r)
			})

			out, err := tool.Execute(context.Background(), withHermesVerb(tc.args, tc.verb))
			if err != nil {
				t.Fatalf("Execute(%s) error = %v", tc.verb, err)
			}
			if len(got) != len(tc.wantHits) {
				t.Fatalf("requests = %v, want %v", got, tc.wantHits)
			}
			for i, h := range tc.wantHits {
				if got[i] != h {
					t.Fatalf("request[%d] = %v, want %v", i, got[i], h)
				}
			}
			tc.check(t, hermesResultPayload(t, out))
		})
	}
}

// withHermesVerb copies args with the verb set.
func withHermesVerb(args map[string]any, verb string) map[string]any {
	out := map[string]any{"verb": verb}
	for k, v := range args {
		out[k] = v
	}
	return out
}

// hermesResultPayload unwraps the *tools.ToolResult envelope into the result map.
func hermesResultPayload(t *testing.T, out any) map[string]any {
	t.Helper()
	type envelope struct {
		Success bool           `json:"success"`
		Result  map[string]any `json:"result"`
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !env.Success {
		t.Fatalf("result not successful: %s", raw)
	}
	return env.Result
}

func TestHermesTool_InvalidVerb(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected request for invalid verb")
	})
	_, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{}, "explode"))
	if err == nil || !strings.Contains(err.Error(), "verb must be") {
		t.Fatalf("invalid verb error = %v, want verb-list message", err)
	}
}

func TestHermesTool_MissingRequiredArgs(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"chat missing input", withHermesVerb(map[string]any{"session_id": "s"}, "chat"), "input is required"},
		{"run missing input", withHermesVerb(map[string]any{}, "run"), "input is required"},
		{"runs missing run_id", withHermesVerb(map[string]any{}, "runs"), "run_id is required"},
		{"steer missing message", withHermesVerb(map[string]any{"run_id": "r"}, "steer"), "message is required"},
		{"messages missing session", withHermesVerb(map[string]any{}, "messages"), "session_id is required"},
		{"set_model missing session", withHermesVerb(map[string]any{"model": "m"}, "set_model"), "session_id is required"},
		{"set_model missing model+provider", withHermesVerb(map[string]any{"session_id": "s"}, "set_model"), "model or provider is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request for %s", tc.name)
			})
			_, err := tool.Execute(context.Background(), tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

// --- session-create / model verbs (v1-limitation extension) --------------

func TestHermesTool_ChatAutoCreatesSession(t *testing.T) {
	var got []string
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			fmt.Fprint(w, hermesCapsBody())
			return
		}
		got = append(got, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/sessions":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"object":"hermes.session","session":{"id":"api_1_new","source":"api_server"}}`)
		case "/api/sessions/api_1_new/chat":
			fmt.Fprint(w, `{"type":"hermes.session.chat.completion","session_id":"api_1_new","message":{"role":"assistant","content":"hello"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{"input": "hi"}, "chat"))
	if err != nil {
		t.Fatalf("chat auto-create: %v", err)
	}
	want := []string{"POST /api/sessions", "POST /api/sessions/api_1_new/chat"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	m := hermesResultPayload(t, out)
	if m["session_id"] != "api_1_new" {
		t.Errorf("session_id = %v, want api_1_new", m["session_id"])
	}
	if m["session_created"] != true {
		t.Errorf("session_created = %v, want true", m["session_created"])
	}
	if m["reply"] != "hello" {
		t.Errorf("reply = %v", m["reply"])
	}
}

func TestHermesTool_ChatExplicitSessionSkipsCreate(t *testing.T) {
	var hits int
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			fmt.Fprint(w, hermesCapsBody())
			return
		}
		hits++
		if r.URL.Path == "/api/sessions" {
			t.Error("explicit session_id must not create a session")
		}
		fmt.Fprint(w, `{"type":"hermes.session.chat.completion","message":{"role":"assistant","content":"ok"}}`)
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{"session_id": "s9", "input": "hi"}, "chat"))
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if hits != 1 {
		t.Fatalf("requests = %d, want 1", hits)
	}
	m := hermesResultPayload(t, out)
	if m["session_id"] != "s9" {
		t.Errorf("session_id = %v, want s9", m["session_id"])
	}
	if _, ok := m["session_created"]; ok {
		t.Errorf("session_created must be absent for an explicit session: %v", m)
	}
}

func TestHermesTool_CreateSessionVerb(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/sessions" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"object":"hermes.session","session":{"id":"s-new","title":"t","model":"glm","source":"api_server"}}`)
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
		"title": "t", "model": "glm", "provider": "z",
	}, "create_session"))
	if err != nil {
		t.Fatalf("create_session: %v", err)
	}
	m := hermesResultPayload(t, out)
	if m["session_id"] != "s-new" || m["title"] != "t" || m["model"] != "glm" {
		t.Fatalf("payload = %v", m)
	}
}

func TestHermesTool_SetModelVerb(t *testing.T) {
	var modelBody map[string]any
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			fmt.Fprint(w, `{"features":{"session_model_lock":true},"endpoints":{}}`)
		case "/api/sessions/s1/model":
			dec := json.NewDecoder(r.Body)
			_ = dec.Decode(&modelBody)
			fmt.Fprint(w, `{"object":"hermes.session.model_lock","session_id":"s1","runtime":{"provider":"z","model":"glm","route_source":"model_routes","model_lock":"accepted"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
		"session_id": "s1", "model": "glm", "provider": "z",
	}, "set_model"))
	if err != nil {
		t.Fatalf("set_model: %v", err)
	}
	m := hermesResultPayload(t, out)
	if m["model"] != "glm" || m["locked"] != true {
		t.Fatalf("payload = %v", m)
	}
	if modelBody["require_model_lock"] != true {
		t.Errorf("request body must set require_model_lock: %v", modelBody)
	}
}

func TestHermesTool_SetModelCapabilityGate(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			fmt.Fprint(w, `{"features":{"session_model_lock":false},"endpoints":{}}`)
			return
		}
		t.Errorf("no model POST expected when capability is off: %s", r.URL.Path)
	})
	_, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
		"session_id": "s1", "model": "glm",
	}, "set_model"))
	if err == nil || !strings.Contains(err.Error(), "does not advertise the session_model_lock capability") {
		t.Fatalf("gate error = %v, want capability message", err)
	}
}

func TestHermesTool_ErrorMapping_SessionExists(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"error":{"message":"Session already exists: s1","type":"invalid_request_error","code":"session_exists"}}`)
	})
	_, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{"session_id": "s1", "title": "t"}, "create_session"))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want exists message", err)
	}
	if strings.Contains(err.Error(), "another hermes surface") {
		t.Fatalf("session_exists must not map to the foreign-session message: %v", err)
	}
	if !errors.Is(err, hermes.ErrHermesSessionExists) {
		t.Fatalf("typed sentinel must survive translation, got %v", err)
	}
}

func TestHermesTool_IsReadOnlyByVerb(t *testing.T) {
	tool := NewHermesTool(nil, false)
	readOnly := []string{"status", "runs", "sessions", "messages"}
	for _, verb := range readOnly {
		if !tool.IsReadOnly(withHermesVerb(map[string]any{}, verb)) {
			t.Errorf("verb %s IsReadOnly = false, want true", verb)
		}
	}
	for _, verb := range []string{"chat", "run", "steer", "stop"} {
		if tool.IsReadOnly(withHermesVerb(map[string]any{}, verb)) {
			t.Errorf("verb %s IsReadOnly = true, want false", verb)
		}
	}
}

func TestHermesTool_DisabledNilClient(t *testing.T) {
	tool := NewHermesTool(nil, false)
	_, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{"input": "x"}, "status"))
	if err == nil || !strings.Contains(err.Error(), "hermes disabled") {
		t.Fatalf("nil-client error = %v, want 'hermes disabled'", err)
	}
}

// --- error mapping (Contract C + leaf 02 task 2) ----------------------------

func TestHermesTool_ErrorMapping_Busy(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "Too many concurrent runs (max 10)")
	})
	_, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{"session_id": "s", "input": "hi"}, "chat"))
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "hermes busy (concurrent-run cap), retry shortly") {
		t.Fatalf("busy error = %q", err.Error())
	}
	if !errors.Is(err, hermes.ErrHermesBusy) {
		t.Fatal("busy error lost typed sentinel")
	}
}

func TestHermesTool_ErrorMapping_SessionForeign(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, "permission_denied")
	})
	_, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{"session_id": "s", "input": "hi"}, "chat"))
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "session belongs to another hermes surface") ||
		!strings.Contains(err.Error(), "create a new session") {
		t.Fatalf("foreign-session error = %q", err.Error())
	}
	if !errors.Is(err, hermes.ErrHermesSessionForeign) {
		t.Fatal("foreign-session error lost typed sentinel")
	}
}

func TestHermesTool_ErrorMapping_Auth(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "unauthorized")
	})
	_, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{}, "status"))
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "check HERMES_API_SERVER_KEY") {
		t.Fatalf("auth error = %q", err.Error())
	}
	if !errors.Is(err, hermes.ErrHermesAuth) {
		t.Fatal("auth error lost typed sentinel")
	}
}

func TestHermesTool_ErrorMapping_APIErrorWrappedWithBaseURL(t *testing.T) {
	tool, srv := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "boom")
	})
	_, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{"input": "x"}, "run"))
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("APIError not wrapped with base URL %s: %q", srv.URL, err.Error())
	}
}

func TestHermesTool_NeverEmitsAPIKey(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		// Upstream echo attack: body contains our own key.
		fmt.Fprintf(w, "bad key %s", hermesTestKey)
	})
	_, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{"session_id": "s", "input": "hi"}, "chat"))
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), hermesTestKey) {
		t.Fatalf("API key leaked in error: %q", err.Error())
	}
}

func TestHermesTool_DescriptionCarriesVerifyNote(t *testing.T) {
	tool := NewHermesTool(nil, false)
	d := tool.Description()
	if !strings.Contains(strings.ToLower(d), "verify before acting") {
		t.Fatalf("description missing verify-before-acting note: %q", d)
	}
	if !strings.Contains(strings.ToLower(d), "own approval policy") {
		t.Fatalf("description missing own-approval-policy note: %q", d)
	}
}

func TestHermesTool_ToolName(t *testing.T) {
	if got := NewHermesTool(nil, false).Name(); got != "hermes" {
		t.Fatalf("Name() = %q, want hermes", got)
	}
}

// --- p2-03: approve verb (two-phase confirmation + auto-approve) -------------

func TestHermesTool_ApproveConfirmationFirst(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			fmt.Fprint(w, hermesCapsBody())
			return
		}
		t.Errorf("no approval POST expected before confirmation: %s %s", r.Method, r.URL.Path)
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
		"run_id": "r1", "decision": "approve", "reason": "looks safe",
	}, "approve"))
	if err != nil {
		t.Fatalf("approve phase 1: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("phase-1 result type %T, want raw confirmation map", out)
	}
	if !IsConfirmationRequest(m) {
		t.Fatalf("result is not a confirmation request: %v", m)
	}
	if m["confirm_arg"] != "confirmed" {
		t.Errorf("confirm_arg = %v, want confirmed", m["confirm_arg"])
	}
	details, _ := m["details"].(map[string]any)
	if details == nil || details["run_id"] != "r1" || details["decision"] != "approve" {
		t.Errorf("details = %v, want run_id+decision preview", details)
	}
}

func TestHermesTool_ApproveConfirmedForwards(t *testing.T) {
	var body map[string]any
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			fmt.Fprint(w, hermesCapsBody())
		case "/v1/runs/r1/approval":
			if r.Method != http.MethodPost {
				t.Errorf("approval method = %s", r.Method)
			}
			dec := json.NewDecoder(r.Body)
			_ = dec.Decode(&body)
			fmt.Fprint(w, `{"run_id":"r1","choice":"once","resolved":1}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
		"run_id": "r1", "decision": "approve", "reason": "ok", "confirmed": true,
	}, "approve"))
	if err != nil {
		t.Fatalf("approve phase 2: %v", err)
	}
	if body["choice"] != "once" {
		t.Errorf("wire choice = %v, want once (approve alias)", body["choice"])
	}
	if body["reason"] != "ok" {
		t.Errorf("wire reason = %v, want ok", body["reason"])
	}
	m := hermesResultPayload(t, out)
	if m["run_id"] != "r1" || m["sent"] != true {
		t.Fatalf("payload = %v", m)
	}
}

func TestHermesTool_ApproveAutoApproveForwardsDirectly(t *testing.T) {
	var hits int
	tool, _ := newHermesTestToolAutoApprove(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			fmt.Fprint(w, hermesCapsBody())
		case "/v1/runs/r9/approval":
			hits++
			fmt.Fprint(w, `{"run_id":"r9","choice":"deny","resolved":1}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
		"run_id": "r9", "decision": "deny",
	}, "approve"))
	if err != nil {
		t.Fatalf("auto-approve: %v", err)
	}
	if hits != 1 {
		t.Fatalf("approval POSTs = %d, want 1 (no confirmation phase)", hits)
	}
	if m, ok := out.(map[string]any); ok && IsConfirmationRequest(m) {
		t.Fatal("auto_approve=true must not return a confirmation request")
	}
	m := hermesResultPayload(t, out)
	if m["run_id"] != "r9" || m["sent"] != true {
		t.Fatalf("payload = %v", m)
	}
}

func TestHermesTool_ApproveCapabilityGate(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			fmt.Fprint(w, hermesCapsBody("run_approval_response"))
			return
		}
		t.Errorf("no approval POST expected when capability is off: %s", r.URL.Path)
	})
	_, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
		"run_id": "r1", "decision": "approve", "confirmed": true,
	}, "approve"))
	if err == nil || !strings.Contains(err.Error(), "does not advertise the run_approval_response capability") {
		t.Fatalf("gate error = %v", err)
	}
}

// --- p2-03: jobs verb ----------------------------------------------------------

func TestHermesTool_JobsList(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/jobs" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"jobs":[{"id":"j1","name":"nightly","prompt":"p","schedule":{"kind":"cron","expr":"0 0 * * *","display":"daily at midnight"},"state":"scheduled","enabled":true}]}`)
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{"action": "list"}, "jobs"))
	if err != nil {
		t.Fatalf("jobs list: %v", err)
	}
	m := hermesResultPayload(t, out)
	jobs, ok := m["jobs"].([]any)
	if !ok || len(jobs) != 1 {
		t.Fatalf("jobs payload = %#v", m["jobs"])
	}
	j0 := jobs[0].(map[string]any)
	if j0["id"] != "j1" || j0["name"] != "nightly" || j0["schedule"] != "daily at midnight" || j0["status"] != "scheduled" {
		t.Fatalf("jobs[0] = %#v", j0)
	}
}

func TestHermesTool_JobsGet(t *testing.T) {
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/jobs/j2" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"job":{"id":"j2","name":"n","prompt":"p","schedule":"30m","state":"paused","enabled":false}}`)
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{"action": "get", "job_id": "j2"}, "jobs"))
	if err != nil {
		t.Fatalf("jobs get: %v", err)
	}
	m := hermesResultPayload(t, out)
	if m["id"] != "j2" || m["status"] != "paused" || m["enabled"] != false {
		t.Fatalf("payload = %v", m)
	}
}

func TestHermesTool_JobsCreate(t *testing.T) {
	var body map[string]any
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/jobs" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		dec := json.NewDecoder(r.Body)
		_ = dec.Decode(&body)
		fmt.Fprint(w, `{"job":{"id":"abc123def456","name":"daily","prompt":"do it","schedule":"every day at 9am","state":"scheduled","enabled":true}}`)
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
		"action": "create", "name": "daily", "schedule": "every day at 9am",
		"prompt": "do it", "repeat_times": 3,
	}, "jobs"))
	if err != nil {
		t.Fatalf("jobs create: %v", err)
	}
	if body["name"] != "daily" || body["schedule"] != "every day at 9am" || body["prompt"] != "do it" {
		t.Errorf("wire body = %v", body)
	}
	if body["repeat"] != float64(3) {
		t.Errorf("wire repeat = %v, want 3", body["repeat"])
	}
	m := hermesResultPayload(t, out)
	if m["id"] != "abc123def456" || m["name"] != "daily" {
		t.Fatalf("payload = %v", m)
	}
}

func TestHermesTool_JobsMutations(t *testing.T) {
	for _, action := range []string{"pause", "resume", "run"} {
		t.Run(action, func(t *testing.T) {
			tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/jobs/j3/"+action {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				fmt.Fprint(w, `{"job":{"id":"j3","name":"n","prompt":"p","schedule":"5m","state":"scheduled","enabled":true}}`)
			})
			out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
				"action": action, "job_id": "j3",
			}, "jobs"))
			if err != nil {
				t.Fatalf("jobs %s: %v", action, err)
			}
			m := hermesResultPayload(t, out)
			if m["job_id"] != "j3" || m[action] != true {
				t.Fatalf("payload = %v", m)
			}
		})
	}
}

func TestHermesTool_JobsValidation(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing action", withHermesVerb(map[string]any{}, "jobs"), "action must be"},
		{"bad action", withHermesVerb(map[string]any{"action": "delete"}, "jobs"), "action must be"},
		{"get missing job_id", withHermesVerb(map[string]any{"action": "get"}, "jobs"), "job_id is required"},
		{"pause missing job_id", withHermesVerb(map[string]any{"action": "pause"}, "jobs"), "job_id is required"},
		{"create missing name", withHermesVerb(map[string]any{"action": "create", "schedule": "5m", "prompt": "p"}, "jobs"), "name, schedule, and prompt are required"},
		{"create missing schedule", withHermesVerb(map[string]any{"action": "create", "name": "n", "prompt": "p"}, "jobs"), "name, schedule, and prompt are required"},
		{"create missing prompt", withHermesVerb(map[string]any{"action": "create", "name": "n", "schedule": "5m"}, "jobs"), "name, schedule, and prompt are required"},
		{"approve missing run_id", withHermesVerb(map[string]any{"decision": "approve"}, "approve"), "run_id is required"},
		{"approve missing decision", withHermesVerb(map[string]any{"run_id": "r"}, "approve"), "decision must be"},
		{"approve bad decision", withHermesVerb(map[string]any{"run_id": "r", "decision": "maybe"}, "approve"), "decision must be"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request for %s: %s", tc.name, r.URL.Path)
			})
			_, err := tool.Execute(context.Background(), tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

// --- p2-03: session_key arg + chat auto-create title ---------------------------

func TestHermesTool_SessionKeyHeaderOnChatAndRun(t *testing.T) {
	gotKeys := map[string]string{}
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		gotKeys[r.URL.Path] = r.Header.Get("X-Hermes-Session-Key")
		switch r.URL.Path {
		case "/v1/capabilities":
			fmt.Fprint(w, hermesCapsBody())
		case "/api/sessions/s1/chat":
			fmt.Fprint(w, `{"type":"hermes.session.chat.completion","message":{"role":"assistant","content":"ok"}}`)
		case "/v1/runs":
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"run_id":"r1"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	if _, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
		"session_id": "s1", "input": "hi", "session_key": "scope-1",
	}, "chat")); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if _, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
		"input": "hi", "session_key": "scope-2",
	}, "run")); err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotKeys["/api/sessions/s1/chat"] != "scope-1" {
		t.Errorf("chat session-key header = %q, want scope-1", gotKeys["/api/sessions/s1/chat"])
	}
	if gotKeys["/v1/runs"] != "scope-2" {
		t.Errorf("run session-key header = %q, want scope-2", gotKeys["/v1/runs"])
	}
}

func TestHermesTool_ChatAutoCreateTitleFromInput(t *testing.T) {
	long := strings.Repeat("word ", 20) // 100 chars
	var createBody map[string]any
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			fmt.Fprint(w, hermesCapsBody())
		case "/api/sessions":
			dec := json.NewDecoder(r.Body)
			_ = dec.Decode(&createBody)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"object":"hermes.session","session":{"id":"s-new"}}`)
		case "/api/sessions/s-new/chat":
			fmt.Fprint(w, `{"type":"hermes.session.chat.completion","message":{"role":"assistant","content":"ok"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{"input": long}, "chat"))
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	title, _ := createBody["title"].(string)
	if len([]rune(title)) > 40 || title == "" {
		t.Fatalf("create title = %q, want non-empty prefix <= 40 chars", title)
	}
	if !strings.HasPrefix(long, title) {
		t.Fatalf("title %q is not a prefix of the input", title)
	}
	m := hermesResultPayload(t, out)
	if m["created_session_id"] != "s-new" {
		t.Errorf("created_session_id = %v, want s-new", m["created_session_id"])
	}
}

// --- p2-03: capability-gating matrix --------------------------------------------

func TestHermesTool_CapabilityGatingMatrix(t *testing.T) {
	cases := []struct {
		verb    string
		feature string
		args    map[string]any
		path    string // action route that must NOT be hit
	}{
		{"chat", "session_chat", map[string]any{"session_id": "s1", "input": "hi"}, "/api/sessions/s1/chat"},
		{"run", "run_submission", map[string]any{"input": "hi"}, "/v1/runs"},
		{"steer", "run_steer", map[string]any{"run_id": "r1", "message": "m"}, "/v1/runs/r1/steer"},
		{"stop", "run_stop", map[string]any{"run_id": "r1"}, "/v1/runs/r1/stop"},
	}
	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/capabilities" {
					fmt.Fprint(w, hermesCapsBody(tc.feature))
					return
				}
				t.Errorf("%s must not hit %s when %s is off", tc.verb, r.URL.Path, tc.feature)
			})
			_, err := tool.Execute(context.Background(), withHermesVerb(tc.args, tc.verb))
			if err == nil || !strings.Contains(err.Error(), "does not advertise the "+tc.feature+" capability") {
				t.Fatalf("gate error = %v, want %q message", err, tc.feature)
			}
		})
	}
}

func TestHermesTool_GateFailsOpenOnProbeError(t *testing.T) {
	// Capabilities probe fails (500) but the action route works: the gate
	// must not brick the verb (matches the landed set_model precedent).
	tool, _ := newHermesTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "boom")
		case "/v1/runs/r1/steer":
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	out, err := tool.Execute(context.Background(), withHermesVerb(map[string]any{
		"run_id": "r1", "message": "m",
	}, "steer"))
	if err != nil {
		t.Fatalf("steer with failed probe: %v", err)
	}
	m := hermesResultPayload(t, out)
	if m["steered"] != true {
		t.Fatalf("payload = %v", m)
	}
}

func TestHermesTool_ReadOnlyApproveJobs(t *testing.T) {
	tool := NewHermesTool(nil, false)
	for _, verb := range []string{"approve", "jobs"} {
		if tool.IsReadOnly(withHermesVerb(map[string]any{}, verb)) {
			t.Errorf("verb %s IsReadOnly = true, want false (conservative)", verb)
		}
	}
}
