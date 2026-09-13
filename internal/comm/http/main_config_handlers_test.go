package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validMainConfigJSON5 = `{
  // meept main config
  "log_level": "info",
  "http": { "enabled": true, "addr": "127.0.0.1:8081" },
}`

func newMainConfigTestServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	// ConfigService is only used as an availability gate here; the handler
	// resolves the path through config.MeeptPath (MEEPT_HOME-aware).
	s := NewServer(ServerConfig{}, &ConfigService{}, nil, nil, nil, nil)
	mux := http.NewServeMux()
	s.setupRESTRoutes(mux)
	return s, mux
}

func mainConfigRequestBody(t *testing.T, content string) *strings.Reader {
	t.Helper()
	b, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return strings.NewReader(string(b))
}

func TestMainConfig_GetReturnsContentAndPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	path := filepath.Join(home, "meept.json5")
	if err := os.WriteFile(path, []byte(validMainConfigJSON5), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	s, mux := newMainConfigTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/main", http.NoBody)
	req.RemoteAddr = "127.0.0.1:54321" // the read is loopback-gated too (F32)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["path"] != path {
		t.Errorf("path = %v, want %s", body["path"], path)
	}
	if body["content"] != validMainConfigJSON5 {
		t.Errorf("content mismatch:\n got %q\nwant %q", body["content"], validMainConfigJSON5)
	}
	if body["writable"] != true {
		t.Errorf("writable = %v, want true", body["writable"])
	}
	t.Logf("GET /api/v1/config/main -> %s", w.Body.String())
	_ = s
}

func TestMainConfig_PostValidWritesAndReadsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	path := filepath.Join(home, "meept.json5")
	old := "{\n  \"log_level\": \"debug\"\n}\n"
	if err := os.WriteFile(path, []byte(old), 0o640); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	_, mux := newMainConfigTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/main", mainConfigRequestBody(t, validMainConfigJSON5))
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["ok"] != true || body["path"] != path {
		t.Errorf("body = %v, want ok=true path=%s", body, path)
	}

	// Written byte-identically (raw JSON5, not normalized).
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != validMainConfigJSON5 {
		t.Errorf("file content not byte-identical:\n got %q\nwant %q", got, validMainConfigJSON5)
	}
	// Mode preserved from the pre-existing 0640.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want 640", fi.Mode().Perm())
	}
	// Previous content preserved as .bak.
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read .bak: %v", err)
	}
	if string(bak) != old {
		t.Errorf(".bak = %q, want %q", bak, old)
	}

	// Readable back over GET (loopback-gated, like the write).
	req = httptest.NewRequest(http.MethodGet, "/api/v1/config/main", http.NoBody)
	req.RemoteAddr = "127.0.0.1:54321"
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var getBody map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &getBody)
	if getBody["content"] != validMainConfigJSON5 {
		t.Errorf("GET content = %q, want the POSTed text", getBody["content"])
	}
}

func TestMainConfig_PostInvalidLeavesFileIdentical(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	path := filepath.Join(home, "meept.json5")
	old := []byte(validMainConfigJSON5)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	_, mux := newMainConfigTestServer(t)
	invalid := `{ "log_level": "info", ` // unterminated object
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/main", mainConfigRequestBody(t, invalid))
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if _, ok := body["error"]; !ok {
		t.Errorf("400 body missing error: %s", w.Body.String())
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(old) {
		t.Errorf("file changed on invalid input:\n got %q\nwant %q", got, old)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf(".bak created on rejected write (err=%v)", err)
	}
}

func TestMainConfig_PostNonLoopbackForbidden(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	path := filepath.Join(home, "meept.json5")
	old := []byte(validMainConfigJSON5)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	_, mux := newMainConfigTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/main", mainConfigRequestBody(t, validMainConfigJSON5))
	req.RemoteAddr = "203.0.113.9:4444" // TEST-NET-3, non-loopback
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(old) {
		t.Errorf("file changed on rejected write:\n got %q\nwant %q", got, old)
	}
}

// TestMainConfig_GetNonLoopbackForbidden pins F32 (bughunt 2026-09-12 wave):
// the read returned the raw meept.json5 — which carries transport API keys —
// to any authenticated client, while the write on the same path was
// loopback-only. The commit claimed "loopback-only main config read/write".
func TestMainConfig_GetNonLoopbackForbidden(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	path := filepath.Join(home, "meept.json5")
	secret := `{
  "transport": { "http": { "enabled": true, "api_keys": ["sk-live-should-not-leak"] } },
}`
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	_, mux := newMainConfigTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/main", http.NoBody)
	req.RemoteAddr = "203.0.113.9:4444" // TEST-NET-3, non-loopback
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "loopback") {
		t.Errorf("403 body = %q, want a loopback explanation", body)
	}
	if strings.Contains(body, "should-not-leak") {
		t.Errorf("403 body leaked main-config content: %s", body)
	}
	if strings.Contains(body, "\\n") || strings.Contains(body, "transport") {
		t.Errorf("403 body looks like the config payload, not an error: %s", body)
	}

	// The same client can read the file over loopback (the GUI/menubar path).
	okReq := httptest.NewRequest(http.MethodGet, "/api/v1/config/main", http.NoBody)
	okReq.RemoteAddr = "127.0.0.1:54321"
	okW := httptest.NewRecorder()
	mux.ServeHTTP(okW, okReq)
	if okW.Code != http.StatusOK {
		t.Fatalf("loopback GET status = %d, want 200; body: %s", okW.Code, okW.Body.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(okW.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode loopback body: %v", err)
	}
	if decoded["content"] != secret {
		t.Errorf("loopback content = %v, want the seeded text (the same-host GUI read must keep working)", decoded["content"])
	}
}

// TestMainConfig_GetNonLoopbackFailsClosedOnUnavailableService: the loopback
// gate runs BEFORE the availability check, so a remote caller cannot even
// learn whether the config service is wired.
func TestMainConfig_GetNonLoopbackFailsClosedOnUnavailableService(t *testing.T) {
	t.Setenv("MEEPT_HOME", t.TempDir())

	s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil) // no config service
	mux := http.NewServeMux()
	s.setupRESTRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/main", http.NoBody)
	req.RemoteAddr = "203.0.113.9:4444"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("non-loopback GET with no config service = %d, want 403 (fail closed)", w.Code)
	}
}

func TestMainConfig_PostNotWritableForbidden(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	path := filepath.Join(home, "meept.json5")
	old := []byte(validMainConfigJSON5)
	if err := os.WriteFile(path, old, 0o400); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	_, mux := newMainConfigTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/main", mainConfigRequestBody(t, validMainConfigJSON5))
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}

func TestMainConfig_NoConfigServiceReturns503(t *testing.T) {
	s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)
	mux := http.NewServeMux()
	s.setupRESTRoutes(mux)

	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/v1/config/main", ""},
		{http.MethodPost, "/api/v1/config/main", `{"content":"{}"}`},
	} {
		var rd *strings.Reader
		if tc.body == "" {
			rd = strings.NewReader("")
		} else {
			rd = strings.NewReader(tc.body)
		}
		req := httptest.NewRequest(tc.method, tc.path, rd)
		req.RemoteAddr = "127.0.0.1:54321"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s status = %d, want 503", tc.method, tc.path, w.Code)
		}
	}
}

// TestMainConfig_LegacyMemoryRouteRemoved guards the duplicate-endpoint
// cleanup: GET /api/v1/config/memory was a second read of meept.json5
// (historically through a hardcoded $HOME/.meept path, so it could disagree
// with the MEEPT_HOME-aware canonical route). The GUI has migrated to
// /config/main and the route is retired, so it must no longer answer.
// Reintroducing a second read endpoint should fail here.
func TestMainConfig_LegacyMemoryRouteRemoved(t *testing.T) {
	t.Setenv("MEEPT_HOME", t.TempDir())

	_, mux := newMainConfigTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/memory", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /api/v1/config/memory status = %d, want 404 (route retired)", w.Code)
	}
}

func TestIsLoopbackRequest(t *testing.T) {
	cases := []struct {
		remote string
		want   bool
	}{
		{"127.0.0.1:5000", true},
		{"[::1]:5000", true},
		{"127.0.0.2:5000", true}, // loopback /8
		{"203.0.113.9:5000", false},
		{"10.0.0.5:5000", false},
		{"localhost:5000", false}, // resolves to names, not an IP
		{"127.0.0.1", false},      // no port
		{"", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/config/main", http.NoBody)
		req.RemoteAddr = tc.remote
		if got := isLoopbackRequest(req); got != tc.want {
			t.Errorf("isLoopbackRequest(%q) = %v, want %v", tc.remote, got, tc.want)
		}
	}
	if isLoopbackRequest(nil) {
		t.Error("isLoopbackRequest(nil) = true, want false")
	}
}
