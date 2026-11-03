package voice

// Leaf 12 T2/T3/T4/T6: binding - session resolve/reuse, brief injection
// once per call, turn persistence, progress events, transcript roundtrip.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeSessions records the session-store operations the binding makes.
type fakeSessions struct {
	mu       sync.Mutex
	created  []string
	renamed  map[string]string
	employee map[string]string
	sessions []NamedSession
	nextID   int
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{
		renamed:  make(map[string]string),
		employee: make(map[string]string),
	}
}

func (f *fakeSessions) Create(name string) (SessionHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := "session-" + name + "-" + string(rune('a'+f.nextID))
	f.created = append(f.created, id)
	sess := NamedSession{ID: id, Name: name, ConversationID: "conv-" + id}
	f.sessions = append(f.sessions, sess)
	return SessionHandle{ID: sess.ID, ConversationID: sess.ConversationID}, nil
}

func (f *fakeSessions) List() ([]NamedSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]NamedSession, len(f.sessions))
	copy(out, f.sessions)
	return out, nil
}

func (f *fakeSessions) UpdateName(sessionID, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renamed[sessionID] = name
	for i := range f.sessions {
		if f.sessions[i].ID == sessionID {
			f.sessions[i].Name = name
		}
	}
	return nil
}

func (f *fakeSessions) SetEmployeeID(sessionID, employeeID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.employee[sessionID] = employeeID
	for i := range f.sessions {
		if f.sessions[i].ID == sessionID {
			f.sessions[i].EmployeeID = employeeID
		}
	}
	return nil
}

// fakeTurns records prompts and returns a canned reply.
type fakeTurns struct {
	mu      sync.Mutex
	prompts []string
	reply   string
	fail    error
}

func (f *fakeTurns) Run(_ context.Context, _ SessionHandle, prompt string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", f.fail
	}
	f.prompts = append(f.prompts, prompt)
	return f.reply, nil
}

// captureProgress records published progress events.
type captureProgress struct {
	mu     sync.Mutex
	events []ProgressEvent
}

func (c *captureProgress) PublishProgress(ev ProgressEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *captureProgress) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

// fakeCalls is a no-op CallStoreT so binding tests never touch sqlite.
type fakeCalls struct{}

func (fakeCalls) Start(_ context.Context, _ CallRecord) error                          { return nil }
func (fakeCalls) End(_ context.Context, _ string, _ string) error                      { return nil }
func (fakeCalls) AppendTranscript(_ context.Context, _ string, _ TranscriptLine) error { return nil }
func (fakeCalls) TodayTranscripts(_ context.Context, _ string) ([]CallTranscript, error) {
	return nil, nil
}
func (fakeCalls) SessionIDFor(_, _ string) (string, bool) { return "", false }

func newTestBinding(t *testing.T) (*Binding, *fakeSessions, *fakeTurns, *captureProgress) {
	t.Helper()
	sessions := newFakeSessions()
	turns := &fakeTurns{reply: "canned reply"}
	progress := &captureProgress{}
	b, err := NewBinding(BindingDeps{
		Calls:    fakeCalls{},
		Sessions: sessions,
		Turns:    turns,
		Progress: progress,
	})
	if err != nil {
		t.Fatalf("NewBinding: %v", err)
	}
	return b, sessions, turns, progress
}

func TestBindingSessionResolveAndReuse(t *testing.T) {
	b, sessions, _, _ := newTestBinding(t)
	ctx := context.Background()

	sid1, err := b.OnCallStart(ctx, "cc-in", "+15551230004", "emp-b", DirectionIncoming, DayBrief{})
	if err != nil {
		t.Fatalf("OnCallStart inbound: %v", err)
	}
	// A second call, opposite direction, same (number, employee): same
	// session must be reused (T2).
	sid2, err := b.OnCallStart(ctx, "cc-out", "+15551230004", "emp-b", DirectionOutgoing, DayBrief{})
	if err != nil {
		t.Fatalf("OnCallStart outbound: %v", err)
	}
	if sid1 != sid2 {
		t.Fatalf("session ids differ across directions: %q vs %q", sid1, sid2)
	}
	if len(sessions.created) != 1 {
		t.Fatalf("created %d sessions; want 1", len(sessions.created))
	}
	wantName := SessionName("emp-b", "+15551230004")
	if sessions.renamed[sid1] != wantName {
		t.Errorf("session name = %q; want %q", sessions.renamed[sid1], wantName)
	}
	if sessions.employee[sid1] != "emp-b" {
		t.Errorf("employee_id = %q; want emp-b", sessions.employee[sid1])
	}
	// A different employee gets a different session.
	sid3, err := b.OnCallStart(ctx, "cc-x", "+15551230004", "emp-c", DirectionIncoming, DayBrief{})
	if err != nil {
		t.Fatalf("OnCallStart other employee: %v", err)
	}
	if sid3 == sid1 {
		t.Fatal("different employee must not reuse the session")
	}
	// Concurrent calls for the same pair resolve to the same session.
	var wg sync.WaitGroup
	ids := make([]string, 4)
	for i := range ids {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			id, err := b.OnCallStart(ctx, "cc-c-"+string(rune('0'+k)), "+15551230005", "emp-d", DirectionIncoming, DayBrief{})
			if err != nil {
				t.Errorf("concurrent OnCallStart: %v", err)
				return
			}
			ids[k] = id
		}(i)
	}
	wg.Wait()
	for k, id := range ids {
		if id != ids[0] {
			t.Errorf("concurrent resolve %d = %q; want %q", k, id, ids[0])
		}
	}
}

func TestBindingTurnBriefOnceAndProgress(t *testing.T) {
	b, _, turns, progress := newTestBinding(t)
	ctx := context.Background()

	brief := DayBrief{DurableMemory: "name: Sam", ActiveGoals: []string{"answer phones"}}
	if _, err := b.OnCallStart(ctx, "cc-t", "+15551230006", "emp-e", DirectionIncoming, brief); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}

	if _, err := b.RunTurn(ctx, "cc-t", "first utterance"); err != nil {
		t.Fatalf("RunTurn 1: %v", err)
	}
	if _, err := b.RunTurn(ctx, "cc-t", "second utterance"); err != nil {
		t.Fatalf("RunTurn 2: %v", err)
	}

	turns.mu.Lock()
	p1, p2 := turns.prompts[0], turns.prompts[1]
	turns.mu.Unlock()

	if want := "[durable memory]"; !strings.Contains(p1, want) {
		t.Errorf("first prompt missing brief section %q: %q", want, p1)
	}
	if !strings.Contains(p1, "first utterance") {
		t.Errorf("first prompt missing user text: %q", p1)
	}
	if strings.Contains(p2, "[durable memory]") {
		t.Errorf("second prompt must NOT re-inject the brief: %q", p2)
	}
	if !strings.Contains(p2, "second utterance") {
		t.Errorf("second prompt missing user text: %q", p2)
	}
	// T4: exactly one progress event per final prompt.
	if got := progress.count(); got != 2 {
		t.Fatalf("progress events = %d; want 2", got)
	}
	progress.mu.Lock()
	ev := progress.events[0]
	progress.mu.Unlock()
	if ev.Kind != ProgressKindPrompt || ev.CallID != "cc-t" || ev.EmployeeID != "emp-e" {
		t.Errorf("event = %+v", ev)
	}
	if ev.At.IsZero() {
		t.Error("event At must be set")
	}
	// Say + interrupt progress paths.
	b.PublishSay("cc-t")
	b.PublishInterrupt("cc-t")
	if got := progress.count(); got != 4 {
		t.Fatalf("progress after say+interrupt = %d; want 4", got)
	}
	// Unknown call say is dropped, not panicked.
	b.PublishSay("cc-none")
	if got := progress.count(); got != 4 {
		t.Fatalf("progress after unknown-call say = %d; want 4", got)
	}
}

func TestBindingTurnUnknownCall(t *testing.T) {
	b, _, _, _ := newTestBinding(t)
	if _, err := b.RunTurn(context.Background(), "cc-none", "hello"); err == nil {
		t.Fatal("RunTurn for unknown call must error")
	}
}

func TestBindingTurnRunnerError(t *testing.T) {
	b, _, turns, _ := newTestBinding(t)
	ctx := context.Background()
	turns.fail = errors.New("llm down")
	if _, err := b.OnCallStart(ctx, "cc-f", "+15551230007", "emp-f", DirectionIncoming, DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}
	if _, err := b.RunTurn(ctx, "cc-f", "hello"); err == nil {
		t.Fatal("RunTurn must surface runner error")
	}
}

func TestBindingOnCallEndIdempotent(t *testing.T) {
	b, _, _, _ := newTestBinding(t)
	ctx := context.Background()
	if _, err := b.OnCallStart(ctx, "cc-e", "+15551230008", "emp-g", DirectionIncoming, DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}
	if err := b.OnCallEnd(ctx, "cc-e", "hangup"); err != nil {
		t.Fatalf("OnCallEnd: %v", err)
	}
	// Second end for the same ccid: no-op, no error.
	if err := b.OnCallEnd(ctx, "cc-e", "hangup"); err != nil {
		t.Fatalf("second OnCallEnd: %v", err)
	}
	// Unknown ccid: no-op.
	if err := b.OnCallEnd(ctx, "cc-none", "failed"); err != nil {
		t.Fatalf("OnCallEnd unknown: %v", err)
	}
}

func TestSessionNameShape(t *testing.T) {
	if got := SessionName("emp-1", "+15551230009"); got != "voice:emp-1:+15551230009" {
		t.Fatalf("SessionName = %q", got)
	}
}
