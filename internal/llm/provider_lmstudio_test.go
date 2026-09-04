package llm

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// lmStudioFixtureServer serves a standard /v1/models list plus an
// /api/v0/models metadata payload on one httptest server, mirroring LM
// Studio's two-endpoint discovery surface (tree 05 leaf 03).
func lmStudioFixtureServer(t *testing.T, v1Body string, v1Status int, v0Body string, v0Status int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if v1Status != http.StatusOK {
			w.WriteHeader(v1Status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v1Body))
	})
	mux.HandleFunc("/api/v0/models", func(w http.ResponseWriter, r *http.Request) {
		if v0Status != http.StatusOK {
			w.WriteHeader(v0Status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v0Body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestFetchLMStudioContexts_ListParse: /v1/models ids become
// lmstudio/<sanitized-id> keys; context is 0 when no metadata exists.
func TestFetchLMStudioContexts_ListParse(t *testing.T) {
	v1 := `{"data":[{"id":"qwen2.5-7b-instruct"},{"id":"llama-3.2-3b"}]}`
	srv := lmStudioFixtureServer(t, v1, http.StatusOK,
		`{"data":[]}`, http.StatusOK)

	got, err := FetchLMStudioContexts(context.Background(), srv.Client(), slog.Default(), srv.URL, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d keys, want 2: %v", len(got), got)
	}
	for _, key := range []string{"lmstudio/qwen2.5-7b-instruct", "lmstudio/llama-3.2-3b"} {
		v, ok := got[key]
		if !ok {
			t.Errorf("missing key %q in %v", key, got)
			continue
		}
		if v != 0 {
			t.Errorf("%s context = %d, want 0 (no metadata)", key, v)
		}
	}
}

// TestFetchLMStudioContexts_MetadataMerge: /api/v0/models metadata merges
// by id; the loaded instance's context_length WINS over max_context_length
// when both exist; absence is tolerated (leaf Task 2).
func TestFetchLMStudioContexts_MetadataMerge(t *testing.T) {
	tests := []struct {
		name      string
		v0Entry   string
		wantCtx   int
		wantCtxOK bool
	}{
		{
			// Both fields present: loaded instance wins.
			name: "loaded instance beats max_context_length",
			v0Entry: `{"id":"qwen2.5-7b-instruct","max_context_length":32768,
				"loaded_instances":[{"config":{"context_length":8192}}]}`,
			wantCtx:   8192,
			wantCtxOK: true,
		},
		{
			// Only max present: max wins.
			name:      "max_context_length used when nothing loaded",
			v0Entry:   `{"id":"qwen2.5-7b-instruct","max_context_length":32768}`,
			wantCtx:   32768,
			wantCtxOK: true,
		},
		{
			// Neither present: tolerated, context stays 0.
			name:      "no metadata fields tolerated",
			v0Entry:   `{"id":"qwen2.5-7b-instruct"}`,
			wantCtx:   0,
			wantCtxOK: true,
		},
		{
			// Loaded instance with a zero context falls back to max.
			name: "zero loaded context falls back to max",
			v0Entry: `{"id":"qwen2.5-7b-instruct","max_context_length":32768,
				"loaded_instances":[{"config":{"context_length":0}}]}`,
			wantCtx:   32768,
			wantCtxOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1 := `{"data":[{"id":"qwen2.5-7b-instruct"}]}`
			srv := lmStudioFixtureServer(t, v1, http.StatusOK,
				`{"data":[`+tt.v0Entry+`]}`, http.StatusOK)

			got, err := FetchLMStudioContexts(context.Background(), srv.Client(), slog.Default(), srv.URL, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			v, ok := got["lmstudio/qwen2.5-7b-instruct"]
			if !ok {
				t.Fatalf("missing key in %v", got)
			}
			if v != tt.wantCtx {
				t.Errorf("context = %d, want %d", v, tt.wantCtx)
			}
			_ = tt.wantCtxOK // presence asserted above via two-value form
		})
	}
}

// TestFetchLMStudioContexts_V0EntryWithoutV1ID: ids come from the OpenAI
// list; a v0 metadata entry with no matching /v1/models id is ignored.
func TestFetchLMStudioContexts_V0EntryWithoutV1ID(t *testing.T) {
	v1 := `{"data":[{"id":"qwen2.5-7b-instruct"}]}`
	v0 := `{"data":[{"id":"ghost-model","max_context_length":4096}]}`
	srv := lmStudioFixtureServer(t, v1, http.StatusOK, v0, http.StatusOK)

	got, err := FetchLMStudioContexts(context.Background(), srv.Client(), slog.Default(), srv.URL, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got["lmstudio/ghost-model"]; ok {
		t.Error("v0-only id must not be registered")
	}
	if _, ok := got["lmstudio/qwen2.5-7b-instruct"]; !ok {
		t.Errorf("v1 id missing from %v", got)
	}
}

// TestFetchLMStudioContexts_HostileIDs: ids that are empty, contain "../",
// whitespace, control chars, or characters outside [a-z0-9-_.] are skipped
// and never registered. Uppercase ids are lowercased first (the OpenAI
// list can carry uppercase; the charset target matches localModelKey).
func TestFetchLMStudioContexts_HostileIDs(t *testing.T) {
	v1 := `{"data":[
		{"id":"../etc/passwd"},
		{"id":"has space"},
		{"id":"tab\tid"},
		{"id":"Ünïcode"},
		{"id":""},
		{"id":"Valid-ID_1.0"}
	]}`
	srv := lmStudioFixtureServer(t, v1, http.StatusOK, `{"data":[]}`, http.StatusOK)

	got, err := FetchLMStudioContexts(context.Background(), srv.Client(), slog.Default(), srv.URL, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d keys, want 1 (only the sanitized id): %v", len(got), got)
	}
	if _, ok := got["lmstudio/valid-id_1.0"]; !ok {
		t.Errorf("sanitized id missing from %v", got)
	}
	for hostile := range got {
		if hostile != "lmstudio/valid-id_1.0" {
			t.Errorf("hostile id registered as %q", hostile)
		}
	}
}

// TestSanitizeLMStudioModelID pins the validation rules directly
// (table-driven; leaf Task 2 "id sanitization").
func TestSanitizeLMStudioModelID(t *testing.T) {
	tests := []struct {
		in   string
		want string // "" = rejected
	}{
		{"", ""},
		{"../etc/passwd", ""},
		{"..", ""},         // charset ok but traversal prefix pattern: ".." itself is a path segment
		{"foo/../bar", ""}, // contains "../"
		{"has space", ""},
		{"tab\tid", ""},
		{"nl\nid", ""},
		{"Ünïcode", ""},
		{"Qwen2.5-7B-Instruct", "qwen2.5-7b-instruct"},
		{"valid-id_1.0", "valid-id_1.0"},
		{"llama-3.2-3b", "llama-3.2-3b"},
	}
	for _, tt := range tests {
		if got := sanitizeLMStudioModelID(tt.in); got != tt.want {
			t.Errorf("sanitizeLMStudioModelID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestFetchLMStudioContexts_ServerErrorTolerated: server down, non-2xx,
// and malformed bodies yield an empty (or partial) result with NO error —
// discovery never breaks the caller (leaf Task 2 tolerance rules).
func TestFetchLMStudioContexts_ServerErrorTolerated(t *testing.T) {
	t.Run("v1 list returns 500", func(t *testing.T) {
		srv := lmStudioFixtureServer(t, "", http.StatusInternalServerError, "", http.StatusOK)
		got, err := FetchLMStudioContexts(context.Background(), srv.Client(), slog.Default(), srv.URL, "")
		if err != nil {
			t.Fatalf("non-2xx must not error, got %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty result, got %v", got)
		}
	})

	t.Run("v1 list malformed JSON", func(t *testing.T) {
		srv := lmStudioFixtureServer(t, `{not-json`, http.StatusOK, "", http.StatusOK)
		got, err := FetchLMStudioContexts(context.Background(), srv.Client(), slog.Default(), srv.URL, "")
		if err != nil {
			t.Fatalf("malformed body must not error, got %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty result, got %v", got)
		}
	})

	t.Run("server unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		srv.Close() // port now dead
		got, err := FetchLMStudioContexts(context.Background(), srv.Client(), slog.Default(), srv.URL, "")
		if err != nil {
			t.Fatalf("unreachable server must not error, got %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty result, got %v", got)
		}
	})

	t.Run("v0 metadata 500 keeps ids with context 0", func(t *testing.T) {
		v1 := `{"data":[{"id":"qwen2.5-7b-instruct"}]}`
		srv := lmStudioFixtureServer(t, v1, http.StatusOK, "", http.StatusInternalServerError)
		got, err := FetchLMStudioContexts(context.Background(), srv.Client(), slog.Default(), srv.URL, "")
		if err != nil {
			t.Fatalf("partial metadata failure must not error, got %v", err)
		}
		if v, ok := got["lmstudio/qwen2.5-7b-instruct"]; !ok || v != 0 {
			t.Errorf("key = (%d, %v), want (0, true)", v, ok)
		}
	})

	t.Run("v0 metadata malformed keeps ids", func(t *testing.T) {
		v1 := `{"data":[{"id":"qwen2.5-7b-instruct"}]}`
		srv := lmStudioFixtureServer(t, v1, http.StatusOK, `{broken`, http.StatusOK)
		got, err := FetchLMStudioContexts(context.Background(), srv.Client(), slog.Default(), srv.URL, "")
		if err != nil {
			t.Fatalf("malformed v0 body must not error, got %v", err)
		}
		if _, ok := got["lmstudio/qwen2.5-7b-instruct"]; !ok {
			t.Errorf("id missing from %v", got)
		}
	})
}

// TestNewLMStudioFetcher: the constructor produces a ContextDiscovery
// Fetcher whose signature is (ctx, baseURL, apiKey) → map[string]int
// (orchestrator wiring seam; the daemon registers it via
// ContextDiscovery.RegisterFetcher — components.go wiring is
// orchestrator-owned, not this leaf's).
func TestNewLMStudioFetcher(t *testing.T) {
	v1 := `{"data":[{"id":"Qwen2.5-7B-Instruct"}]}`
	v0 := `{"data":[{"id":"Qwen2.5-7B-Instruct","max_context_length":32768}]}`
	srv := lmStudioFixtureServer(t, v1, http.StatusOK, v0, http.StatusOK)

	var f = NewLMStudioFetcher(srv.Client(), slog.Default())
	got, err := f(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := got["lmstudio/qwen2.5-7b-instruct"]; !ok || v != 32768 {
		t.Errorf("key = (%d, %v), want (32768, true)", v, ok)
	}
}
