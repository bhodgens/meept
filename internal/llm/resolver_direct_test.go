package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// testFakeChatter is a minimal Chatter implementation for testing.
type testFakeChatter struct {
	callCount int
	responses []struct {
		resp *Response
		err  error
	}
	idx int
}

func (f *testFakeChatter) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	f.callCount++
	if f.idx < len(f.responses) {
		r := f.responses[f.idx]
		f.idx++
		return r.resp, r.err
	}
	return nil, errors.New("no more responses")
}

func (f *testFakeChatter) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	return f.Chat(ctx, messages, opts...)
}

func (f *testFakeChatter) Config() *ModelConfig {
	return &ModelConfig{ModelID: "test", ProviderID: "test"}
}

// TestQuotaWaitChatter_WaitsThenRetries verifies the basic wait+retry path.
func TestQuotaWaitChatter_WaitsThenRetries(t *testing.T) {
	wait := 50 * time.Millisecond
	qe := &QuotaResetError{
		ProviderID: "test",
		ModelID:    "test",
		Code:       "quota_exceeded",
		RetryAfter: wait,
		MaxWait:    24 * time.Hour,
	}

	fc := &testFakeChatter{
		responses: []struct {
			resp *Response
			err  error
		}{
			{nil, qe},
			{&Response{}, nil},
		},
	}

	ch := newQuotaWaitChatter(fc, QuotaWaitConfig{Enabled: true, MaxWait: 24 * time.Hour, DefaultEstimate: time.Hour}, nil)
	start := time.Now()
	resp, err := ch.Chat(context.Background(), nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if fc.callCount != 2 {
		t.Errorf("expected 2 calls, got %d", fc.callCount)
	}
	if elapsed < wait {
		t.Errorf("expected wait >= %v, got %v", wait, elapsed)
	}
}

// TestQuotaWaitChatter_CtxCancelledDuringWait verifies ctx cancellation wins.
func TestQuotaWaitChatter_CtxCancelledDuringWait(t *testing.T) {
	wait := 500 * time.Millisecond
	qe := &QuotaResetError{
		ProviderID: "test",
		ModelID:    "test",
		Code:       "quota_exceeded",
		RetryAfter: wait,
		MaxWait:    24 * time.Hour,
	}

	fc := &testFakeChatter{
		responses: []struct {
			resp *Response
			err  error
		}{
			{nil, qe},
			{&Response{}, nil},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch := newQuotaWaitChatter(fc, QuotaWaitConfig{Enabled: true, MaxWait: 24 * time.Hour, DefaultEstimate: time.Hour}, nil)

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := ch.Chat(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if fc.callCount != 1 {
		t.Errorf("expected 1 call (cancelled before retry), got %d", fc.callCount)
	}
}

// TestQuotaWaitChatter_CallerDeadlineEarlierThanWait verifies caller deadline wins.
func TestQuotaWaitChatter_CallerDeadlineEarlierThanWait(t *testing.T) {
	wait := 500 * time.Millisecond
	qe := &QuotaResetError{
		ProviderID: "test",
		ModelID:    "test",
		Code:       "quota_exceeded",
		RetryAfter: wait,
		MaxWait:    24 * time.Hour,
	}

	fc := &testFakeChatter{
		responses: []struct {
			resp *Response
			err  error
		}{
			{nil, qe},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	ch := newQuotaWaitChatter(fc, QuotaWaitConfig{Enabled: true, MaxWait: 24 * time.Hour, DefaultEstimate: time.Hour}, nil)
	_, err := ch.Chat(ctx, nil)
	// Deadline earlier than wait: return quota error immediately (no retry)
	if !IsQuotaResetError(err) {
		t.Fatalf("expected QuotaResetError, got: %v", err)
	}
	if fc.callCount != 1 {
		t.Errorf("expected 1 call (deadline blocked), got %d", fc.callCount)
	}
}

// TestQuotaWaitChatter_WaitExceedsMaxWait verifies immediate error when wait > MaxWait.
func TestQuotaWaitChatter_WaitExceedsMaxWait(t *testing.T) {
	wait := 48 * time.Hour
	qe := &QuotaResetError{
		ProviderID: "test",
		ModelID:    "test",
		Code:       "quota_exceeded",
		RetryAfter: wait,
		MaxWait:    24 * time.Hour,
	}

	fc := &testFakeChatter{
		responses: []struct {
			resp *Response
			err  error
		}{
			{nil, qe},
		},
	}

	ch := newQuotaWaitChatter(fc, QuotaWaitConfig{Enabled: true, MaxWait: 24 * time.Hour, DefaultEstimate: time.Hour}, nil)
	_, err := ch.Chat(context.Background(), nil)
	if !IsQuotaResetError(err) {
		t.Fatalf("expected QuotaResetError, got: %v", err)
	}
	if fc.callCount != 1 {
		t.Errorf("expected 1 call (no retry), got %d", fc.callCount)
	}
}

// TestQuotaWaitChatter_ResetAtInPast verifies immediate error when ResetAt is in the past.
func TestQuotaWaitChatter_ResetAtInPast(t *testing.T) {
	qe := &QuotaResetError{
		ProviderID: "test",
		ModelID:    "test",
		Code:       "quota_exceeded",
		ResetAt:    time.Now().Add(-1 * time.Hour),
		MaxWait:    24 * time.Hour,
	}

	fc := &testFakeChatter{
		responses: []struct {
			resp *Response
			err  error
		}{
			{nil, qe},
		},
	}

	ch := newQuotaWaitChatter(fc, QuotaWaitConfig{Enabled: true, MaxWait: 24 * time.Hour, DefaultEstimate: time.Hour}, nil)
	_, err := ch.Chat(context.Background(), nil)
	if !IsQuotaResetError(err) {
		t.Fatalf("expected QuotaResetError, got: %v", err)
	}
	if fc.callCount != 1 {
		t.Errorf("expected 1 call (no retry), got %d", fc.callCount)
	}
}

// TestQuotaWaitChatter_NonQuotaError verifies non-quota errors pass through immediately.
func TestQuotaWaitChatter_NonQuotaErrorReturnedImmediately(t *testing.T) {
	want := errors.New("some other error")
	fc := &testFakeChatter{
		responses: []struct {
			resp *Response
			err  error
		}{
			{nil, want},
		},
	}

	ch := newQuotaWaitChatter(fc, QuotaWaitConfig{Enabled: true, MaxWait: 24 * time.Hour, DefaultEstimate: time.Hour}, nil)
	_, err := ch.Chat(context.Background(), nil)
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got: %v", want, err)
	}
	if fc.callCount != 1 {
		t.Errorf("expected 1 call, got %d", fc.callCount)
	}
}

// TestQuotaWaitChatter_SuccessFirstCall verifies zero overhead on success.
func TestQuotaWaitChatter_SuccessFirstCall(t *testing.T) {
	fc := &testFakeChatter{
		responses: []struct {
			resp *Response
			err  error
		}{
			{&Response{}, nil},
		},
	}

	ch := newQuotaWaitChatter(fc, QuotaWaitConfig{Enabled: true, MaxWait: 24 * time.Hour, DefaultEstimate: time.Hour}, nil)
	resp, err := ch.Chat(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if fc.callCount != 1 {
		t.Errorf("expected 1 call, got %d", fc.callCount)
	}
}

// TestQuotaWaitChatter_Disabled verifies no-wait when disabled.
func TestQuotaWaitChatter_Disabled(t *testing.T) {
	qe := &QuotaResetError{
		ProviderID: "test",
		ModelID:    "test",
		Code:       "quota_exceeded",
		RetryAfter: 50 * time.Millisecond,
		MaxWait:    24 * time.Hour,
	}

	fc := &testFakeChatter{
		responses: []struct {
			resp *Response
			err  error
		}{
			{nil, qe},
		},
	}

	ch := newQuotaWaitChatter(fc, QuotaWaitConfig{Enabled: false, MaxWait: 24 * time.Hour, DefaultEstimate: time.Hour}, nil)
	_, err := ch.Chat(context.Background(), nil)
	if !IsQuotaResetError(err) {
		t.Fatalf("expected QuotaResetError, got: %v", err)
	}
	if fc.callCount != 1 {
		t.Errorf("expected 1 call (disabled, no retry), got %d", fc.callCount)
	}
}

// TestQuotaWaitChatter_ZeroConfigGated verifies zero-value config stays disabled.
func TestQuotaWaitChatter_ZeroConfigGated(t *testing.T) {
	qe := &QuotaResetError{
		ProviderID: "test",
		ModelID:    "test",
		Code:       "quota_exceeded",
		RetryAfter: 50 * time.Millisecond,
		MaxWait:    24 * time.Hour,
	}

	fc := &testFakeChatter{
		responses: []struct {
			resp *Response
			err  error
		}{
			{nil, qe},
			{&Response{}, nil},
		},
	}

	ch := newQuotaWaitChatter(fc, QuotaWaitConfig{}, nil)
	_, err := ch.Chat(context.Background(), nil)
	if !IsQuotaResetError(err) {
		t.Fatalf("expected QuotaResetError, got: %v", err)
	}
	if fc.callCount != 1 {
		t.Errorf("expected 1 call (zero config disabled), got %d", fc.callCount)
	}
}

// TestChatterForModelQuota verifies ChatterForModel wraps returned chatter
// in quota-aware retry when the broker's quota_retry config is enabled.
func TestChatterForModelQuota_Enabled(t *testing.T) {
	mc := &ModelConfig{ModelID: "test", ProviderID: "test", BaseURL: "http://localhost"}
	broker := NewModelBroker(BrokerConfig{
		Logger: nil,
	})
	broker.mu.Lock()
	broker.entries["test/test"] = &brokerEntry{
		model:   mc,
		chatter: &testFakeChatter{},
	}
	broker.mu.Unlock()

	ch := broker.ChatterForModel("test/test")
	if ch == nil {
		t.Fatal("expected non-nil chatter")
	}
	if _, ok := ch.(*quotaWaitChatter); ok {
		t.Fatal("expected raw chatter when quota_retry not configured")
	}
}

func TestChatterForModelQuota_WithQuotaRetry(t *testing.T) {
	mc := &ModelConfig{ModelID: "test", ProviderID: "test", BaseURL: "http://localhost"}
	broker := NewModelBroker(BrokerConfig{
		QuotaRetry: QuotaWaitConfig{Enabled: true, MaxWait: 24 * time.Hour, DefaultEstimate: time.Hour},
		Logger:     nil,
	})
	broker.mu.Lock()
	broker.entries["test/test"] = &brokerEntry{
		model:   mc,
		chatter: &testFakeChatter{},
	}
	broker.mu.Unlock()

	ch := broker.ChatterForModel("test/test")
	if ch == nil {
		t.Fatal("expected non-nil chatter")
	}
	if _, ok := ch.(*quotaWaitChatter); !ok {
		t.Fatalf("expected *quotaWaitChatter, got %T", ch)
	}
}

func TestChatterForModelQuota_UnknownModel(t *testing.T) {
	broker := NewModelBroker(BrokerConfig{
		Logger: nil,
	})
	ch := broker.ChatterForModel("nonexistent/model")
	if ch != nil {
		t.Fatalf("expected nil for unknown model, got %v", ch)
	}
}

func TestChatterForModelQuota_ServesQuotaWait(t *testing.T) {
	mc := &ModelConfig{ModelID: "test", ProviderID: "test", BaseURL: "http://localhost"}

	fc := &testFakeChatter{
		responses: []struct {
			resp *Response
			err  error
		}{
			{nil, &QuotaResetError{ProviderID: "test", ModelID: "test", Code: "quota_exceeded", RetryAfter: 50 * time.Millisecond, MaxWait: 24 * time.Hour}},
			{&Response{}, nil},
		},
	}

	broker := NewModelBroker(BrokerConfig{
		QuotaRetry: QuotaWaitConfig{Enabled: true, MaxWait: 24 * time.Hour, DefaultEstimate: time.Hour},
		Logger:     nil,
	})
	broker.mu.Lock()
	broker.entries["test/test"] = &brokerEntry{
		model:   mc,
		chatter: fc,
	}
	broker.mu.Unlock()

	ch := broker.ChatterForModel("test/test")
	if ch == nil {
		t.Fatal("expected non-nil chatter")
	}

	start := time.Now()
	resp, err := ch.Chat(context.Background(), nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if fc.callCount != 2 {
		t.Errorf("expected 2 calls (wait + retry), got %d", fc.callCount)
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected wait >= 50ms, got %v", elapsed)
	}
}

func TestChatterForModelQuota_CtxCancelled(t *testing.T) {
	mc := &ModelConfig{ModelID: "test", ProviderID: "test", BaseURL: "http://localhost"}

	fc := &testFakeChatter{
		responses: []struct {
			resp *Response
			err  error
		}{
			{nil, &QuotaResetError{ProviderID: "test", ModelID: "test", Code: "quota_exceeded", RetryAfter: 500 * time.Millisecond, MaxWait: 24 * time.Hour}},
			{&Response{}, nil},
		},
	}

	broker := NewModelBroker(BrokerConfig{
		QuotaRetry: QuotaWaitConfig{Enabled: true, MaxWait: 24 * time.Hour, DefaultEstimate: time.Hour},
		Logger:     nil,
	})
	broker.mu.Lock()
	broker.entries["test/test"] = &brokerEntry{
		model:   mc,
		chatter: fc,
	}
	broker.mu.Unlock()

	ch := broker.ChatterForModel("test/test")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := ch.Chat(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if fc.callCount != 1 {
		t.Errorf("expected 1 call (cancelled before retry), got %d", fc.callCount)
	}
}
