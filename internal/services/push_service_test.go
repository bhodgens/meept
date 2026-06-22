package services

import (
	"context"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/models"
)

type fakeBus struct {
	mu     sync.Mutex
	topics map[string][]*models.BusMessage
}

func newFakeBus() *fakeBus {
	return &fakeBus{topics: make(map[string][]*models.BusMessage)}
}

func (f *fakeBus) Publish(topic string, msg *models.BusMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.topics[topic] = append(f.topics[topic], msg)
}

func (f *fakeBus) Subscribe(id, topic string) *bus.Subscriber {
	return nil // push doesn't subscribe
}

func (f *fakeBus) Unsubscribe(*bus.Subscriber) {}

func TestNewPushService(t *testing.T) {
	b := newFakeBus()
	store := &fakeStore{}

	s := NewPushService(store, b, nil)
	if s == nil {
		t.Fatal("expected non-nil PushService")
	}
	if s.logger == nil {
		t.Error("expected non-nil logger")
	}
}

func TestPush_NilRequest(t *testing.T) {
	b := newFakeBus()
	store := &fakeStore{}

	s := NewPushService(store, b, nil)
	_, err := s.Push(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil request")
	}
}

func TestPush_EmptyContent(t *testing.T) {
	b := newFakeBus()
	store := &fakeStore{}

	s := NewPushService(store, b, nil)
	_, err := s.Push(context.Background(), &PushRequest{
		Content: "",
	})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestPush_WithoutBus(t *testing.T) {
	store := &fakeStore{}
	s := NewPushService(store, nil, nil)
	_, err := s.Push(context.Background(), &PushRequest{
		Content: "hello",
	})
	if err == nil {
		t.Error("expected error for nil bus")
	}
}

func TestPush_PublishesMessage(t *testing.T) {
	b := newFakeBus()
	store := &fakeStore{}

	s := NewPushService(store, b, nil)

	result, err := s.Push(context.Background(), &PushRequest{
		Content:    "test notification",
		SessionIDs: []string{"sess-1"},
		Source:     "test",
		Type:       PushTypeAlert,
		Priority:   PushPriorityUrgent,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Delivered != 1 {
		t.Errorf("expected 1 delivered, got %d", result.Delivered)
	}
}

func TestPush_Defaults(t *testing.T) {
	b := newFakeBus()
	store := &fakeStore{}

	s := NewPushService(store, b, nil)

	result, err := s.Push(context.Background(), &PushRequest{
		Content: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result // defaults applied in Push method; just verify it completes
}

func TestPush_MultiSession(t *testing.T) {
	b := newFakeBus()
	store := &fakeStore{}

	s := NewPushService(store, b, nil)

	result, err := s.Push(context.Background(), &PushRequest{
		Content:    "broadcast",
		SessionIDs: []string{"sess-1", "sess-2", "sess-3"},
		Type:       PushTypeNotification,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Delivered != 3 {
		t.Errorf("expected 3 delivered, got %d", result.Delivered)
	}
}

func TestPush_ContextCanceled(t *testing.T) {
	b := newFakeBus()
	store := &fakeStore{}

	s := NewPushService(store, b, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	result, err := s.Push(ctx, &PushRequest{
		Content:    "should be skipped",
		SessionIDs: []string{"sess-1"},
	})
	_ = result
	// Context is checked inside the loop after input validation.
	// With content present, validation passes, then ctx.Err() is checked.
}

func TestPushType_Values(t *testing.T) {
	for _, pt := range []PushType{PushTypeNotification, PushTypeAlert, PushTypeSummary} {
		if pt == "" {
			t.Errorf("expected non-empty PushType value, got empty")
		}
	}
}

func TestPushPriority_Values(t *testing.T) {
	for _, pp := range []PushPriority{PushPriorityLow, PushPriorityNormal, PushPriorityHigh, PushPriorityUrgent} {
		if pp == "" {
			t.Errorf("expected non-empty PushPriority value, got empty")
		}
	}
}

func TestPush_BusMessageFormat(t *testing.T) {
	b := newFakeBus()
	store := &fakeStore{}

	s := NewPushService(store, b, nil)

	_, err := s.Push(context.Background(), &PushRequest{
		Content:    "format check",
		SessionIDs: []string{"sess-1"},
		Type:       PushTypeSummary,
		Priority:   PushPriorityLow,
		Extra: map[string]any{
			"custom_key": "custom_value",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the bus received the message on a per-session topic
	b.mu.Lock()
	topics := b.topics
	b.mu.Unlock()

	// Check per-session push topic
	sessMsgs, ok := topics["push.sess-1"]
	if !ok || len(sessMsgs) == 0 {
		t.Fatal("expected push.sess-1 message on bus")
	}

	for _, m := range sessMsgs {
		if m.Type != models.MessageTypeEvent {
			t.Errorf("expected MessageTypeEvent, got %s", m.Type)
		}
		if m.Source != "svc.push" {
			t.Errorf("expected source 'svc.push', got %s", m.Source)
		}
		if len(m.Payload) == 0 {
			t.Error("expected non-empty payload")
		}
	}
}

func TestPush_PerSessionTopic(t *testing.T) {
	b := newFakeBus()
	store := &fakeStore{}

	s := NewPushService(store, b, nil)

	_, err := s.Push(context.Background(), &PushRequest{
		Content:    "per session test",
		SessionIDs: []string{"sess-1", "sess-2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b.mu.Lock()
	topics := b.topics
	b.mu.Unlock()

	if len(topics) != 2 {
		t.Errorf("expected 2 topics published, got %d", len(topics))
	}
	for _, topic := range []string{"push.sess-1", "push.sess-2"} {
		if _, ok := topics[topic]; !ok {
			t.Errorf("expected message on topic %q", topic)
		}
	}
}

// --- test helpers ---

type fakeStore struct{}

func (f *fakeStore) Create(name string) (*session.Session, error)                    { return nil, nil }
func (f *fakeStore) Get(string) *session.Session            { return nil }
func (f *fakeStore) GetByConversationID(string) *session.Session  { return nil }
func (f *fakeStore) GetMostRecent() *session.Session          { return nil }
func (f *fakeStore) List() ([]*session.Session, error)        { return nil, nil }
func (f *fakeStore) Delete(string) bool                       { return false }
func (f *fakeStore) Attach(string, string) error            { return nil }
func (f *fakeStore) Detach(string, string) error            { return nil }
func (f *fakeStore) UpdateActivity(string) error            { return nil }
func (f *fakeStore) AddWorker(string, string) error         { return nil }
func (f *fakeStore) RemoveWorker(string, string) error      { return nil }
func (f *fakeStore) SaveMessages(string, []session.Message) error   { return nil }
func (f *fakeStore) GetMessages(string, int, int) ([]session.Message, error) {
	return nil, nil
}
func (f *fakeStore) GetMessageCount(string) (int, error)     { return 0, nil }
func (f *fakeStore) UpdateDescription(string, string) error  { return nil }
func (f *fakeStore) UpdateName(string, string) error         { return nil }
func (f *fakeStore) HasResponses(string) (bool, error)       { return false, nil }
func (f *fakeStore) Close() error                            { return nil }

func (f *fakeStore) GetLeafMessageID(string) (int64, error)           { return 0, nil }
func (f *fakeStore) SetLeafMessageID(string, int64) error             { return nil }
func (f *fakeStore) GetMessagePath(string, int64) ([]session.Message, error) {
	return nil, nil
}
func (f *fakeStore) GetMessageBranches(string) ([]session.Branch, error) { return nil, nil }
func (f *fakeStore) NavigateToBranch(string, int64) (int64, error)     { return 0, nil }
func (f *fakeStore) CreateBranch(string, int64, int64, string) (*session.Branch, error) {
	return nil, nil
}
func (f *fakeStore) GetBranch(string, string) (*session.Branch, error) { return nil, nil }
func (f *fakeStore) DeleteBranch(string, string) error               { return nil }
func (f *fakeStore) ListBranches(string) ([]session.Branch, error)   { return nil, nil }
func (f *fakeStore) GetTree(string) ([]session.TreeNode, error)      { return nil, nil }
func (f *fakeStore) Compact(string, map[string]any) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f *fakeStore) Search(string, int) ([]*session.Session, error) { return nil, nil }
func (f *fakeStore) Save(context.Context, *session.Session) error { return nil }
func (f *fakeStore) ListWithLimit(int, int) ([]*session.Session, error) {
	return nil, nil
}
func (f *fakeStore) GetByClientID(string) (*session.Session, error) { return nil, nil }
func (f *fakeStore) GetThreadList(string) ([]session.Thread, error) { return nil, nil }
func (f *fakeStore) GetOrCreateThread(string, string) (*session.Thread, error) {
	return nil, nil
}
func (f *fakeStore) ArchiveThread(string) error             { return nil }
func (f *fakeStore) ListThreadSummary(context.Context) ([]session.ThreadSummary, error) {
	return nil, nil
}
