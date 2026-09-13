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

// --- doc-drift pin (F32 follow-up) ------------------------------------------

// mainConfigDocFiles are the tables that document the main-config route. The
// F32 fix gated the READ as well as the write, but left every one of these
// saying "loopback clients only" on the POST row alone — and left the
// handler's own comment claiming the menubar app reads the endpoint (no Swift
// file references /api/v1/config/main; it calls /config/client, /config/models,
// /config/agents, /config/menubar). Doc drift on a security gate is how the
// gate gets "simplified" away later, so it is pinned here.
var mainConfigDocFiles = []string{
	"docs/reference/http-api.md",
	"docs/reference/http-api-complete.md",
	"menubar/README.md",
}

// mainConfigProseDocFiles state the rule in prose (they describe the GUI's
// connect path) rather than a table row.
var mainConfigProseDocFiles = []string{
	"ui/flutter_ui/README.md",
	"ui/flutter_ui/WEB_DEV.md",
}

func repoRootForDocsPin(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("repo root (go.mod) not found above %s", dir)
	return ""
}

// mainConfigTableRow returns the markdown table row for method +
// /api/v1/config/main ("" when absent).
func mainConfigTableRow(body, method string) string {
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "/api/v1/config/main") {
			continue
		}
		if !strings.Contains(line, "| "+method+" |") {
			continue
		}
		return line
	}
	return ""
}

func TestMainConfigDocsPinBothMethodsLoopbackOnly(t *testing.T) {
	root := repoRootForDocsPin(t)

	for _, rel := range mainConfigDocFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		body := string(b)
		for _, method := range []string{"GET", "POST"} {
			row := mainConfigTableRow(body, method)
			if row == "" {
				t.Errorf("%s: no %s /api/v1/config/main row found", rel, method)
				continue
			}
			if !strings.Contains(strings.ToLower(row), "loopback") {
				t.Errorf("%s: the %s /api/v1/config/main row does not say the route is loopback-only: %q", rel, method, strings.TrimSpace(row))
			}
		}
	}

	for _, rel := range mainConfigProseDocFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		// Collapse the line wraps so the assertion is about wording, not
		// where the editor broke the line.
		flat := strings.Join(strings.Fields(string(b)), " ")
		if !strings.Contains(flat, "`GET`/`POST /api/v1/config/main`") {
			t.Errorf("%s: does not document that GET and POST /api/v1/config/main are BOTH loopback-only (the GUI reads this endpoint for its multi-user probe)", rel)
		}
		if !strings.Contains(strings.ToLower(flat), "loopback clients only") {
			t.Errorf("%s: does not state the loopback-clients-only restriction on the main config route", rel)
		}
	}
}
