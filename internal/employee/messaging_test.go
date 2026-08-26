package employee

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestMessageStore(t *testing.T) *MessageStore {
	t.Helper()
	s, err := NewMessageStore(filepath.Join(t.TempDir(), "messages.db"), nil)
	if err != nil {
		t.Fatalf("NewMessageStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMessageStore_EnqueueDrainTransitions(t *testing.T) {
	s := newTestMessageStore(t)

	msg := &AgentMessage{From: "orchestrator", To: "coder", Body: "hello"}
	if err := s.Enqueue(msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if msg.ID == "" {
		t.Fatal("Enqueue must assign an ID")
	}
	if msg.State != MessageStateQueued {
		t.Fatalf("state = %q; want queued", msg.State)
	}
	if msg.CreatedAt.IsZero() {
		t.Fatal("CreatedAt must be set")
	}

	got, err := s.DrainInbox("coder", 10)
	if err != nil {
		t.Fatalf("DrainInbox: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("drained %d messages; want 1", len(got))
	}
	if got[0].Body != "hello" || got[0].From != "orchestrator" || got[0].To != "coder" {
		t.Fatalf("drained message fields mismatch: %+v", got[0])
	}
	if got[0].State != MessageStateDelivered {
		t.Fatalf("state = %q; want delivered", got[0].State)
	}
	if got[0].DeliveredAt == nil {
		t.Fatal("DeliveredAt must be stamped on drain")
	}

	// Mark read.
	if err := s.MarkRead([]string{got[0].ID}); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
}

func TestMessageStore_DrainFIFOAndLimit(t *testing.T) {
	s := newTestMessageStore(t)
	for i := 0; i < 5; i++ {
		m := &AgentMessage{From: "a", To: "b", Body: fmt.Sprintf("m%d", i)}
		if err := s.Enqueue(m); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
		// Ensure strictly increasing created_at ordering.
		time.Sleep(2 * time.Millisecond)
	}

	first, err := s.DrainInbox("b", 2)
	if err != nil {
		t.Fatalf("DrainInbox(2): %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("got %d; want 2", len(first))
	}
	if first[0].Body != "m0" || first[1].Body != "m1" {
		t.Fatalf("FIFO order violated: %q, %q", first[0].Body, first[1].Body)
	}

	// Second drain gets remaining 3 in order.
	rest, err := s.DrainInbox("b", 10)
	if err != nil {
		t.Fatalf("second DrainInbox: %v", err)
	}
	if len(rest) != 3 {
		t.Fatalf("got %d; want 3", len(rest))
	}
	if rest[0].Body != "m2" || rest[2].Body != "m4" {
		t.Fatalf("FIFO order violated in second drain: %q..%q", rest[0].Body, rest[2].Body)
	}

	// Empty inbox afterwards.
	empty, err := s.DrainInbox("b", 10)
	if err != nil {
		t.Fatalf("third drain: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty inbox; got %d", len(empty))
	}
}

func TestMessageStore_DrainIsolationPerRecipient(t *testing.T) {
	s := newTestMessageStore(t)
	if err := s.Enqueue(&AgentMessage{From: "x", To: "alice", Body: "for alice"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.DrainInbox("bob", 10)
	if err != nil {
		t.Fatalf("DrainInbox(bob): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bob should see nothing; got %d", len(got))
	}
	// Alice's message stays queued.
	again, err := s.DrainInbox("alice", 10)
	if err != nil || len(again) != 1 {
		t.Fatalf("alice drain: %d msgs, err %v", len(again), err)
	}
}

func TestMessageStore_BodyCap(t *testing.T) {
	s := newTestMessageStore(t)
	big := &AgentMessage{From: "a", To: "b", Body: strings.Repeat("x", MaxMessageBodyBytes+1)}
	err := s.Enqueue(big)
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("err = %v; want ErrMessageTooLarge", err)
	}
	ok := &AgentMessage{From: "a", To: "b", Body: strings.Repeat("x", MaxMessageBodyBytes)}
	err = s.Enqueue(ok)
	if err != nil {
		t.Fatalf("exactly-32KB body should be accepted: %v", err)
	}
}

func TestMessageStore_PersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "messages.db")
	s1, err := NewMessageStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Enqueue(&AgentMessage{From: "a", To: "b", Body: "persisted"}); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	s2, err := NewMessageStore(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	got, err := s2.DrainInbox("b", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Body != "persisted" {
		t.Fatalf("message lost across reopen: %+v", got)
	}
}
