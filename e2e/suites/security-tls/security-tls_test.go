//go:build e2e

// Suite security-tls: the daemon's HTTP listener honors the configured
// tls_min_version floor — a TLS-1.2-only client is rejected when the
// config pins tls1.3, while a TLS-1.3 client connects; and the floor
// defaults to 1.2.
package securitytls

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// tlsSandbox is the daemon-lifecycle sandbox shape with a configurable
// tls_min_version — a hand-written config because the harness Stack
// pins one boot per Stack and this suite needs version variants.
type tlsSandbox struct {
	t         *testing.T
	Work      string
	Home      string
	MeeptHome string
	StateDir  string
	Socket    string
	HTTPAddr  string
	Fake      *harness.FakeLLM
}

func newTLSSandbox(t *testing.T, port int, minVersion string) *tlsSandbox {
	t.Helper()
	work, err := os.MkdirTemp("", "meept-e2e-tls-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	if resolved, err := filepath.EvalSymlinks(work); err == nil {
		work = resolved
	}
	s := &tlsSandbox{
		t:        t,
		Work:     work,
		Home:     filepath.Join(work, "home"),
		StateDir: filepath.Join(work, "state"),
		Socket:   filepath.Join(work, "state", "meept.sock"),
		HTTPAddr: harness.PortString(port),
		Fake:     harness.NewFakeLLM(),
	}
	t.Cleanup(s.Fake.Close)
	s.MeeptHome = filepath.Join(s.Home, ".meept")
	for _, d := range []string{s.Home, s.MeeptHome, s.StateDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	minJSON := ""
	if minVersion != "" {
		minJSON = fmt.Sprintf(`"tls_min_version": %q,`, minVersion)
	}
	cfg := fmt.Sprintf(`{
  "daemon": {
    "socket_path": %q,
    "pid_file": %q,
    "data_dir": %q,
    "log_level": "INFO",
  },
  "transport": {
    "rpc": { "enabled": true, "socket_path": %q },
    "http": {
      "enabled": true,
      "addr": %q,
      "require_auth": false,
      %s
      "tls_cert_file": %q,
      "tls_key_file": %q,
      "rest": true,
      "websocket": false,
      "mcp": false,
    },
  },
  "memory": { "data_dir": %q },
  "projects": { "enabled": true, "base_dir": %q, "auto_detect": false, "fence_enabled": false },
  "security": { "audit_db_path": %q, "allowed_paths": [%q] },
  "multiagent": { "enabled": true },
}`,
		s.Socket,
		filepath.Join(s.StateDir, "meept.pid"),
		s.StateDir,
		s.Socket,
		s.HTTPAddr,
		minJSON,
		filepath.Join(s.StateDir, "tls", "cert.pem"),
		filepath.Join(s.StateDir, "tls", "key.pem"),
		filepath.Join(s.StateDir, "memory"),
		filepath.Join(s.StateDir, "projects"),
		filepath.Join(s.StateDir, "audit.db"),
		filepath.ToSlash(filepath.Join(s.Work, "**")),
	)
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "meept.json5"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	models := fmt.Sprintf(`{
  "model": "fake/fake-model",
  "small_model": "fake/fake-model",
  "classifier_model": "classifier",
  "summarizer_model": "summarizer",
  "providers": { "fake": { "api": "openai", "options": { "baseURL": %q, "noAuth": true },
    "models": { "fake-model": { "name": "fake-model", "capabilities": ["completion"] } } } }
}`, s.Fake.URL()+"/v1")
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "models.json5"), []byte(models), 0o600); err != nil {
		t.Fatalf("write models: %v", err)
	}
	return s
}

// boot starts the daemon and waits for /health with a TLS-1.3-capable
// client (any floor accepts 1.3). Registers a kill cleanup once healthy.
func (s *tlsSandbox) boot() {
	t := s.t
	cmd := exec.Command(harness.DaemonPath(t),
		"-c", filepath.Join(s.MeeptHome, "meept.json5"),
		"-d", s.StateDir,
		"-s", s.Socket,
	)
	cmd.Dir = s.Work
	logFile, err := os.OpenFile(filepath.Join(s.Work, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "HOME="+s.Home, "MEEPT_HOME="+s.MeeptHome)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	// Ensure the process never outlives the test even on a failed boot.
	alreadyKilled := false
	kill := func() {
		if !alreadyKilled {
			alreadyKilled = true
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
	t.Cleanup(kill)

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,             //nolint:gosec // scratch self-signed cert
				MinVersion:         tls.VersionTLS13, // the probe client speaks 1.3
				MaxVersion:         tls.VersionTLS13,
			},
		},
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := client.Get("https://" + s.HTTPAddr + "/health") //nolint:noctx // bounded poll
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon not healthy within 60s; log tail:\n%s", s.logTail())
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (s *tlsSandbox) logTail() string {
	data, err := os.ReadFile(filepath.Join(s.Work, "daemon.log"))
	if err != nil {
		return fmt.Sprintf("(no log: %v)", err)
	}
	if len(data) > 4096 {
		data = data[len(data)-4096:]
	}
	return string(data)
}

// dialTLS performs a raw TLS handshake at exactly the requested version
// and reports whether the server accepted it.
func dialTLS(t *testing.T, addr string, version uint16) error {
	t.Helper()
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // scratch self-signed cert
		MinVersion:         version,
		MaxVersion:         version, // pin EXACTLY the version under test
	})
	if err != nil {
		return err
	}
	return conn.Close()
}

// TestSecurityTLS_MinVersionFloorEnforced covers security-tls-01: with
// tls_min_version "tls1.3" the listener rejects a TLS-1.2-only handshake
// but accepts TLS 1.3; without the pin the floor defaults to 1.2 and both
// versions connect.
func TestSecurityTLS_MinVersionFloorEnforced(t *testing.T) {
	// --- Pinned floor: tls1.3 rejects a 1.2-only client. ---
	pinned := newTLSSandbox(t, harness.FreePort(t), "tls1.3")
	pinned.boot()

	if err := dialTLS(t, pinned.HTTPAddr, tls.VersionTLS12); err == nil {
		t.Fatal("TLS 1.2 handshake succeeded against a tls1.3 floor — floor not enforced")
	}
	if err := dialTLS(t, pinned.HTTPAddr, tls.VersionTLS13); err != nil {
		t.Fatalf("TLS 1.3 handshake failed against a tls1.3 floor: %v\nlog tail:\n%s", err, pinned.logTail())
	}

	// The HTTP surface serves the healthy payload to a 1.3 client.
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // scratch self-signed cert
				MinVersion:         tls.VersionTLS13,
				MaxVersion:         tls.VersionTLS13,
			},
		},
	}
	resp, err := client.Get("https://" + pinned.HTTPAddr + "/health") //nolint:noctx // bounded call
	if err != nil {
		t.Fatalf("health over TLS 1.3: %v", err)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("health decode: %v", err)
	}
	_ = resp.Body.Close()
	if body["status"] != "ok" {
		t.Fatalf("health = %+v", body)
	}

	// --- Default floor: unspecified means 1.2 — both versions connect. ---
	defaultFloor := newTLSSandbox(t, harness.FreePort(t), "")
	defaultFloor.boot()
	if err := dialTLS(t, defaultFloor.HTTPAddr, tls.VersionTLS12); err != nil {
		t.Fatalf("TLS 1.2 should connect under the default floor: %v", err)
	}
	if err := dialTLS(t, defaultFloor.HTTPAddr, tls.VersionTLS13); err != nil {
		t.Fatalf("TLS 1.3 should connect under the default floor: %v", err)
	}
}
