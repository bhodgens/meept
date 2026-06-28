package services

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/session"
)

func testBus() *bus.MessageBus {
	return bus.New(&bus.Config{
		BufferSize: 100,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestNewPushService(t *testing.T) {
	store := &fakeStore{}
	b := testBus()
	defer b.Close()

	s := NewPushService(store, b, nil)
	if s == nil {
		t.Fatal("expected non-nil PushService")
	}
	if s.logger == nil {
		t.Error("expected non-nil logger")
	}
}

func TestPush_NilRequest(t *testing.T) {
	store := &fakeStore{}
	b := testBus()
	defer b.Close()

	s := NewPushService(store, b, nil)
	_, err := s.Push(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil request")
	}
}

func TestPush_EmptyContent(t *testing.T) {
	store := &fakeStore{}
	b := testBus()
	defer b.Close()

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
	store := &fakeStore{}
	b := testBus()
	defer b.Close()

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
	store := &fakeStore{}
	b := testBus()
	defer b.Close()

	s := NewPushService(store, b, nil)

	result, err := s.Push(context.Background(), &PushRequest{
		Content: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestPush_MultiSession(t *testing.T) {
	store := &fakeStore{}
	b := testBus()
	defer b.Close()

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
	store := &fakeStore{}
	b := testBus()
	defer b.Close()

	s := NewPushService(store, b, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = s.Push(ctx, &PushRequest{
		Content:    "should be skipped",
		SessionIDs: []string{"sess-1"},
	})
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
func (f *fakeStore) ForkSession(string, int64, string) (*session.Session, error) {
	return nil, nil
}
func (f *fakeStore) SaveToolCalls(int64, []session.ToolCall) error { return nil }
func (f *fakeStore) GetToolCalls(int64) ([]session.ToolCall, error) { return nil, nil }
func (f *fakeStore) GetToolCallsForMessages([]int64) (map[int64][]session.ToolCall, error) {
	return nil, nil
}
func (f *fakeStore) SetProject(string, string, string) error { return nil }
func (f *fakeStore) SearchMessages(context.Context, string, int) ([]session.MessageSearchResult, error) {
	return nil, nil
}
func (f *fakeStore) SearchMessagesSemantic(context.Context, []float32, int) ([]session.MessageSearchResult, error) {
	return nil, nil
}
func (f *fakeStore) StoreEmbedding(context.Context, int64, []float32) error { return nil }
func (f *fakeStore) UnembeddedMessages(context.Context, int) ([]session.MessageSearchResult, error) {
	return nil, nil
}
func (f *fakeStore) GetActiveThread(context.Context, string) (*session.Thread, error) {
	return nil, nil
}
func (f *fakeStore) ListThreadsBySession(context.Context, string) ([]*session.Thread, error) {
	return nil, nil
}
func (f *fakeStore) CreateThread(context.Context, *session.Thread) error { return nil }
func (f *fakeStore) GetThread(context.Context, string) (*session.Thread, error) { return nil, nil }
func (f *fakeStore) UpdateThread(context.Context, *session.Thread) error { return nil }
func (f *fakeStore) DeleteThread(context.Context, string) error { return nil }
func (f *fakeStore) SetActiveThread(context.Context, string, string) error { return nil }
func (f *fakeStore) GetDesignatedSessionIDs() ([]string, error) { return nil, nil }
func (f *fakeStore) UpdateDesignation(string, session.DesignationStatus, string, string) error {
	return nil
}
func (f *fakeStore) ClearDesignation(string) error { return nil }
func (f *fakeStore) Archive(string, bool) error { return nil }
