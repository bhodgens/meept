package security

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func TestNewEngine(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh:     true,
		RequireConfirmationCritical: true,
		BlockFinancial:              true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Check database was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}
}

func TestEngineCheckBasicAction(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh:     true,
		RequireConfirmationCritical: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Test safe action
	decision := engine.Check("file_read", "file_read", nil, "")
	if !decision.Allowed {
		t.Errorf("file_read should be allowed, got: %+v", decision)
	}
	if decision.RiskLevel != RiskSafe {
		t.Errorf("file_read risk level should be SAFE, got: %s", decision.RiskLevel.String())
	}
}

func TestEngineCheckHighRiskAction(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh:     true,
		RequireConfirmationCritical: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Test high-risk action
	decision := engine.Check("file_delete", "file_delete", map[string]string{"path": "/tmp/test.txt"}, "")
	if decision.Allowed {
		t.Errorf("file_delete should require confirmation, got: %+v", decision)
	}
	if !decision.RequiresConfirmation {
		t.Errorf("file_delete should require confirmation")
	}
}

func TestEngineCheckDangerousCommand(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh:     true,
		RequireConfirmationCritical: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	tests := []struct {
		name        string
		command     string
		wantAllowed bool
		wantRisk    RiskLevel
	}{
		{
			name:        "rm -rf",
			command:     "rm -rf /tmp/test",
			wantAllowed: false,
			wantRisk:    RiskHigh,
		},
		{
			name:        "sudo",
			command:     "sudo apt update",
			wantAllowed: false,
			wantRisk:    RiskHigh,
		},
		{
			name:        "simple ls",
			command:     "ls -la",
			wantAllowed: true,
			wantRisk:    RiskLow,
		},
		{
			name:        "git status",
			command:     "git status",
			wantAllowed: true,
			wantRisk:    RiskLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := engine.Check("shell_execute", "shell", map[string]string{"command": tt.command}, "")
			if decision.Allowed != tt.wantAllowed {
				t.Errorf("command %q: allowed = %v, want %v (reason: %s)", tt.command, decision.Allowed, tt.wantAllowed, decision.Reason)
			}
			if decision.RiskLevel != tt.wantRisk {
				t.Errorf("command %q: risk = %s, want %s", tt.command, decision.RiskLevel.String(), tt.wantRisk.String())
			}
		})
	}
}

func TestEngineCheckImmutableCommand(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh:     true,
		RequireConfirmationCritical: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Test immutable command (rm -rf /)
	decision := engine.Check("shell_execute", "shell", map[string]string{"command": "rm -rf /"}, "")
	if decision.Allowed {
		t.Errorf("rm -rf / should be denied")
	}
	if decision.RuleSource != "immutable" {
		t.Errorf("rm -rf / should be denied by immutable rule, got: %s", decision.RuleSource)
	}
}

func TestEngineCheckFinancial(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		BlockFinancial: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Test financial operation detection
	decision := engine.Check("send_message", "send_message", map[string]string{
		"content": "Please transfer funds to my bank account",
	}, "")
	if decision.Allowed {
		t.Errorf("Financial operations should be blocked, got: %+v", decision)
	}
	if decision.RiskLevel != RiskCritical {
		t.Errorf("Financial operations should have CRITICAL risk level")
	}
}

func TestEngineRecordOverride(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Record an allow override
	id, err := engine.AllowOnce("shell_execute", "*pip*", "Approved by user", 10, 7)
	if err != nil {
		t.Fatalf("AllowOnce failed: %v", err)
	}
	if id == 0 {
		t.Error("Override ID should not be zero")
	}

	// Check that the override is applied
	decision := engine.Check("shell_execute", "shell", map[string]string{"command": "pip install requests"}, "")
	if !decision.Allowed {
		t.Errorf("pip install should be allowed with override, got: %+v", decision)
	}
	if !decision.OverrideApplied {
		t.Error("Override should be marked as applied")
	}
}

func TestEngineBlockAction(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Record a block override
	// SEC-3 fix: Use proper glob pattern that matches entire value (filepath.Match semantics)
	_, err = engine.BlockAction("network_request", "https://dangerous.com/*", "Blocked by admin")
	if err != nil {
		t.Fatalf("BlockAction failed: %v", err)
	}

	// Check that the block is applied
	decision := engine.Check("network_request", "network", map[string]string{"url": "https://dangerous.com/malware"}, "")
	if decision.Allowed {
		t.Errorf("Request to dangerous.com should be blocked, got: %+v", decision)
	}
	if !decision.OverrideApplied {
		t.Error("Override should be marked as applied")
	}
}

func TestEngineGetContextForLLM(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	engine, err := NewEngine(dbPath, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	decision := Decision{
		Allowed:    false,
		Reason:     "Test denial reason",
		RiskLevel:  RiskHigh,
		RuleSource: "test_rule",
	}

	context := engine.GetContextForLLM(decision, "shell_execute", map[string]string{"command": "rm -rf /"})

	if context == "" {
		t.Error("Context should not be empty")
	}
	if !contains(context, "shell_execute") {
		t.Error("Context should contain action")
	}
	if !contains(context, "HIGH") {
		t.Error("Context should contain risk level")
	}
	if !contains(context, "Denied") {
		t.Error("Context should indicate denial")
	}
}

func TestEngineConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Test concurrent access
	done := make(chan bool)
	for range 10 {
		go func() {
			for range 100 {
				_ = engine.Check("file_read", "file_read", nil, "")
				_ = engine.Check("shell_execute", "shell", map[string]string{"command": "ls"}, "")
			}
			done <- true
		}()
	}

	for range 10 {
		<-done
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestCheckPath_FailClosedOnDBError verifies that when the path rule query
// fails (e.g. the DB has been closed), checkPath returns a deny Decision
// instead of nil (which callers would treat as "allow").
func TestCheckPath_FailClosedOnDBError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh:     true,
		RequireConfirmationCritical: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Close the DB out from under the engine so subsequent queries fail.
	if err := engine.db.Close(); err != nil {
		t.Fatalf("failed to close DB: %v", err)
	}

	decision := engine.checkPath("/tmp/should-not-matter", "file_read")
	if decision == nil {
		t.Fatal("checkPath returned nil on DB error; expected fail-closed deny Decision")
	}
	if decision.Allowed {
		t.Errorf("checkPath should deny on DB error, got allowed=true: %+v", decision)
	}
	if decision.RuleSource != "fail_closed" {
		t.Errorf("expected RuleSource=fail_closed, got %q", decision.RuleSource)
	}
}

// TestEngineCheckFinancial_DisabledByConfig verifies that when BlockFinancial is false,
// financial operations are not blocked. (SEC-1 fix verification)
func TestEngineCheckFinancial_DisabledByConfig(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	// BlockFinancial is explicitly false
	cfg := &config.SecurityConfig{
		BlockFinancial: false,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Financial operations should NOT be blocked when BlockFinancial is false
	decision := engine.Check("send_message", "send_message", map[string]string{
		"content": "Please transfer funds to my bank account",
	}, "")

	// Since BlockFinancial is false, financial check should pass through
	// The action will be evaluated by other rules but not blocked by financial check
	if decision.RuleSource == "immutable" && decision.Reason == "Financial operations are blocked by policy" {
		t.Errorf("Financial operations should NOT be blocked when BlockFinancial=false, got: %+v", decision)
	}
}

// TestEngineCheckFinancial_NilConfig verifies that when config is nil,
// financial operations are not blocked. (SEC-1 fix verification)
func TestEngineCheckFinancial_NilConfig(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	// Pass nil config
	engine, err := NewEngine(dbPath, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Financial operations should NOT be blocked when config is nil
	decision := engine.Check("send_message", "send_message", map[string]string{
		"content": "Please transfer funds to my bank account",
	}, "")

	// Since config is nil, financial check should pass through
	if decision.RuleSource == "immutable" && decision.Reason == "Financial operations are blocked by policy" {
		t.Errorf("Financial operations should NOT be blocked when config is nil, got: %+v", decision)
	}
}

// TestCheckPath_PathTraversalBypass verifies that path traversal attacks are blocked.
// /tmp_backup/secret should NOT match allow rule for /tmp (SEC-4 fix verification)
func TestCheckPath_PathTraversalBypass(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Add an allow rule for /tmp
	_, err = engine.db.Exec(`
		INSERT INTO path_rules (pattern, rule_type, risk_level, description, immutable, enabled)
		VALUES ('/tmp', 'allow', 1, 'Allow temp directory', 0, 1)`)
	if err != nil {
		t.Fatalf("Failed to add allow rule: %v", err)
	}

	// /tmp/test should be allowed (it's under /tmp)
	decision := engine.checkPath("/tmp/test", "file_read")
	if decision != nil {
		t.Errorf("/tmp/test should be allowed under /tmp rule, got: %+v", decision)
	}

	// /tmp_backup/secret should NOT be allowed (it's not under /tmp, just has /tmp as prefix)
	decision = engine.checkPath("/tmp_backup/secret", "file_read")
	if decision == nil {
		t.Errorf("/tmp_backup/secret should NOT be allowed - it's not under /tmp directory")
	}
}

// TestCheckPath_BlockedPathTraversal verifies that blocked path rules also work correctly.
// /etc_backup should NOT be blocked by rule for /etc (SEC-4 fix verification)
func TestCheckPath_BlockedPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// /etc/passwd should be blocked (it's under /etc which is blocked by default seeds)
	decision := engine.checkPath("/etc/passwd", "file_read")
	if decision == nil || decision.Allowed {
		t.Errorf("/etc/passwd should be blocked, got: %+v", decision)
	}

	// /etc_backup/passwd should NOT be blocked by the /etc rule
	// since /etc_backup is a different directory
	decision = engine.checkPath("/etc_backup/passwd", "file_read")
	// This should either be nil (no rule matches) or allowed
	if decision != nil && !decision.Allowed {
		// Check if it was blocked by the /etc pattern incorrectly
		if contains(decision.Reason, "/etc") {
			t.Errorf("/etc_backup/passwd should NOT be blocked by /etc rule - different directory, got: %+v", decision)
		}
	}
}

// TestNormalizePathForComparison tests the path normalization helper.
func TestNormalizePathForComparison(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/tmp", "/tmp/"},
		{"/tmp/", "/tmp/"},
		{"/var/log", "/var/log/"},
		{"", ""},
	}

	for _, tt := range tests {
		result := normalizePathForComparison(tt.input)
		if result != tt.expected {
			t.Errorf("normalizePathForComparison(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestIsPathUnderDir tests the directory containment helper.
func TestIsPathUnderDir(t *testing.T) {
	tests := []struct {
		path     string
		dir      string
		expected bool
	}{
		{"/tmp/test", "/tmp", true},
		{"/tmp/sub/file", "/tmp", true},
		{"/tmp", "/tmp", true},
		{"/tmp_backup", "/tmp", false},
		{"/tmp_backup/secret", "/tmp", false},
		{"/var/log", "/var", true},
		{"/variable", "/var", false},
	}

	for _, tt := range tests {
		result := isPathUnderDir(tt.path, tt.dir)
		if result != tt.expected {
			t.Errorf("isPathUnderDir(%q, %q) = %v, want %v", tt.path, tt.dir, result, tt.expected)
		}
	}
}
