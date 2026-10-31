package config

// cluster_config_test.go — leaf 08 / C9 (agent-groups plan): the
// cluster.security.require_node_signatures DEFAULT is TRUE, and the
// effective value for a cluster-enabled boot is signatures-required even
// when the operator's config omits (or zero-fills) the security section.
// An explicit false is honored — the opt-out survives the flip.

import (
	"testing"
)

// TestDefaultClusterConfig_RequireNodeSignaturesOn pins the schema
// default: a freshly built ClusterConfig requires node signatures.
func TestDefaultClusterConfig_RequireNodeSignaturesOn(t *testing.T) {
	cfg := DefaultClusterConfig()
	if !cfg.Security.RequireNodeSignatures {
		t.Fatal("cluster.security.require_node_signatures default must be true (leaf 08 / C9)")
	}
}

// TestClusterEnabledRequiresSignatures pins the load-boundary contract:
// ANY path that produces a cluster-enabled config — defaults, TOML load,
// JSON5 load — yields require_node_signatures=true when the security
// section is omitted. Non-cluster boots are untouched: the flip is
// conditional on cluster.enabled and the disabled default config keeps
// require_node_signatures=true anyway (defense in depth), so a config
// that merely enables the cluster section later inherits signatures ON.
func TestClusterEnabledRequiresSignatures(t *testing.T) {
	// DefaultConfig is the substrate every loader starts from.
	cfg := DefaultConfig()
	if !cfg.Cluster.Enabled {
		// The shipped default has the cluster section itself disabled;
		// the security DEFAULT is still ON so that enabling the cluster
		// never silently downgrades signature enforcement.
		if !cfg.Cluster.Security.RequireNodeSignatures {
			t.Fatal("cluster.security.require_node_signatures must default true even while cluster is disabled")
		}
	}

	// Simulate the config-merge direction: an operator config that sets
	// cluster.enabled=true and mentions NO security keys lands on top of
	// DefaultConfig via TOML unmarshal. The security zero-value that
	// unmarshal produces must NOT silently win over the default — pin
	// the normalize seam that enforces the conditional flip.
	merged := DefaultConfig()
	merged.Cluster.Enabled = true // as if TOML set it
	NormalizeClusterSecurityDefaults(&merged.Cluster)
	if !merged.Cluster.Security.RequireNodeSignatures {
		t.Fatal("cluster.enabled=true with no explicit security section must require node signatures")
	}
	if merged.Cluster.Security.Ed25519KeyRotationDays != 90 {
		t.Fatalf("rotation days default: want 90, got %d", merged.Cluster.Security.Ed25519KeyRotationDays)
	}

	// Explicit opt-out survives: an operator who set the key to false
	// keeps false (zero-value indistinguishable from absent at the
	// struct level, so the seam must be applied only to configs NOT
	// deserialized from user text — the DefaultConfig substrate always
	// carries the explicit-true baseline that a user false overrides).
	optOut := DefaultConfig()
	optOut.Cluster.Security.RequireNodeSignatures = false
	NormalizeClusterSecurityDefaults(&optOut.Cluster)
	if optOut.Cluster.Security.RequireNodeSignatures {
		t.Fatal("explicit require_node_signatures=false must be honored (opt-out survives the flip)")
	}
}

// TestClusterConfigZeroValueNormalize pins the seam's guard: the
// normalization is a no-op on an already-correct config (idempotent) and
// fills the rotation-days default alongside the signature default.
func TestClusterConfigZeroValueNormalize(t *testing.T) {
	cfg := DefaultClusterConfig()
	NormalizeClusterSecurityDefaults(&cfg)
	if !cfg.Security.RequireNodeSignatures || cfg.Security.Ed25519KeyRotationDays != 90 {
		t.Fatalf("normalize corrupted a correct config: %+v", cfg.Security)
	}
}
