package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newTestStore creates a SQLiteStore backed by a temp file for testing.
// Reuses the testHelper from store_sqlite_test.go.
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, _ := testHelper(t)
	return store
}

// saveTestMessages saves a slice of messages to a new session and returns the
// session ID. Messages get sequential IDs assigned by the store.
func saveTestMessages(t *testing.T, store *SQLiteStore, msgs []Message) string {
	t.Helper()
	sess, err := store.Create("test-session")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i := range msgs {
		msgs[i].SessionID = sess.ID
		msgs[i].Timestamp = time.Now().UTC()
	}
	if err := store.SaveMessages(sess.ID, msgs); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}
	return sess.ID
}

func TestSearchMessages_FTS(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()

	msgs := []Message{
		{Role: "user", Content: "hello world", EntryType: "message", BranchID: "main"},
		{Role: "assistant", Content: "the specific term is zipline", EntryType: "message", BranchID: "main"},
		{Role: "user", Content: "unrelated content here", EntryType: "message", BranchID: "main"},
	}
	sessionID := saveTestMessages(t, store, msgs)
	ctx := context.Background()

	// Search for the specific term.
	results, err := store.SearchMessages(ctx, "zipline", 10)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.MessageID == 0 {
		t.Error("MessageID should be non-zero")
	}
	if r.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", r.SessionID, sessionID)
	}
	if r.Content != "the specific term is zipline" {
		t.Errorf("Content = %q", r.Content)
	}
	if r.Snippet == "" {
		t.Error("Snippet should not be empty")
	}
}

func TestSearchMessages_Limit(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()

	msgs := make([]Message, 5)
	for i := range msgs {
		msgs[i] = Message{
			Role:      "user",
			Content:   "common keyword banana",
			EntryType: "message",
			BranchID:  "main",
		}
	}
	saveTestMessages(t, store, msgs)

	results, err := store.SearchMessages(context.Background(), "banana", 2)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected exactly 2 results (limit), got %d", len(results))
	}
}

func TestSearchMessages_EmptyQuery(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()

	msgs := []Message{
		{Role: "user", Content: "some content", EntryType: "message", BranchID: "main"},
	}
	saveTestMessages(t, store, msgs)

	results, err := store.SearchMessages(context.Background(), "", 10)
	// Empty MATCH query is an error in FTS5; the store surfaces it as an error.
	// The test's contract: no results returned and no fatal crash.
	if err == nil && len(results) > 0 {
		t.Errorf("expected no results for empty query, got %d", len(results))
	}
}

// vec0Available probes whether the vec0 virtual table module is registered in
// the current sqlite build. Returns true if a vec0 table can be created.
func vec0Available(t *testing.T, store *SQLiteStore) bool {
	t.Helper()
	// The migrate() call in NewSQLiteStore already attempts to create
	// session_message_vectors. Probe for its existence.
	var name string
	err := store.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='session_message_vectors'`,
	).Scan(&name)
	return err == nil && name == "session_message_vectors"
}

func TestStoreEmbedding_SearchSemantic(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()

	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available")
	}

	msgs := []Message{
		{Role: "user", Content: "semantic test content", EntryType: "message", BranchID: "main"},
	}
	saveTestMessages(t, store, msgs)

	// Retrieve the saved message ID.
	saved, err := store.GetMessages(store.GetMostRecent().ID, 0, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved message, got %d", len(saved))
	}
	msgID := saved[0].ID

	// Build a 768-dim embedding (fixed dimension per schema).
	emb := make([]float32, 768)
	for i := range emb {
		emb[i] = float32(i%100) * 0.01
	}

	ctx := context.Background()
	if err := store.StoreEmbedding(ctx, msgID, emb); err != nil {
		t.Fatalf("StoreEmbedding: %v", err)
	}

	results, err := store.SearchMessagesSemantic(ctx, emb, 10)
	if err != nil {
		t.Fatalf("SearchMessagesSemantic: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if results[0].Relevance <= 0 {
		t.Errorf("expected Relevance > 0, got %f", results[0].Relevance)
	}
	if results[0].MessageID != msgID {
		t.Errorf("MessageID = %d, want %d", results[0].MessageID, msgID)
	}
}

func TestUnembeddedMessages(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()

	msgs := []Message{
		{Role: "user", Content: "first message", EntryType: "message", BranchID: "main"},
		{Role: "assistant", Content: "second message", EntryType: "message", BranchID: "main"},
		{Role: "user", Content: "third message", EntryType: "message", BranchID: "main"},
	}
	saveTestMessages(t, store, msgs)

	// Embed one of the three messages.
	saved, _ := store.GetMessages(store.GetMostRecent().ID, 0, 10)
	if len(saved) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(saved))
	}

	ctx := context.Background()
	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available; UnembeddedMessages requires session_message_vectors table")
	}

	// Store an embedding for the second message.
	emb := make([]float32, 768)
	if err := store.StoreEmbedding(ctx, saved[1].ID, emb); err != nil {
		t.Fatalf("StoreEmbedding: %v", err)
	}

	results, err := store.UnembeddedMessages(ctx, 10)
	if err != nil {
		t.Fatalf("UnembeddedMessages: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 unembedded messages, got %d", len(results))
	}
}

func TestSearchMessagesSemantic_Unavailable(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()

	if vec0Available(t, store) {
		t.Skip("vec0 is available; ErrSemanticUnavailable test is a no-op")
	}

	// Use a non-empty embedding so we reach the vec0 probe.
	emb := []float32{0.1, 0.2, 0.3}
	_, err := store.SearchMessagesSemantic(context.Background(), emb, 10)
	if !errors.Is(err, ErrSemanticUnavailable) {
		t.Errorf("expected ErrSemanticUnavailable, got %v", err)
	}
}
