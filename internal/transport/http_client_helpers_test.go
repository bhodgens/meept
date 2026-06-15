package transport

import (
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// httpHandlerFunc returns a test server handler matching the responses used by
// the existing http_client tests. Centralising it here keeps the test file
// short while allowing new tests to share the same setup. The handler matches
// the historical behaviour of returning the status payload for any path,
// except for explicit well-known routes (health, chat, bus/call).
func httpHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/chat":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"reply": "hello from daemon"}`))
		case "/api/v1/bus/call":
			// Use method in payload to dispatch; default to the generic
			// {"key":"value"} result so TestHTTPClient_Call keeps passing.
			body := `{"result": {"key": "value"}}`
			if r.Method == http.MethodPost {
				// Inspect the body to detect "status" method calls.
				// Keep this best-effort so the test does not need a full
				// bus proxy implementation.
				var req struct {
					Method string `json:"method"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req.Method == "status" {
					body = `{"result": {
						"status": "running",
						"uptime_seconds": 3600,
						"tokens_used": 100,
						"tokens_remaining": 900,
						"budget_used": 0.05,
						"budget_remaining": 0.95,
						"registered_methods": ["chat", "status"],
						"bus_subscribers": 3
					}}`
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		default:
			// Historical tests assumed any other path returned the status
			// payload verbatim. Keep that behaviour for compatibility.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"result": {
					"status": "running",
					"uptime_seconds": 3600,
					"tokens_used": 100,
					"tokens_remaining": 900,
					"budget_used": 0.05,
					"budget_remaining": 0.95,
					"registered_methods": ["chat", "status"],
					"bus_subscribers": 3
				}
			}`))
		}
	}
}

// httpHandlerFuncStatus returns a handler that always responds with the given
// HTTP status code.
func httpHandlerFuncStatus(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}
}

// publicKeySPKI extracts the SubjectPublicKeyInfo bytes from a certificate.
// It is the same encoding used by VerifyPeerCertificate for SPKI pinning.
func publicKeySPKI(cert *x509.Certificate) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(cert.PublicKey)
}

// Suppress "unused" warnings for json/httptest when no test references them.
var (
	_ = json.Marshal
	_ = httptest.NewServer
	_ = strings.TrimPrefix
	_ = (*testing.T)(nil)
)
