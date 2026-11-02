package security

import (
	"testing"
)

// backstopCfg builds an armed backstop over a fake home. agent-update is
// the default admin; boss (user) identity is irrelevant here (the file
// lane has no identity, only agent ids).
func backstopCfg() GroupsBackstopConfig {
	return GroupsBackstopConfig{
		Enabled:   true,
		Home:      "/home/u/.meept",
		UsersFile: "/home/u/.meept/users.json5",
		AdminAgents: map[string]struct{}{
			"agent-update": {},
		},
	}
}

// T3 PASS state: bypass attempt via the file tool denied for a non-admin
// agent; admin agent unaffected; stage inert when disarmed.
func TestGroupsBackstop(t *testing.T) {
	cfg := backstopCfg()

	tests := []struct {
		name     string
		agentID  string
		path     string
		wantDeny bool
	}{
		{
			name:     "non-admin write to users store denied",
			agentID:  "aastub",
			path:     "/home/u/.meept/users.json5",
			wantDeny: true,
		},
		{
			name:     "non-admin write into agents dir denied",
			agentID:  "aastub",
			path:     "/home/u/.meept/agents/coder/AGENT.md",
			wantDeny: true,
		},
		{
			name:     "non-admin write to main config denied",
			agentID:  "aastub",
			path:     "/home/u/.meept/meept.json5",
			wantDeny: true,
		},
		{
			name:     "admin agent keeps its tools (users store)",
			agentID:  "agent-update",
			path:     "/home/u/.meept/users.json5",
			wantDeny: false,
		},
		{
			name:     "admin agent keeps its tools (agents dir)",
			agentID:  "agent-update",
			path:     "/home/u/.meept/agents/coder/AGENT.md",
			wantDeny: false,
		},
		{
			name:     "unrelated path unaffected",
			agentID:  "aastub",
			path:     "/home/u/work/project/file.txt",
			wantDeny: false,
		},
		{
			name:     "empty agent id fails closed on protected path",
			agentID:  "",
			path:     "/home/u/.meept/users.json5",
			wantDeny: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cfg.check(tt.agentID, tt.path)
			if tt.wantDeny && err == nil {
				t.Fatalf("check(%s, %s): want deny, got nil", tt.agentID, tt.path)
			}
			if !tt.wantDeny && err != nil {
				t.Fatalf("check(%s, %s): want allow, got %v", tt.agentID, tt.path, err)
			}
		})
	}

	// Shell lane: literal target reference denied for non-admin, allowed
	// for admin.
	if err := cfg.checkCommand("aastub", "cat /home/u/.meept/users.json5"); err == nil {
		t.Fatalf("shell probe on users store: want deny for non-admin")
	}
	if err := cfg.checkCommand("agent-update", "cat /home/u/.meept/users.json5"); err != nil {
		t.Fatalf("shell probe by admin: want allow, got %v", err)
	}

	// Disarmed stage: nothing denied (legacy behavior preserved).
	off := cfg
	off.Enabled = false
	if err := off.check("aastub", "/home/u/.meept/users.json5"); err != nil {
		t.Fatalf("disarmed stage must allow, got %v", err)
	}
}

// The stage fires inside CheckPermissionForAgent with rule_source pinned
// (the F4 fixture path: policy allows file_write, backstop denies).
func TestGroupsBackstopStageInPermissionPipeline(t *testing.T) {
	pc := NewPermissionChecker(Config{
		GroupsBackstop: backstopCfg(),
	})
	res := pc.CheckPermissionForAgent("file_write", map[string]string{
		"path":      "/home/u/.meept/users.json5",
		"agent_id":  "aastub",
		"tool_name": "file_write",
	}, "aastub")
	if res.Allowed {
		t.Fatalf("backstop stage: non-admin users-store write allowed through the pipeline")
	}
	if res.RuleSource != "groups_backstop" {
		t.Fatalf("rule_source = %q, want groups_backstop", res.RuleSource)
	}

	// Admin exemption through the full pipeline.
	res = pc.CheckPermissionForAgent("file_write", map[string]string{
		"path":      "/home/u/.meept/users.json5",
		"agent_id":  "agent-update",
		"tool_name": "file_write",
	}, "agent-update")
	if !res.Allowed {
		t.Fatalf("backstop stage locked OUT the admin: %s", res.Reason)
	}
}
