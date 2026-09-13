package daemon

import (
	"net"
	"testing"

	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/config"
)

// TestHTTPListenAddrEmptyConfigBindsLoopback pins finding F13 (bughunt
// 2026-09-12 wave): an ENABLED HTTP transport with neither `addr` nor `port`
// bound to loopback. The defect was that daemon.go assigned
// `httpCfg.Addr = fullCfg.Transport.HTTP.ListenAddr()` unconditionally;
// ListenAddr() returns "" for "neither key set", and net.Listen("tcp", "")
// binds EVERY interface on an ephemeral port — exactly the exposure 84ce30f9
// removed when it made the transport loopback-only.
//
// No listener is opened here: the invariant is asserted on the resolved string
// (loopback host, non-zero port) so the test cannot collide with a live daemon.
func TestHTTPListenAddrEmptyConfigBindsLoopback(t *testing.T) {
	// An operator config that enables the transport without an address — the
	// shipped template with `addr` removed, or a config predating the key.
	transport := config.HTTPTransportConfig{Enabled: true}

	// The premise this fix exists for: the raw resolution is EMPTY, so any
	// consumer that assigns it straight onto the server config would hand
	// net.Listen the wildcard address on an ephemeral port.
	if got := transport.ListenAddr(); got != "" {
		t.Fatalf("premise changed: ListenAddr() for a keyless transport = %q, want empty (re-derive the F13 fix if this is intentional)", got)
	}

	serverDefault := http.DefaultServerConfig().Addr
	got := httpListenAddrFor(transport, serverDefault)
	if got != serverDefault {
		t.Fatalf("resolved addr = %q, want the server's own loopback default %q", got, serverDefault)
	}
	if got == "" {
		t.Fatal("resolved addr is empty: net.Listen would bind the wildcard address on an ephemeral port")
	}

	host, port, err := net.SplitHostPort(got)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", got, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		t.Fatalf("resolved host %q is not an IP; the bind policy cannot be verified", host)
	}
	if !ip.IsLoopback() {
		t.Errorf("resolved host %q is not loopback: the transport must never bind all interfaces", host)
	}
	if ip.IsUnspecified() {
		t.Errorf("resolved host %q is the unspecified address (all interfaces)", host)
	}
	if port == "" || port == "0" {
		t.Errorf("resolved port %q is ephemeral/zero: consumers derive the GUI endpoint from this address", port)
	}
}

// TestHTTPListenAddrConfigWinsOverDefault pins the other half of F13: the
// fallback must never override an explicit operator address. `addr` wins, then
// the `port` alias, then the server default — the documented precedence.
func TestHTTPListenAddrConfigWinsOverDefault(t *testing.T) {
	serverDefault := http.DefaultServerConfig().Addr

	cases := []struct {
		name      string
		transport config.HTTPTransportConfig
		want      string
	}{
		{name: "addr wins", transport: config.HTTPTransportConfig{Enabled: true, Addr: "127.0.0.1:19999"}, want: "127.0.0.1:19999"},
		{name: "addr wins over port", transport: config.HTTPTransportConfig{Enabled: true, Addr: "127.0.0.1:19999", Port: 18095}, want: "127.0.0.1:19999"},
		{name: "port alias", transport: config.HTTPTransportConfig{Enabled: true, Port: 18095}, want: "127.0.0.1:18095"},
		{name: "neither -> server default", transport: config.HTTPTransportConfig{Enabled: true}, want: serverDefault},
		{name: "disabled but keyless -> server default", transport: config.HTTPTransportConfig{}, want: serverDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := httpListenAddrFor(tc.transport, serverDefault); got != tc.want {
				t.Errorf("httpListenAddrFor(%+v) = %q, want %q", tc.transport, got, tc.want)
			}
		})
	}
}

// TestHTTPTransportAddrBootLogNeverEmpty pins the boot-log consumer of the same
// resolution: it previously printed an empty addr for exactly the config the
// server binds on the loopback default. A nil config still reports "" (no
// config loaded), a loaded config never does.
func TestHTTPTransportAddrBootLogNeverEmpty(t *testing.T) {
	if got := httpTransportAddr(nil); got != "" {
		t.Errorf("httpTransportAddr(nil) = %q, want empty", got)
	}

	cfg := &config.Config{}
	cfg.Transport.HTTP.Enabled = true // no addr, no port
	got := httpTransportAddr(cfg)
	if got == "" {
		t.Fatal("httpTransportAddr reported an empty addr for an enabled keyless transport")
	}
	if got != http.DefaultServerConfig().Addr {
		t.Errorf("httpTransportAddr = %q, want the server loopback default %q", got, http.DefaultServerConfig().Addr)
	}

	cfg.Transport.HTTP.Addr = "127.0.0.1:19998"
	if got := httpTransportAddr(cfg); got != "127.0.0.1:19998" {
		t.Errorf("httpTransportAddr with explicit addr = %q, want it honored verbatim", got)
	}
}
