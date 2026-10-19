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
