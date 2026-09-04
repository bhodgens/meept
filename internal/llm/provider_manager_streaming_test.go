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
// streamDeltas (when non-empty) are delivered via onDelta before the
// configured outcome, simulating a provider that streams partial content
// and then fails — the shape that exposed duplicated consumer text across
// PM rotation before attempt-tagged deltas.
type streamingStubChatter struct {
	chatErr      error
	streamOK     *Response
	streamCalled bool
	streamDeltas []string
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
	for _, d := range s.streamDeltas {
		if err := onDelta(d); err != nil {
			return nil, err
		}
	}
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

// type attemptDelta {attempt int; delta string} — delivered events recorded
// by the attempt-reset consumer below.
type attemptDelta struct {
	attempt int
	delta   string
}

// TestProviderManager_StreamingDeltasTaggedWithAttempt pins the
// attempt-tagged-deltas contract (option B "accumulate with attempt
// tagging"): the primary streams N partial deltas then fails; the fallback
// streams its own full text. The attempt-aware consumer sees the attempt
// index increment 0 → 1 exactly at the rotation boundary, and resetting
// accumulation on each increment yields the fallback's text with NO
// attempt-0 residue (the duplication bug this seam replaces).
func TestProviderManager_StreamingDeltasTaggedWithAttempt(t *testing.T) {
	primary := &streamingStubChatter{
		streamDeltas: []string{"par", "tial"},
		chatErr:      &ClientError{Message: "streaming failed after 3 attempts", Cause: errors.New("HTTP 500: rate_limit_check_failed")},
	}
	fallback := &streamingStubChatter{
		streamDeltas: []string{"full ", "text"},
		streamOK:     &Response{Content: "full text", Usage: TokenUsage{TotalTokens: 1}},
	}
	pm := newStreamingTestPM(t, map[string]*streamingStubChatter{
		"primary":  primary,
		"fallback": fallback,
	})

	var events []attemptDelta
	// Mirror the intended consumer pattern: RESET (replace) accumulation
	// when attempt increments; append within an attempt.
	accumulated := ""
	currentAttempt := 0
	resp, err := pm.ChatWithDeltaCallbackWithAttempt(
		context.Background(),
		[]ChatMessage{{Role: "user", Content: "hi"}},
		func(delta string, deltaType string, attempt int) error {
			if deltaType != deltaTypeText {
				t.Errorf("deltaType = %q, want %q", deltaType, deltaTypeText)
			}
			if attempt != currentAttempt {
				// Rotation boundary: reset, never append across attempts.
				accumulated = ""
				currentAttempt = attempt
			}
			accumulated += delta
			events = append(events, attemptDelta{attempt: attempt, delta: delta})
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ChatWithDeltaCallbackWithAttempt: %v", err)
	}
	if resp == nil || resp.Content != "full text" {
		t.Fatalf("response = %+v, want fallback content %q", resp, "full text")
	}

	// Attempt 0 saw the primary's partial deltas, attempt 1 the fallback's.
	want := []attemptDelta{
		{attempt: 0, delta: "par"},
		{attempt: 0, delta: "tial"},
		{attempt: 1, delta: "full "},
		{attempt: 1, delta: "text"},
	}
	if len(events) != len(want) {
		t.Fatalf("deltas = %+v, want %+v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("deltas[%d] = %+v, want %+v", i, events[i], want[i])
		}
	}

	// The reset-on-rotation accumulation equals the surviving attempt's
	// text exactly — no attempt-0 residue ("partialfull text" would be the
	// append-across-attempts bug).
	if accumulated != "full text" {
		t.Errorf("accumulated = %q, want %q (reset-on-rotation must drop attempt-0 partials)", accumulated, "full text")
	}
}

// TestProviderManager_StreamingPlainCallbackSingleAttempt pins the
// byte-compatibility half of the seam: with NO rotation (primary succeeds
// mid-stream), the plain onDelta path receives every delta unchanged —
// attempt tagging is invisible to direct-Client-style consumers.
func TestProviderManager_StreamingPlainCallbackSingleAttempt(t *testing.T) {
	primary := &streamingStubChatter{
		streamDeltas: []string{"he", "llo"},
		streamOK:     &Response{Content: "hello", Usage: TokenUsage{TotalTokens: 1}},
	}
	fallback := &streamingStubChatter{
		streamDeltas: []string{"SHOULD NOT STREAM"},
		streamOK:     &Response{Content: "fallback", Usage: TokenUsage{TotalTokens: 1}},
	}
	pm := newStreamingTestPM(t, map[string]*streamingStubChatter{
		"primary":  primary,
		"fallback": fallback,
	})

	var got []string
	resp, err := pm.ChatWithDeltaCallback(
		context.Background(),
		[]ChatMessage{{Role: "user", Content: "hi"}},
		func(delta string) error {
			got = append(got, delta)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback: %v", err)
	}
	if resp == nil || resp.Content != "hello" {
		t.Fatalf("response = %+v, want primary content", resp)
	}
	if len(got) != 2 || got[0] != "he" || got[1] != "llo" {
		t.Errorf("deltas = %v, want [he llo] unchanged", got)
	}
	if fallback.streamCalled {
		t.Errorf("fallback streamed despite healthy primary — rotation must not fire on success")
	}
}
