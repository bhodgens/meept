package transport

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPClient(t *testing.T) {
	client := NewHTTPClient("https://localhost:9999", 5*time.Second)
	if client == nil {
		t.Fatal("NewHTTPClient returned nil")
	}
	client.Close()
}

func TestNewHTTPClient_ZeroTimeout(t *testing.T) {
	// Zero timeout should use the default of 120s
	client := NewHTTPClient("https://localhost:9999", 0)
	if client == nil {
		t.Fatal("NewHTTPClient with zero timeout returned nil")
	}
	client.Close()
}

func TestNewHTTPClient_WithInsecureSkipVerify(t *testing.T) {
	client := NewHTTPClient("https://localhost:9999", 5*time.Second, WithInsecureSkipVerify(true))
	if client == nil {
		t.Fatal("NewHTTPClient with InsecureSkipVerify returned nil")
	}
	client.Close()
}

func TestHTTPClient_Connect(t *testing.T) {
	server := httptest.NewServer(httpHandlerFunc())
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	err := client.Connect()
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
}

func TestHTTPClient_Connect_NotRunning(t *testing.T) {
	client := NewHTTPClient("https://localhost:1", 1*time.Second, WithInsecureSkipVerify(true))
	defer client.Close()

	err := client.Connect()
	if err == nil {
		t.Error("Connect() to non-running server should fail")
	}
}

func TestHTTPClient_Connect_WrongStatus(t *testing.T) {
	server := httptest.NewServer(httpHandlerFuncStatus(http.StatusServiceUnavailable))
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	err := client.Connect()
	if err == nil {
		t.Error("Connect() should fail when server returns non-200")
	}
}

func TestHTTPClient_IsConnected(t *testing.T) {
	server := httptest.NewServer(httpHandlerFunc())
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	if !client.IsConnected() {
		t.Error("IsConnected() should return true when health endpoint returns 200")
	}
}

func TestHTTPClient_IsConnected_False(t *testing.T) {
	client := NewHTTPClient("https://localhost:1", 1*time.Second, WithInsecureSkipVerify(true))
	defer client.Close()

	if client.IsConnected() {
		t.Error("IsConnected() should return false when server is not reachable")
	}
}

func TestHTTPClient_Close(t *testing.T) {
	client := NewHTTPClient("https://localhost:9999", 5*time.Second)
	err := client.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestHTTPClient_SetTimeout(t *testing.T) {
	client := NewHTTPClient("https://localhost:9999", 5*time.Second)
	defer client.Close()

	// SetTimeout should not panic
	client.SetTimeout(10 * time.Second)
}

func TestHTTPClient_Chat(t *testing.T) {
	server := httptest.NewServer(httpHandlerFunc())
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	reply, err := client.Chat("hi", "conv-123")
	if err != nil {
		t.Fatalf("Chat() failed: %v", err)
	}
	if reply != "hello from daemon" {
		t.Errorf("Chat() reply = %q, want %q", reply, "hello from daemon")
	}
}

func TestHTTPClient_Status(t *testing.T) {
	server := httptest.NewServer(httpHandlerFunc())
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	status, err := client.Status()
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if status.Status != "running" {
		t.Errorf("Status().Status = %q, want %q", status.Status, "running")
	}
	if status.UptimeSeconds != 3600 {
		t.Errorf("Status().UptimeSeconds = %v, want %v", status.UptimeSeconds, 3600)
	}
}

func TestHTTPClient_Call(t *testing.T) {
	server := httptest.NewServer(httpHandlerFunc())
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	result, err := client.Call("test.method", map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatalf("Call() failed: %v", err)
	}
	// json.RawMessage preserves the exact bytes from the response
	expected := `{"key": "value"}`
	if string(result) != expected {
		t.Errorf("Call() result = %s, want %s", string(result), expected)
	}
}

// TestPinnedFingerprintRejectsMismatch verifies that a pinned fingerprint
// mismatches yield a connection error rather than silently succeeding.
func TestPinnedFingerprintRejectsMismatch(t *testing.T) {
	c := NewHTTPClient(
		"https://localhost:0",
		5*time.Second,
		WithPinnedFingerprint("deadbeef", "deadbeef"),
	)
	hc, ok := c.(*httpClient)
	if !ok {
		t.Fatalf("client is not *httpClient: %T", c)
	}
	if hc.certFingerprint == "" || hc.spkiFingerprint == "" {
		t.Fatal("pinning fields not set by WithPinnedFingerprint")
	}

	// buildTLSConfig must produce a tls.Config that uses the pinning callback
	// instead of InsecureSkipVerify.
	cfg := hc.buildTLSConfig()
	if cfg.InsecureSkipVerify {
		t.Fatal("buildTLSConfig left InsecureSkipVerify=true when a pin is configured")
	}
	if cfg.VerifyPeerCertificate == nil {
		t.Fatal("buildTLSConfig did not set VerifyPeerCertificate")
	}
	// Empty cert chain must be rejected by the callback.
	if err := cfg.VerifyPeerCertificate(nil, nil); err == nil {
		t.Fatal("VerifyPeerCertificate accepted empty chain")
	}
	// A bogus cert payload must not match the pinned fingerprint.
	if err := cfg.VerifyPeerCertificate([][]byte{[]byte("not a real cert")}, nil); err == nil {
		t.Fatal("VerifyPeerCertificate accepted mismatching cert")
	}
}

// TestPinnedFingerprintAcceptsMatch spins up httptest.NewTLSServer, computes
// the real cert + SPKI SHA-256 digests, and verifies a GET succeeds.
func TestPinnedFingerprintAcceptsMatch(t *testing.T) {
	server := httptest.NewTLSServer(httpHandlerFunc())
	defer server.Close()

	cert := server.Certificate()
	if cert == nil {
		t.Fatal("server did not expose a certificate")
	}
	certSum := sha256.Sum256(cert.Raw)
	certHex := hex.EncodeToString(certSum[:])
	spki, err := publicKeySPKI(cert)
	if err != nil {
		t.Fatalf("marshal SPKI: %v", err)
	}
	spkiSum := sha256.Sum256(spki)
	spkiHex := hex.EncodeToString(spkiSum[:])

	client := NewHTTPClient(
		server.URL,
		5*time.Second,
		WithPinnedFingerprint(certHex, spkiHex),
		// Install a transport that trusts the test server's CA so that the
		// chain validation step (InsecureSkipVerify=false) succeeds.
		withTestServerTransport(server),
	)
	defer client.Close()

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect with matching pin failed: %v", err)
	}
}

// TestPinnedFingerprintCaseInsensitive verifies that hex comparison is
// case-insensitive so callers can supply either lower or upper case digests.
func TestPinnedFingerprintCaseInsensitive(t *testing.T) {
	server := httptest.NewTLSServer(httpHandlerFunc())
	defer server.Close()

	cert := server.Certificate()
	certSum := sha256.Sum256(cert.Raw)
	certHex := strings.ToUpper(hex.EncodeToString(certSum[:]))

	client := NewHTTPClient(
		server.URL,
		5*time.Second,
		WithPinnedFingerprint(certHex, ""),
		withTestServerTransport(server),
	)
	defer client.Close()

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect with uppercase pin failed: %v", err)
	}
}

// withTestServerTransport returns an option that replaces the underlying
// *http.Transport with one whose RootCAs pool contains the test server's
// certificate, allowing chain validation to succeed against the test CA.
func withTestServerTransport(server *httptest.Server) HTTPClientOption {
	return func(c *httpClient) {
		pool := x509.NewCertPool()
		pool.AddCert(server.Certificate())
		transport, ok := c.client.Transport.(*http.Transport)
		if !ok {
			return
		}
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.RootCAs = pool
	}
}

// Reference the tls package so the import stays used even if the compile-time
// signature of the test file evolves.
var _ = tls.VersionTLS12
