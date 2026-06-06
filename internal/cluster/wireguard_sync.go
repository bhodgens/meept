package cluster

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	texttemplate "text/template"
)

// WireGuard interface config template.
// Uses the [Interface] and [Peer] section format understood by wireguard-tools.
const wgConfigTemplate = `[Interface]
PrivateKey = {{ .PrivateKey }}
Address = {{ .ClusterIP }}/32
ListenPort = {{ .ListenPort }}
DNS = {{ .DNS }}

{{- range .Peers }}
[Peer]
PublicKey = {{ .WireGuardPub }}
AllowedIPs = {{ .ClusterIP }}/32
PersistentKeepalive = {{ .PersistentKeepalive }}
{{- if .Endpoint }}
Endpoint = {{ .Endpoint }}
{{- end }}
{{- end }}
`

// WireGuard syncconf template — minimal config for wg syncconf.
// syncconf requires the interface's private key plus peer configuration.
const wgSyncConfTemplate = `[Interface]
PrivateKey = {{ .PrivateKey }}
Address = {{ .ClusterIP }}/32

{{- range .Peers }}
[Peer]
PublicKey = {{ .WireGuardPub }}
AllowedIPs = {{ .ClusterIP }}/32
PersistentKeepalive = {{ .PersistentKeepalive }}
{{- if .Endpoint }}
Endpoint = {{ .Endpoint }}
{{- end }}
{{- end }}
`

// WireGuardConfig holds the parameters needed to generate a WireGuard
// configuration.
type WireGuardConfig struct {
	// PrivateKey is this node's WireGuard private key (base64).
	PrivateKey string

	// ClusterIP is this node's IP address in the WireGuard subnet.
	ClusterIP string

	// ListenPort is the port to listen on for WireGuard traffic.
	ListenPort int

	// DNS is the DNS server to include in the config (e.g., "8.8.8.8").
	DNS string

	// Peers are the known cluster members to add as peers.
	Peers []Member

	// PersistentKeepalive is the interval for keepalive messages (e.g., "25").
	PersistentKeepalive string
}

// WireGuardManager handles WireGuard configuration generation and application.
type WireGuardManager struct {
	configPath string
	iface      string
	tmpl       *texttemplate.Template
}

// NewWireGuardManager creates a new WireGuard config manager.
//
// configPath is where the generated WireGuard config will be written
// (e.g., "~/.meept/cluster/wg0.conf"). iface is the interface name
// (e.g., "wg0").
func NewWireGuardManager(configPath, iface string) (*WireGuardManager, error) {
	tmpl, err := parseTemplate("wg-interface", wgConfigTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse WireGuard config template: %w", err)
	}

	return &WireGuardManager{
		configPath: configPath,
		iface:      iface,
		tmpl:       tmpl,
	}, nil
}

// GenerateConfig generates the WireGuard configuration text from the
// provided config.
func (m *WireGuardManager) GenerateConfig(cfg *WireGuardConfig) ([]byte, error) {
	var buf bytes.Buffer
	if err := m.tmpl.Execute(&buf, cfg); err != nil {
		return nil, fmt.Errorf("failed to generate WireGuard config: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteConfig writes the WireGuard configuration to disk and applies it via
// wg syncconf. The config file is written with 0600 permission so only the
// local user can read the WireGuard private key.
func (m *WireGuardManager) WriteConfig(cfg *WireGuardConfig) error {
	data, err := m.GenerateConfig(cfg)
	if err != nil {
		return err
	}

	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}

	if err := os.WriteFile(m.configPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write WireGuard config to %s: %w", m.configPath, err)
	}

	return nil
}

// ApplyConfig generates a new WireGuard configuration from cfg, writes it
// to a temporary file, and syncs it to the interface via `wg syncconf`.
// This method does not tear down the existing interface, so established
// connections are preserved.
func (m *WireGuardManager) ApplyConfig(cfg *WireGuardConfig) error {
	// Write the full config file (for diagnostics / persistence).
	if err := m.WriteConfig(cfg); err != nil {
		return err
	}

	// Write the minimal syncconf to a temp file to avoid race conditions
	// between read and write.
	tmpFile, err := os.CreateTemp(filepath.Dir(m.configPath), "wg-new-*.conf")
	if err != nil {
		return fmt.Errorf("failed to create temp config file: %w", err)
	}
	tmpPath := tmpFile.Name()

	tmpConfig, err := m.generateSyncConf(cfg)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}

	if _, err := tmpFile.Write(tmpConfig); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp config: %w", err)
	}
	tmpFile.Close()

	// wg syncconf applies the new config atomically — it replaces peers
	// without tearing down the interface.
	cmd := exec.Command("wg", "syncconf", m.iface, tmpPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.Remove(tmpPath)

		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return fmt.Errorf("wg syncconf failed for %s: %s", m.iface, stderrStr)
		}
		return fmt.Errorf("wg syncconf failed for %s: %w %s", m.iface, err, stdout.String())
	}

	// Cleanup temp file.
	os.Remove(tmpPath)

	return nil
}

// AddPeer adds a single peer to the WireGuard interface by re-applying the
// full config with the peer appended to the list.
func (m *WireGuardManager) AddPeer(peer Member, cfg *WireGuardConfig) error {
	cfg.Peers = append(cfg.Peers, peer)
	return m.ApplyConfig(cfg)
}

// RemovePeer removes a peer from the WireGuard interface by filtering it out
// and re-applying the config.
func (m *WireGuardManager) RemovePeer(nodeID string, cfg *WireGuardConfig) error {
	var peers []Member
	for _, p := range cfg.Peers {
		if p.NodeID != nodeID {
			peers = append(peers, p)
		}
	}
	cfg.Peers = peers
	return m.ApplyConfig(cfg)
}

// UpdatePeers replaces the entire peer list with the provided members and
// applies the new configuration.
func (m *WireGuardManager) UpdatePeers(peers []Member, cfg *WireGuardConfig) error {
	cfg.Peers = peers
	return m.ApplyConfig(cfg)
}

// SyncWithInterface reads the current WireGuard interface state via
// `wg show` and returns it as a string. This is useful for diagnostics.
func (m *WireGuardManager) SyncWithInterface() (string, error) {
	cmd := exec.Command("wg", "show", m.iface)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("wg show %s failed: %w", m.iface, err)
	}

	return stdout.String(), nil
}

// generateSyncConf creates the minimal WireGuard config suitable for
// wg syncconf. syncconf requires only the interface private key plus peer
// definitions.
func (m *WireGuardManager) generateSyncConf(cfg *WireGuardConfig) ([]byte, error) {
	tmpl, err := parseTemplate("wg-syncconf", wgSyncConfTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse syncconf template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return nil, fmt.Errorf("failed to generate syncconf config: %w", err)
	}
	return buf.Bytes(), nil
}

// parseTemplate parses a WireGuard config text template.
func parseTemplate(name, text string) (*texttemplate.Template, error) {
	return texttemplate.New(name).Parse(text)
}
