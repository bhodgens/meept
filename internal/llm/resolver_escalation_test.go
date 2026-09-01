package llm

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// Tree 01 leaf 04: ResolveEscalationRef — single resolution entry point for
// escalation targets (alias name or "provider/model" ref). Satisfies
// agent.ModelResolver.

func TestResolveEscalationRef_Alias(t *testing.T) {
	r := NewResolver(createTestConfig(), nil)

	ref, err := r.ResolveEscalationRef("coder")
	if err != nil {
		t.Fatalf("ResolveEscalationRef(coder): %v", err)
	}
	if ref != "zai/glm-4.7" {
		t.Fatalf("alias resolution: got %q, want %q", ref, "zai/glm-4.7")
	}
}

func TestResolveEscalationRef_DirectRef(t *testing.T) {
	r := NewResolver(createTestConfig(), nil)

	ref, err := r.ResolveEscalationRef("ollama/llama3.2")
	if err != nil {
		t.Fatalf("ResolveEscalationRef(ollama/llama3.2): %v", err)
	}
	if ref != "ollama/llama3.2" {
		t.Fatalf("direct ref: got %q, want %q", ref, "ollama/llama3.2")
	}
}

func TestResolveEscalationRef_UnknownAlias(t *testing.T) {
	r := NewResolver(createTestConfig(), nil)

	if _, err := r.ResolveEscalationRef("does-not-exist"); err == nil {
		t.Fatal("unknown alias must error")
	}
}

func TestResolveEscalationRef_UnknownDirectRef(t *testing.T) {
	r := NewResolver(createTestConfig(), nil)

	if _, err := r.ResolveEscalationRef("nope/no-model"); err == nil {
		t.Fatal("unknown provider/model ref must error")
	}
}

func TestResolveEscalationRef_EmptyRef(t *testing.T) {
	r := NewResolver(createTestConfig(), nil)

	if _, err := r.ResolveEscalationRef(""); err == nil {
		t.Fatal("empty ref must error")
	}
}

// A fully quota-blocked alias surfaces ErrAllModelsQuotaBlocked — the loop's
// existing quota handling takes over; no second handling path
// (SHARED-CONVENTIONS §2).
func TestResolveEscalationRef_QuotaBlockedAlias(t *testing.T) {
	// Quota blocking is opt-in via SetQuotaConfig (nil/zero = disabled);
	// reuse the shared quota-test fixture to enable it.
	r := newTestResolver(enabledQuotaCfg())

	r.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(1*time.Hour))
	r.BlockQuotaEntry("coder", "ollama", "llama3.2", time.Now().Add(1*time.Hour))

	if _, err := r.ResolveEscalationRef("coder"); !errors.Is(err, ErrAllModelsQuotaBlocked) {
		t.Fatalf("quota-blocked alias: got %v, want ErrAllModelsQuotaBlocked", err)
	}
}

// Escalation resolutions are recorded in the routing log with reason
// "escalation" (leaf 04 Task 3).
func TestResolveEscalationRef_RoutingLogReason(t *testing.T) {
	logger, err := NewRoutingLogger(filepath.Join(t.TempDir(), "routing.db"), nil)
	if err != nil {
		t.Fatalf("NewRoutingLogger: %v", err)
	}
	defer logger.Close()

	r := NewResolver(createTestConfig(), nil)
	r.SetRoutingLogger(logger)

	if _, err := r.ResolveEscalationRef("ollama/llama3.2"); err != nil {
		t.Fatalf("ResolveEscalationRef: %v", err)
	}

	decisions, err := logger.Recent(context.Background(), 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	for _, d := range decisions {
		if d.Reason == "escalation" {
			if d.ChosenModelID != "llama3.2" || d.ChosenProviderID != "ollama" {
				t.Fatalf("escalation decision: got %s/%s, want ollama/llama3.2", d.ChosenProviderID, d.ChosenModelID)
			}
			return
		}
	}
	t.Fatal("no routing-log row with reason \"escalation\"")
}

// Alias-branch escalations must ALSO land a reason="escalation" routing row
// (D3: alias is the primary escalation style), carrying the alias name so
// escalations are queryable uniformly regardless of target form.
func TestResolveEscalationRef_RoutingLogReason_AliasBranch(t *testing.T) {
	logger, err := NewRoutingLogger(filepath.Join(t.TempDir(), "routing.db"), nil)
	if err != nil {
		t.Fatalf("NewRoutingLogger: %v", err)
	}
	defer logger.Close()

	r := NewResolver(createTestConfig(), nil)
	r.SetRoutingLogger(logger)

	if _, err := r.ResolveEscalationRef("coder"); err != nil {
		t.Fatalf("ResolveEscalationRef(coder): %v", err)
	}

	decisions, err := logger.Recent(context.Background(), 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	for _, d := range decisions {
		if d.Reason == "escalation" {
			if d.Alias != "coder" {
				t.Fatalf("escalation decision alias: got %q, want %q", d.Alias, "coder")
			}
			if d.ChosenModelID != "glm-4.7" || d.ChosenProviderID != "zai" {
				t.Fatalf("escalation decision: got %s/%s, want zai/glm-4.7", d.ChosenProviderID, d.ChosenModelID)
			}
			return
		}
	}
	t.Fatal("no routing-log row with reason \"escalation\" for alias resolution")
}
