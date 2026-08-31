package llm

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestResolverQuotaBlock(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	resolver := NewResolver(cfg, logger)

	// Set quota config
	resolver.quotaCfg = &QuotaWaitConfig{
		Enabled:         true,
		MaxWait:         24 * time.Hour,
		DefaultEstimate: 1 * time.Hour,
	}

	// Verify no blocks initially
	blocks := resolver.ActiveQuotaBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks, got %d", len(blocks))
	}

	// Block one model from the existing alias
	blocks = resolver.ActiveQuotaBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks before blocking, got %d", len(blocks))
	}

	// Block via entry
	resolver.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(30*time.Minute))

	blocks = resolver.ActiveQuotaBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].ProviderID != "zai" {
		t.Errorf("expected provider zai, got %s", blocks[0].ProviderID)
	}
	if blocks[0].ModelID != "glm-4.7" {
		t.Errorf("expected model glm-4.7, got %s", blocks[0].ModelID)
	}
}

func TestResolverQuotaCredentialBlock(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	resolver := NewResolver(cfg, logger)

	resolver.quotaCfg = &QuotaWaitConfig{
		Enabled:         true,
		MaxWait:         24 * time.Hour,
		DefaultEstimate: 1 * time.Hour,
	}

	// Block by credential key
	resolver.BlockQuotaCredential("coder", "zai:key:abcd1234", time.Now().Add(1*time.Hour))

	unblock := resolver.QuotaBlockedUntil("zai:key:abcd1234")
	if unblock.IsZero() {
		t.Error("expected non-zero unblock time")
	}
	if unblock.Before(time.Now()) {
		t.Error("expected unblock time to be in the future")
	}
}

func TestResolverQuotaClearAfterReset(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	resolver := NewResolver(cfg, logger)

	resolver.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(-1*time.Hour))

	// Block should have expired
	blocks := resolver.ActiveQuotaBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks after expiry, got %d", len(blocks))
	}
}

func TestResolverQuotaAllBlocked(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	resolver := NewResolver(cfg, logger)

	resolver.quotaCfg = &QuotaWaitConfig{
		Enabled:         true,
		MaxWait:         24 * time.Hour,
		DefaultEstimate: 1 * time.Hour,
	}

	// Block both models in the coder alias
	resolver.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(1*time.Hour))
	resolver.BlockQuotaEntry("coder", "ollama", "llama3.2", time.Now().Add(1*time.Hour))

	if resolver.HasHealthyModels("coder") {
		t.Error("expected HasHealthyModels to be false when all models blocked")
	}
}

func TestResolverQuotaOneBlockStillHealthy(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	resolver := NewResolver(cfg, logger)

	resolver.quotaCfg = &QuotaWaitConfig{
		Enabled:         true,
		MaxWait:         24 * time.Hour,
		DefaultEstimate: 1 * time.Hour,
	}

	// Block only one model
	resolver.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(1*time.Hour))

	// Should still have healthy models (ollama/llama3.2 is available)
	if !resolver.HasHealthyModels("coder") {
		t.Error("expected HasHealthyModels to be true when one model is available")
	}
}
