package security

// decision_audit_test.go — RED tests for the decision-audit RuleSource
// stamping (docs/plans/20261030-agent-groups/01-permissions/02-decision-audit.md).
//
// RED reason: CheckResult.RuleSource does not exist yet. Every failure
// below must be the missing-field compile error
// ("unknown field RuleSource in struct literal of type security.CheckResult"),
// never a syntax error.
//
// C2 contract: every CheckResult returned by CheckPermissionForAgent
// carries a rule_source from the FROZEN vocabulary:
//
//	base_rule, group_deny, group_allow, fence, employee_pre_exec,
//	override, confirmation_gate, financial, unknown_action
//
// Denies must never carry an empty rule_source.

import (
	"testing"
)

// ruleSourceVocabulary is the FROZEN C2 vocabulary. It must never grow
// silently — a value outside this set is a contract violation.
var ruleSourceVocabulary = map[string]bool{
	"base_rule":         true,
	"group_deny":        true,
	"group_allow":       true,
	"fence":             true,
	"employee_pre_exec": true,
	"override":          true,
	"confirmation_gate": true,
	"financial":         true,
	"unknown_action":    true,
}

// TestRuleSource_VocabularyFrozen pins the vocabulary membership set
// itself. If the implementation (or a future edit) redefines a value,
// this table is the diff point to review deliberately.
func TestRuleSource_VocabularyFrozen(t *testing.T) {
	want := []string{
		"base_rule", "group_deny", "group_allow", "fence",
		"employee_pre_exec", "override", "confirmation_gate",
		"financial", "unknown_action",
	}
	if len(want) != len(ruleSourceVocabulary) {
		t.Fatalf("vocabulary size drifted: %d entries, want %d", len(ruleSourceVocabulary), len(want))
	}
	for _, v := range want {
		if !ruleSourceVocabulary[v] {
			t.Errorf("vocabulary value %q missing", v)
		}
	}
}

// TestRuleSource_StampedOnEveryReturn walks every builtin action through
// CheckPermissionForAgent and asserts the returned CheckResult carries a
// RuleSource from the frozen vocabulary — non-empty on every deny.
//
// RED: CheckResult.RuleSource absent → compile error at the field access.
func TestRuleSource_StampedOnEveryReturn(t *testing.T) {
	// Two configs exercise different return sites: the plain path
	// (Permitted / Unknown action / financial) and the confirmation-gated
	// path (NeedsConfirm deny).
	configs := []struct {
		name string
		cfg  Config
	}{
		{"plain", Config{
			AllowedPaths:   []string{"/tmp"},
			BlockedPaths:   []string{"/etc"},
			BlockFinancial: true,
		}},
		{"confirmation-gated", Config{
			AllowedPaths:                []string{"/tmp"},
			RequireConfirmationHigh:     true,
			RequireConfirmationCritical: true,
		}},
	}

	tests := []struct {
		name    string
		action  string
		details map[string]string
	}{
		{"file_read allowed", "file_read", map[string]string{"path": "/tmp/a.txt"}},
		{"file_write allowed", "file_write", map[string]string{"path": "/tmp/b.txt"}},
		{"file_delete confirmation deny", "file_delete", map[string]string{"path": "/tmp/c.txt"}},
		{"shell_execute allowed", "shell_execute", map[string]string{"command": "ls -la"}},
		{"shell_execute financial deny", "shell_execute", map[string]string{"command": "transfer funds abroad"}},
		{"file_read blocked path deny", "file_read", map[string]string{"path": "/etc/passwd"}},
		{"file_read outside allowed deny", "file_read", map[string]string{"path": "/var/log/system.log"}},
		{"network_request allowed", "network_request", map[string]string{"url": "https://api.example.com"}},
		{"memory_read allowed", "memory_read", map[string]string{"key": "k"}},
		{"memory_write allowed", "memory_write", map[string]string{"key": "k", "value": "v"}},
		{"platform_read allowed", "platform_read", map[string]string{"scope": "os"}},
		{"task_read allowed", "task_read", map[string]string{"task_id": "t1"}},
		{"task_write allowed", "task_write", map[string]string{"task_id": "t1", "title": "x"}},
		{"agent_delegate allowed", "agent_delegate", map[string]string{"agent": "worker"}},
		{"skill_execute allowed", "skill_execute", map[string]string{"skill": "deploy"}},
		{"generate_image allowed", "generate_image", map[string]string{"prompt": "a lighthouse"}},
		{"generate_video allowed", "generate_video", map[string]string{"prompt": "a wave"}},
		{"send_message allowed", "send_message", map[string]string{"to": "user", "body": "hello"}},
		{"install_package confirmation deny", "install_package", map[string]string{"package": "ripgrep"}},
		{"system_modify confirmation deny", "system_modify", map[string]string{"target": "service"}},
		{"unknown action deny", "definitely_not_an_action", map[string]string{}},
	}

	for _, cfg := range configs {
		pc := NewPermissionChecker(cfg.cfg)
		for _, tt := range tests {
			t.Run(cfg.name+"/"+tt.name, func(t *testing.T) {
				got := pc.CheckPermissionForAgent(tt.action, tt.details, "")
				if got.RuleSource == "" {
					t.Fatalf("CheckPermissionForAgent(%q) returned empty RuleSource (allowed=%v reason=%q)",
						tt.action, got.Allowed, got.Reason)
				}
				if !ruleSourceVocabulary[got.RuleSource] {
					t.Fatalf("RuleSource %q not in frozen vocabulary (action=%q allowed=%v)",
						got.RuleSource, tt.action, got.Allowed)
				}
				if !got.Allowed && got.RuleSource == "" {
					t.Fatalf("deny returned empty rule_source (action=%q reason=%q)", tt.action, got.Reason)
				}
			})
		}
	}
}

// TestRuleSource_DenySources pin the EXPECTED vocabulary value per deny
// return site — the mapping itself, not just membership. These encode C2's
// "groups rule sources have a defined home from day one": today
// group_deny/group_allow cannot fire (no leaf 04 yet) but every value that
// CAN fire must map to the right token.
//
// RED: both .RuleSource field reads are the missing-field compile error.
func TestRuleSource_DenySources(t *testing.T) {
	tests := []struct {
		name           string
		cfg            Config
		action         string
		details        map[string]string
		wantRuleSource string
	}{
		{
			name:           "unknown action",
			cfg:            Config{},
			action:         "no_such_action",
			details:        map[string]string{},
			wantRuleSource: "unknown_action",
		},
		{
			name:           "financial block",
			cfg:            Config{BlockFinancial: true},
			action:         "shell_execute",
			details:        map[string]string{"command": "wire transfer $100"},
			wantRuleSource: "financial",
		},
		{
			name: "confirmation gate",
			cfg: Config{
				RequireConfirmationHigh:     true,
				RequireConfirmationCritical: true,
			},
			action:         "file_delete",
			details:        map[string]string{"path": "/tmp/d.txt"},
			wantRuleSource: "confirmation_gate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := NewPermissionChecker(tt.cfg)
			got := pc.CheckPermissionForAgent(tt.action, tt.details, "")
			if got.Allowed {
				t.Fatalf("expected deny for %s, got allow (reason %q)", tt.action, got.Reason)
			}
			if got.RuleSource != tt.wantRuleSource {
				t.Fatalf("RuleSource = %q, want %q", got.RuleSource, tt.wantRuleSource)
			}
		})
	}
}
