package llm

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// streamingStubChatter satisfies Chatter + StreamingChatter. Its Chat /
// ChatWithDeltaCallback return the configured error (or success response);
// streamCalled records whether the streaming path was used, so tests can
// assert which candidate actually served the streaming request.
type streamingStubChatter struct {
	chatErr      error
	streamOK     *Response
	streamCalled bool
}

func (s *streamingStubChatter) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	if s.chatErr != nil {
		return nil, s.chatErr
	}
	return s.streamOK, nil
}

func (s *streamingStubChatter) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *streamingStubChatter) ChatWithDeltaCallback(ctx context.Context, messages []ChatMessage, onDelta DeltaCallback, opts ...ChatOption) (*Response, error) {
	s.streamCalled = true
	if s.chatErr != nil {
		return nil, s.chatErr
	}
	return s.streamOK, nil
}

func (s *streamingStubChatter) Config() *ModelConfig {
	return &ModelConfig{ModelID: "stub"}
}

// newStreamingTestPM builds a ProviderManager whose named providers are
// backed by the supplied stub chatters (bypassing createChatterFor, which
// would build real HTTP clients).
func newStreamingTestPM(t *testing.T, stubs map[string]*streamingStubChatter) *ProviderManager {
	t.Helper()
	pm := NewProviderManager(ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "primary", BaseURL: "http://primary/v1", ModelID: "m1"},
			{ProviderID: "fallback", BaseURL: "http://fallback/v1", ModelID: "m2"},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	for _, entry := range pm.providers {
		stub, ok := stubs[entry.Config.ProviderID]
		if !ok {
			t.Fatalf("no stub configured for provider %s", entry.Config.ProviderID)
		}
		entry.Chatter = stub
	}
	return pm
}

func findEntry(pm *ProviderManager, providerID string) *ProviderEntry {
	for _, e := range pm.providers {
		if e.Config.ProviderID == providerID {
			return e
		}
	}
	return nil
}

// TestProviderManager_StreamingRotatesOnServerError: the primary's streaming
// call fails with a server error (the observed agnes 500 rate-limit-check
// shape); the manager must rotate to the healthy fallback rather than fail
// the turn.
func TestProviderManager_StreamingRotatesOnServerError(t *testing.T) {
	primary := &streamingStubChatter{
		chatErr: &ClientError{Message: "streaming failed after 3 attempts", Cause: errors.New("HTTP 500: rate_limit_check_failed")},
	}
	fallback := &streamingStubChatter{
		streamOK: &Response{Content: "ok", Usage: TokenUsage{TotalTokens: 1}},
	}
	pm := newStreamingTestPM(t, map[string]*streamingStubChatter{
		"primary":  primary,
		"fallback": fallback,
	})

	resp, err := pm.ChatWithDeltaCallback(
		context.Background(),
		[]ChatMessage{{Role: "user", Content: "hi"}},
		func(string) error { return nil },
	)
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback: %v", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("response = %+v, want fallback content", resp)
	}
	if primary.streamCalled && fallback.streamCalled {
		// Correct rotation shape: primary attempted, failed, fallback served.
	} else if !primary.streamCalled {
		t.Errorf("primary streaming not attempted — rotation should try the primary first")
	}
}

// TestProviderManager_StreamingQuotaBlocksCredential: a quota error on the
// primary's streaming call blocks the credential and rotates, mirroring
// Chat()'s quota semantics.
func TestProviderManager_StreamingQuotaBlocksCredential(t *testing.T) {
	quotaErr := &QuotaResetError{
		Message: "quota",
		ResetAt: time.Now().Add(time.Hour),
	}
	primary := &streamingStubChatter{chatErr: quotaErr}
	fallback := &streamingStubChatter{
		streamOK: &Response{Content: "ok", Usage: TokenUsage{TotalTokens: 1}},
	}
	pm := newStreamingTestPM(t, map[string]*streamingStubChatter{
		"primary":  primary,
		"fallback": fallback,
	})

	resp, err := pm.ChatWithDeltaCallback(
		context.Background(),
		[]ChatMessage{{Role: "user", Content: "hi"}},
		func(string) error { return nil },
	)
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback: %v", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("response = %+v, want fallback content", resp)
	}

	primaryEntry := findEntry(pm, "primary")
	if primaryEntry == nil {
		t.Fatalf("primary provider entry missing")
	}
	key := QuotaCredentialKey(primaryEntry.Config.ProviderID, primaryEntry.Config)
	pm.mu.RLock()
	_, blocked := pm.quotaBlocks[key]
	pm.mu.RUnlock()
	if !blocked {
		t.Errorf("primary credential not quota-blocked after QuotaResetError")
	}
}
