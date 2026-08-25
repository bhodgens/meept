package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/caimlas/meept/internal/secrets"
)

// TestSecretsConfigDefaults verifies DefaultConfig carries an empty non-nil
// secrets source map and a disabled proxy.
func TestSecretsConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Secrets.Sources == nil {
		t.Fatal("default Secrets.Sources must be empty non-nil map")
	}
	if len(cfg.Secrets.Sources) != 0 {
		t.Fatalf("default Secrets.Sources must be empty, got %v", cfg.Secrets.Sources)
	}
	if cfg.Secrets.Proxy.Enabled {
		t.Error("default Secrets.Proxy.Enabled must be false")
	}
	if cfg.Secrets.Proxy.Listen != "" {
		t.Errorf("default Secrets.Proxy.Listen must be empty (leaf 04 fills behavior), got %q", cfg.Secrets.Proxy.Listen)
	}
}

// TestSecretsConfigRoundTrip loads a TOML snippet through the real config
// load path and verifies [secrets] parses into typed fields.
func TestSecretsConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")

	content := `
[secrets.sources.api_token]
kind = "env"
name = "GITHUB_TOKEN"
hosts = ["api.github.com", "github.com"]
header = "Authorization"
format = "Bearer {}"

[secrets.sources.signing_key]
kind = "file"
name = "/etc/meept/signing.key"
header = "X-Signature"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if got := len(cfg.Secrets.Sources); got != 2 {
		t.Fatalf("loaded %d secret sources, want 2: %+v", got, cfg.Secrets.Sources)
	}

	api, ok := cfg.Secrets.Sources["api_token"]
	if !ok {
		t.Fatal("api_token missing from parsed sources")
	}
	want := secrets.Source{
		Kind:   "env",
		Name:   "GITHUB_TOKEN",
		Hosts:  []string{"api.github.com", "github.com"},
		Header: "Authorization",
		Format: "Bearer {}",
	}
	if !reflect.DeepEqual(api, want) {
		t.Fatalf("api_token = %+v, want %+v", api, want)
	}

	sig := cfg.Secrets.Sources["signing_key"]
	if sig.Kind != "file" || sig.Name != "/etc/meept/signing.key" || sig.Header != "X-Signature" {
		t.Fatalf("signing_key parsed wrong: %+v", sig)
	}
}

// TestSecretsProxyConfigRoundTrip verifies the [secrets.proxy] subsection
// parses enabled/listen through the real config load path (leaf 04).
func TestSecretsProxyConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")

	content := `
[secrets.proxy]
enabled = true
listen  = "127.0.0.1:18080"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !cfg.Secrets.Proxy.Enabled {
		t.Error("Secrets.Proxy.Enabled should parse true")
	}
	if cfg.Secrets.Proxy.Listen != "127.0.0.1:18080" {
		t.Errorf("Secrets.Proxy.Listen = %q, want %q", cfg.Secrets.Proxy.Listen, "127.0.0.1:18080")
	}
}

// TestSecretsSourceIsBrokerSource pins that the config-layer type IS the
// broker-layer Source (single definition; no drift between layers).
func TestSecretsSourceIsBrokerSource(t *testing.T) {
	var _ map[string]secrets.Source = SecretSources{}
	var _ secrets.Config = map[string]secrets.Source{}
	// Conversion between the two named types must be legal (identical
	// underlying type), proving zero drift.
	var cfg secrets.Config = secrets.Config(SecretSources{})
	if cfg == nil {
		t.Fatal("conversion produced nil")
	}
}
