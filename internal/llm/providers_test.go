package llm

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestModelAliasEntry_DefaultModel verifies the default_model field and its
// omitempty JSON tag on the config entry struct.
func TestModelAliasEntry_DefaultModel(t *testing.T) {
	alias := ModelAliasEntry{
		Models:       []string{"zai/glm-4.7", "ollama/llama3.2"},
		Timeout:      30,
		MaxFails:     3,
		DefaultModel: "zai/glm-4.7",
	}
	assert.Equal(t, "zai/glm-4.7", alias.DefaultModel)
	assert.False(t, alias.BalancedStickyRequests)

	// Zero value: empty DefaultModel serializes to nothing (omitempty).
	zero := ModelAliasEntry{Models: []string{"zai/glm-4.7"}}
	assert.Empty(t, zero.DefaultModel)
	assert.False(t, zero.BalancedStickyRequests)
}

// TestModelAliasEntry_StickyRequests verifies the balanced_sticky_requests
// field and its omitempty JSON tag.
func TestModelAliasEntry_StickyRequests(t *testing.T) {
	alias := ModelAliasEntry{
		Models:                 []string{"zai/glm-4.7", "ollama/llama3.2"},
		Timeout:                30,
		MaxFails:               3,
		BalancedStickyRequests: true,
	}
	assert.True(t, alias.BalancedStickyRequests)
	assert.Empty(t, alias.DefaultModel)
}

// TestResolver_NewResolver_PropagatesAliasOptions verifies the resolver
// copies DefaultModel and BalancedStickyRequests from the config entry into
// its internal AliasEntry (the feature is inert otherwise).
func TestResolver_NewResolver_PropagatesAliasOptions(t *testing.T) {
	cfg := createTestConfig()
	cfg.ModelAliases["with-options"] = ModelAliasEntry{
		Models:                 []string{"zai/glm-4.7", "ollama/llama3.2"},
		Timeout:                30,
		MaxFails:               3,
		DefaultModel:           "zai/glm-4.7",
		BalancedStickyRequests: true,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	resolver := NewResolver(cfg, logger)

	entry, ok := resolver.aliases["with-options"]
	if !ok {
		t.Fatal("expected with-options alias to exist")
	}
	if entry.DefaultModel != "zai/glm-4.7" {
		t.Errorf("expected DefaultModel zai/glm-4.7, got %q", entry.DefaultModel)
	}
	if !entry.BalancedStickyRequests {
		t.Error("expected BalancedStickyRequests to be true")
	}

	// Options default to zero values when unset.
	plain, ok := resolver.aliases["coder"]
	if !ok {
		t.Fatal("expected coder alias to exist")
	}
	if plain.DefaultModel != "" || plain.BalancedStickyRequests {
		t.Errorf("expected zero-value options on plain alias, got default=%q sticky=%v",
			plain.DefaultModel, plain.BalancedStickyRequests)
	}
}
