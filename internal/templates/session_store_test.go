package templates

import (
	"strings"
	"testing"
	"time"
)

func TestSessionStore_Activate(t *testing.T) {
	store := NewSessionStore()

	active := ActiveTemplate{
		Name:            "role-senior-dev",
		SubstitutedBody: "You are a senior Go developer.",
		ActivatedAt:     time.Now(),
		CharCount:       30,
	}

	err := store.Activate("conv-1", active)
	if err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	got := store.GetActive("conv-1")
	if len(got) != 1 {
		t.Fatalf("GetActive returned %d templates, want 1", len(got))
	}

	if got[0].Name != "role-senior-dev" {
		t.Errorf("Name = %q, want role-senior-dev", got[0].Name)
	}
}

func TestSessionStore_ActivateReplaceExisting(t *testing.T) {
	store := NewSessionStore()

	active1 := ActiveTemplate{
		Name:            "role-dev",
		SubstitutedBody: "You are a developer.",
		ActivatedAt:     time.Now(),
		CharCount:       20,
	}

	active2 := ActiveTemplate{
		Name:            "role-dev",
		SubstitutedBody: "You are a senior developer.",
		ActivatedAt:     time.Now(),
		CharCount:       28,
	}

	if err := store.Activate("conv-1", active1); err != nil {
		t.Fatalf("First activate failed: %v", err)
	}
	if err := store.Activate("conv-1", active2); err != nil {
		t.Fatalf("Second activate failed: %v", err)
	}

	got := store.GetActive("conv-1")
	if len(got) != 1 {
		t.Errorf("GetActive returned %d templates, want 1 (replaced)", len(got))
	}

	if got[0].SubstitutedBody != "You are a senior developer." {
		t.Errorf("SubstitutedBody = %q, want updated version", got[0].SubstitutedBody)
	}
}

func TestSessionStore_ActivateMaxTemplates(t *testing.T) {
	store := NewSessionStore()

	// Fill up to the max.
	for i := range MaxSessionScopedTemplates {
		active := ActiveTemplate{
			Name:            "template-" + string(rune('a'+i)),
			SubstitutedBody: "body",
			ActivatedAt:     time.Now(),
			CharCount:       10,
		}
		if err := store.Activate("conv-1", active); err != nil {
			t.Fatalf("Activate %d failed: %v", i, err)
		}
	}

	// One more should fail.
	active := ActiveTemplate{
		Name:            "template-extra",
		SubstitutedBody: "extra",
		ActivatedAt:     time.Now(),
		CharCount:       10,
	}
	err := store.Activate("conv-1", active)
	if err == nil {
		t.Error("Expected error when exceeding max templates")
	}

	if !strings.Contains(err.Error(), "maximum") {
		t.Errorf("Error should mention maximum, got: %v", err)
	}
}

func TestSessionStore_ActivateMaxChars(t *testing.T) {
	store := NewSessionStore()

	// Activate a template that nearly fills the char limit.
	bigActive := ActiveTemplate{
		Name:            "big-template",
		SubstitutedBody: strings.Repeat("x", MaxSessionScopedCharsTotal-10),
		ActivatedAt:     time.Now(),
		CharCount:       MaxSessionScopedCharsTotal - 10,
	}
	if err := store.Activate("conv-1", bigActive); err != nil {
		t.Fatalf("Activate big template failed: %v", err)
	}

	// Another template that would exceed the limit should fail.
	tooBig := ActiveTemplate{
		Name:            "too-big",
		SubstitutedBody: "this is 20 chars..",
		ActivatedAt:     time.Now(),
		CharCount:       20,
	}
	err := store.Activate("conv-1", tooBig)
	if err == nil {
		t.Error("Expected error when exceeding max chars")
	}

	if !strings.Contains(err.Error(), "total characters") {
		t.Errorf("Error should mention characters, got: %v", err)
	}
}

func TestSessionStore_Deactivate(t *testing.T) {
	store := NewSessionStore()

	active := ActiveTemplate{
		Name:            "role-dev",
		SubstitutedBody: "You are a developer.",
		ActivatedAt:     time.Now(),
		CharCount:       20,
	}
	if err := store.Activate("conv-1", active); err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	removed := store.Deactivate("conv-1", "role-dev")
	if !removed {
		t.Error("Deactivate should return true for existing template")
	}

	got := store.GetActive("conv-1")
	if got != nil {
		t.Errorf("GetActive should return nil after deactivation, got %v", got)
	}
}

func TestSessionStore_DeactivateCaseInsensitive(t *testing.T) {
	store := NewSessionStore()

	active := ActiveTemplate{
		Name:            "Role-Dev",
		SubstitutedBody: "You are a developer.",
		ActivatedAt:     time.Now(),
		CharCount:       20,
	}
	if err := store.Activate("conv-1", active); err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	removed := store.Deactivate("conv-1", "role-dev")
	if !removed {
		t.Error("Deactivate should match case-insensitively")
	}
}

func TestSessionStore_DeactivateNonExistent(t *testing.T) {
	store := NewSessionStore()

	removed := store.Deactivate("conv-1", "nonexistent")
	if removed {
		t.Error("Deactivate should return false for nonexistent template")
	}
}

func TestSessionStore_Clear(t *testing.T) {
	store := NewSessionStore()

	for i := range 3 {
		active := ActiveTemplate{
			Name:            "template-" + string(rune('a'+i)),
			SubstitutedBody: "body",
			ActivatedAt:     time.Now(),
			CharCount:       10,
		}
		if err := store.Activate("conv-1", active); err != nil {
			t.Fatalf("Activate failed: %v", err)
		}
	}

	names := store.Clear("conv-1")
	if len(names) != 3 {
		t.Errorf("Clear returned %d names, want 3", len(names))
	}

	got := store.GetActive("conv-1")
	if got != nil {
		t.Error("GetActive should return nil after clear")
	}
}

func TestSessionStore_ClearNonExistent(t *testing.T) {
	store := NewSessionStore()

	names := store.Clear("nonexistent-conv")
	if names != nil {
		t.Errorf("Clear should return nil for nonexistent conversation, got %v", names)
	}
}

func TestSessionStore_GetActiveIsolation(t *testing.T) {
	store := NewSessionStore()

	active := ActiveTemplate{
		Name:            "role-dev",
		SubstitutedBody: "You are a developer.",
		ActivatedAt:     time.Now(),
		CharCount:       20,
	}
	if err := store.Activate("conv-1", active); err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	// Getting active for a different conversation should return nil.
	got := store.GetActive("conv-2")
	if got != nil {
		t.Error("GetActive should return nil for conversation with no active templates")
	}
}

func TestSessionStore_ContextString(t *testing.T) {
	store := NewSessionStore()

	ts := time.Now()
	active := ActiveTemplate{
		Name:            "role-dev",
		SubstitutedBody: "You are a senior Go developer.",
		ActivatedAt:     ts,
		CharCount:       30,
	}
	if err := store.Activate("conv-1", active); err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	ctx := store.ContextString("conv-1")
	if ctx == "" {
		t.Fatal("ContextString should not be empty with active templates")
	}

	if !strings.Contains(ctx, "<template-context>") {
		t.Error("ContextString should contain <template-context> tag")
	}
	if !strings.Contains(ctx, "</template-context>") {
		t.Error("ContextString should contain </template-context> tag")
	}
	if !strings.Contains(ctx, "role-dev") {
		t.Error("ContextString should contain template name")
	}
	if !strings.Contains(ctx, "You are a senior Go developer.") {
		t.Error("ContextString should contain substituted body")
	}
}

func TestSessionStore_ContextStringEmpty(t *testing.T) {
	store := NewSessionStore()

	ctx := store.ContextString("nonexistent")
	if ctx != "" {
		t.Errorf("ContextString should return empty string for no templates, got %q", ctx)
	}
}

func TestSessionStore_MultipleConversations(t *testing.T) {
	store := NewSessionStore()

	for _, convID := range []string{"conv-1", "conv-2"} {
		active := ActiveTemplate{
			Name:            "role-dev",
			SubstitutedBody: "You are a developer for " + convID,
			ActivatedAt:     time.Now(),
			CharCount:       20,
		}
		if err := store.Activate(convID, active); err != nil {
			t.Fatalf("Activate for %s failed: %v", convID, err)
		}
	}

	// Each conversation should have its own templates.
	got1 := store.GetActive("conv-1")
	got2 := store.GetActive("conv-2")

	if len(got1) != 1 || got1[0].SubstitutedBody != "You are a developer for conv-1" {
		t.Errorf("conv-1 templates incorrect: %v", got1)
	}
	if len(got2) != 1 || got2[0].SubstitutedBody != "You are a developer for conv-2" {
		t.Errorf("conv-2 templates incorrect: %v", got2)
	}

	// Clearing one should not affect the other.
	store.Clear("conv-1")
	if store.GetActive("conv-2") == nil {
		t.Error("Clearing conv-1 should not affect conv-2")
	}
}
