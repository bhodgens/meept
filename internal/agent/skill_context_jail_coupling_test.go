package agent

// skill_context_jail_coupling_test.go — bughunt H5 coupling pin: the
// EAGER-BODIES default (H5 flip) and the per-session jail must stay
// compatible ON THE REAL ARM PATH. The index-only default this replaces
// pointed the model at file_read paths OUTSIDE the jail; eager bodies
// load through the skill loader (not file_read), so a default-config,
// FENCED loop must still refuse an out-of-root file_read the prompt
// might name — the jail boundary does not depend on the skill posture.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/pkg/security"
)

func TestEagerBodiesDefault_KeepsJailOnFileRead(t *testing.T) {
	jail := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "skill.md")

	// Default config = eager bodies (H5). The loop carries the fenced
	// checker exactly the way the voice/employee manager arm installs it
	// (composeLoopFence over the daemon mint).
	cfg := DefaultAgentConfig()
	if cfg.SkillContextIndexOnly {
		t.Fatal("default config must be eager bodies (SkillContextIndexOnly zero-value false)")
	}
	loop := NewAgentLoop("h5-jail-coupling", jail, WithAgentConfig(cfg))
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	// Base checker the way the manager arm installs it (fresh empty base
	// when the template carries none — fail closed).
	base := loop.Security()
	if base == nil {
		base = security.NewPermissionChecker(security.Config{})
	}
	fenced := composeLoopFence(base, newSessionFenceFactory(testLoggerForFence()), jail, false, "", loop.GroupResolution())
	if fenced == nil || fenced == base {
		t.Fatal("composeLoopFence must install a fenced clone")
	}
	loop.SetSecurityChecker(fenced)

	// Eager bodies on: an out-of-jail file_read is still refused through
	// the REAL central pipeline (the tool layer's entry point).
	got := loop.Security().CheckPermissionForAgent("file_read", map[string]string{"path": outsideFile}, "")
	if got.Allowed {
		t.Fatalf("eager-bodies default must NOT relax the jail: out-of-root file_read allowed: %+v", got)
	}
	if got.RuleSource != "fence" {
		t.Fatalf("expected a fence denial, got rule source %q (%s)", got.RuleSource, got.Reason)
	}
	// In-jail reads stay open.
	inside := loop.Security().CheckPermissionForAgent("file_read", map[string]string{"path": filepath.Join(jail, "ok.md")}, "")
	if !inside.Allowed {
		t.Fatalf("in-jail file_read denied under the eager default: %+v", inside)
	}
	_ = context.Background()
}
