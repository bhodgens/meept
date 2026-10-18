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
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := hermes.NewClient(srv.URL, hermesTestKey, 5_000_000_000) // 5s
	return NewHermesTool(client), srv
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
		{"chat missing session", withHermesVerb(map[string]any{"input": "hi"}, "chat"), "session_id is required"},
		{"run missing input", withHermesVerb(map[string]any{}, "run"), "input is required"},
		{"runs missing run_id", withHermesVerb(map[string]any{}, "runs"), "run_id is required"},
		{"steer missing message", withHermesVerb(map[string]any{"run_id": "r"}, "steer"), "message is required"},
		{"messages missing session", withHermesVerb(map[string]any{}, "messages"), "session_id is required"},
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

func TestHermesTool_IsReadOnlyByVerb(t *testing.T) {
	tool := NewHermesTool(nil)
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
	tool := NewHermesTool(nil)
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
	tool := NewHermesTool(nil)
	d := tool.Description()
	if !strings.Contains(strings.ToLower(d), "verify before acting") {
		t.Fatalf("description missing verify-before-acting note: %q", d)
	}
	if !strings.Contains(strings.ToLower(d), "own approval policy") {
		t.Fatalf("description missing own-approval-policy note: %q", d)
	}
}

func TestHermesTool_ToolName(t *testing.T) {
	if got := NewHermesTool(nil).Name(); got != "hermes" {
		t.Fatalf("Name() = %q, want hermes", got)
	}
}
