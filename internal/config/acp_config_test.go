package config

import (
	"strings"
	"testing"
)

func TestDefaultConfig_ACP(t *testing.T) {
	cfg := DefaultConfig()
	acp := cfg.ACP
	if acp.Enabled {
		t.Error("acp.enabled default = true, want false")
	}
	if acp.AgentsFile != "~/.meept/acp_agents.json5" {
		t.Errorf("acp.agents_file default = %q, want ~/.meept/acp_agents.json5", acp.AgentsFile)
	}
	if acp.DialTimeout != 10 {
		t.Errorf("acp.dial_timeout default = %d, want 10", acp.DialTimeout)
	}
	if acp.CallTimeout != 120 {
		t.Errorf("acp.call_timeout default = %d, want 120", acp.CallTimeout)
	}
	if acp.MaxAgents != 3 {
		t.Errorf("acp.max_agents default = %d, want 3", acp.MaxAgents)
	}
	if acp.PermissionMode != "permissive" {
		t.Errorf("acp.permission_mode default = %q, want permissive", acp.PermissionMode)
	}
	if acp.DecisionThreshold != 0.5 {
		t.Errorf("acp.decision_threshold default = %v, want 0.5", acp.DecisionThreshold)
	}
	if acp.RelayTimeout != 120 {
		t.Errorf("acp.relay_timeout default = %d, want 120", acp.RelayTimeout)
	}
	if acp.DecisionEscalateToHuman {
		t.Error("acp.decision_escalate_to_human default = true, want false")
	}
	if err := acp.Validate(); err != nil {
		t.Errorf("default acp config should validate: %v", err)
	}
}

func TestACPConfig_Validate(t *testing.T) {
	valid := func() ACPConfig {
		return DefaultConfig().ACP
	}

	ok := valid()
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	// "decision" is a valid mode when decision_threshold is in (0, 1].
	dec := valid()
	dec.PermissionMode = "decision"
	dec.DecisionThreshold = 0.75
	if err := dec.Validate(); err != nil {
		t.Errorf("permission_mode \"decision\" with threshold 0.75 rejected: %v", err)
	}
	edge := valid()
	edge.PermissionMode = "decision"
	edge.DecisionThreshold = 1
	if err := edge.Validate(); err != nil {
		t.Errorf("permission_mode \"decision\" with threshold 1 rejected: %v", err)
	}

	// Outside decision mode the knob is unused: an explicit zero is accepted
	// even though the default is 0.5.
	zero := valid()
	zero.PermissionMode = "permissive"
	zero.DecisionThreshold = 0
	if err := zero.Validate(); err != nil {
		t.Errorf("permission_mode \"permissive\" with threshold 0 rejected: %v", err)
	}
	denyZero := valid()
	denyZero.PermissionMode = "deny"
	denyZero.DecisionThreshold = 0
	if err := denyZero.Validate(); err != nil {
		t.Errorf("permission_mode \"deny\" with threshold 0 rejected: %v", err)
	}

	// "human" is a valid mode: each permission request pauses for a human
	// answer over the relay. relay_timeout applies, but the mode itself
	// needs no extra knobs.
	hum := valid()
	hum.PermissionMode = "human"
	if err := hum.Validate(); err != nil {
		t.Errorf("permission_mode \"human\" rejected: %v", err)
	}

	// relay_timeout is enforced only when the relay is actually reachable:
	// mode "human" OR decision_escalate_to_human. Elsewhere a non-positive
	// value is accepted (unused), same convention as decision_threshold.
	for _, mode := range []string{"permissive", "deny"} {
		idle := valid()
		idle.PermissionMode = mode
		idle.RelayTimeout = 0
		if err := idle.Validate(); err != nil {
			t.Errorf("relay_timeout 0 rejected in %q mode with escalation off: %v", mode, err)
		}
		esc := valid()
		esc.PermissionMode = mode
		esc.RelayTimeout = 0
		esc.DecisionEscalateToHuman = true
		if err := esc.Validate(); err == nil || !strings.Contains(err.Error(), "relay_timeout") {
			t.Errorf("relay_timeout 0 with escalation on in %q mode: error %v must mention relay_timeout", mode, err)
		}
	}

	cases := []struct {
		name    string
		mutate  func(*ACPConfig)
		wantSub string
	}{
		{
			name:    "dial_timeout zero",
			mutate:  func(c *ACPConfig) { c.DialTimeout = 0 },
			wantSub: "dial_timeout",
		},
		{
			name:    "dial_timeout negative",
			mutate:  func(c *ACPConfig) { c.DialTimeout = -1 },
			wantSub: "dial_timeout",
		},
		{
			name:    "call_timeout zero",
			mutate:  func(c *ACPConfig) { c.CallTimeout = 0 },
			wantSub: "call_timeout",
		},
		{
			name:    "max_agents zero",
			mutate:  func(c *ACPConfig) { c.MaxAgents = 0 },
			wantSub: "max_agents",
		},
		{
			name:    "max_agents too high",
			mutate:  func(c *ACPConfig) { c.MaxAgents = 33 },
			wantSub: "max_agents",
		},
		{
			name:    "permission_mode empty",
			mutate:  func(c *ACPConfig) { c.PermissionMode = "" },
			wantSub: "permission_mode",
		},
		{
			name:    "permission_mode unknown",
			mutate:  func(c *ACPConfig) { c.PermissionMode = "ask" },
			wantSub: "permission_mode",
		},
		{
			name: "decision mode zero threshold",
			mutate: func(c *ACPConfig) {
				c.PermissionMode = "decision"
				c.DecisionThreshold = 0
			},
			wantSub: "decision_threshold",
		},
		{
			name: "decision mode negative threshold",
			mutate: func(c *ACPConfig) {
				c.PermissionMode = "decision"
				c.DecisionThreshold = -0.5
			},
			wantSub: "decision_threshold",
		},
		{
			name: "decision mode threshold above one",
			mutate: func(c *ACPConfig) {
				c.PermissionMode = "decision"
				c.DecisionThreshold = 1.5
			},
			wantSub: "decision_threshold",
		},
		{
			name: "human mode zero relay_timeout",
			mutate: func(c *ACPConfig) {
				c.PermissionMode = "human"
				c.RelayTimeout = 0
			},
			wantSub: "relay_timeout",
		},
		{
			name: "human mode negative relay_timeout",
			mutate: func(c *ACPConfig) {
				c.PermissionMode = "human"
				c.RelayTimeout = -3
			},
			wantSub: "relay_timeout",
		},
		{
			name: "decision mode escalation on zero relay_timeout",
			mutate: func(c *ACPConfig) {
				c.PermissionMode = "decision"
				c.DecisionEscalateToHuman = true
				c.RelayTimeout = 0
			},
			wantSub: "relay_timeout",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			tc.mutate(&c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("Validate() accepted %+v", c)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not contain %q", err, tc.wantSub)
			}
		})
	}

	// The mode-list error is a user-facing contract: pin it verbatim.
	bad := valid()
	bad.PermissionMode = "ask"
	if err := bad.Validate(); err == nil ||
		err.Error() != `permission_mode must be "permissive", "deny", "decision", or "human"` {
		t.Errorf("permission_mode error = %v, want the four-mode verbatim message", err)
	}
}

func TestConfigValidateAll_ACPWired(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ACP.MaxAgents = 99
	err := cfg.ValidateAll()
	if err == nil {
		t.Fatal("ValidateAll() = nil, want error for bogus acp.max_agents")
	}
	if !strings.Contains(err.Error(), "acp config") {
		t.Fatalf("ValidateAll() error %q must wrap acp config", err)
	}
}
