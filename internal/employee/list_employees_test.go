package employee

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bot"
)

// TestListEmployees_ExcludesPlainBots proves the root-cause fix: ListEmployees
// returns only constitution-backed employees, not every bot row. A plain bot
// registered directly in the bot store has no constitution and must be absent
// from the employee list, while a bot hired via Hire (which writes a
// constitution) is present. The plain bot row itself must remain reachable via
// botManager.ListBots — nothing is deleted.
func TestListEmployees_ExcludesPlainBots(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "bots.db")
	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { _ = botStore.Close() })
	constStore, err := NewConstitutionStore(dbPath, nil)
	if err != nil {
		t.Fatalf("constitution store: %v", err)
	}
	t.Cleanup(func() { _ = constStore.Close() })
	goalStore, err := NewGoalStore(dbPath, nil)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	t.Cleanup(func() { _ = goalStore.Close() })
	auditStore, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("audit store: %v", err)
	}
	t.Cleanup(func() { _ = auditStore.Close() })

	botManager := bot.NewManager(botStore, nil)
	mgr := NewManagerWithStores(botManager, botStore, constStore, goalStore, auditStore, nil)

	// A plain bot: a definition row with no constitution.
	const plainID = "plain-bot"
	if err := botStore.Create(ctx, bot.BotDefinition{
		ID:      plainID,
		Name:    "plain bot",
		Prompt:  "plain bot with no constitution",
		Enabled: true,
	}); err != nil {
		t.Fatalf("create plain bot: %v", err)
	}

	// A hired employee: Hire writes a real constitution.
	const employeeID = "hired-employee"
	c := validConstitution()
	c.EscalatesTo = []string{"user"}
	if _, err := mgr.Hire(ctx, HireRequest{
		ID:           employeeID,
		Name:         "hired employee",
		Prompt:       "hired employee prompt",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, c),
	}); err != nil {
		t.Fatalf("hire employee: %v", err)
	}

	// ListEmployees must return ONLY the constitution-backed employee.
	emps, err := mgr.ListEmployees(ctx, "")
	if err != nil {
		t.Fatalf("ListEmployees: %v", err)
	}
	if len(emps) != 1 {
		t.Fatalf("ListEmployees returned %d employees, want 1: %+v", len(emps), emps)
	}
	if emps[0].ID != employeeID {
		t.Errorf("ListEmployees employee id = %q, want %q", emps[0].ID, employeeID)
	}
	if !emps[0].HasConstitution() {
		t.Error("listed employee should have a constitution")
	}

	// The plain bot row must still exist in the bot store (nothing deleted).
	bots, err := botManager.ListBots(ctx)
	if err != nil {
		t.Fatalf("ListBots: %v", err)
	}
	if len(bots) != 2 {
		t.Fatalf("ListBots returned %d bots, want 2 (plain bot + employee)", len(bots))
	}
	found := make(map[string]bool, len(bots))
	for _, b := range bots {
		found[b.ID] = true
	}
	for _, id := range []string{plainID, employeeID} {
		if !found[id] {
			t.Errorf("bot %q missing from ListBots — plain bot must not be deleted", id)
		}
	}
}
