package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// testSummarizer is a mock BranchSummarizer for testing.
type testSummarizer struct {
	result *SummarizeBranchResult
	err    error
	called bool
}

func (ts *testSummarizer) SummarizeBranch(ctx context.Context, req SummarizeBranchRequest) (*SummarizeBranchResult, error) {
	ts.called = true
	if ts.err != nil {
		return nil, ts.err
	}
	return ts.result, nil
}

// Ensure testSummarizer implements BranchSummarizer.
var _ BranchSummarizer = (*testSummarizer)(nil)

// helperBranchManager creates a BranchManager with a SQLiteStore for testing.
func helperBranchManager(t *testing.T, summarizer BranchSummarizer) (*BranchManager, *SQLiteStore) {
	t.Helper()
	store, _ := testHelper(t)
	cfg := config.SessionConfig{
		BranchSummaryThreshold: 3,
	}
	bm := NewBranchManager(store, summarizer, cfg, slog.Default())
	return bm, store
}

// helperCreateMessages creates N messages in a chain, returning their IDs.
func helperCreateMessages(t *testing.T, store *SQLiteStore, sessionID string, count int) []int64 {
	t.Helper()
	now := time.Now().UTC()
	var ids []int64

	// First message has no parent
	msg := Message{
		SessionID: sessionID,
		Role:      "user",
		Content:   "message 1",
		Timestamp: now,
		EntryType: "message",
		BranchID:  "main",
	}
	if err := store.SaveMessages(sessionID, []Message{msg}); err != nil {
		t.Fatalf("failed to save message 1: %v", err)
	}
	msgs, _ := store.GetMessages(sessionID, 0, 1)
	ids = append(ids, msgs[0].ID)

	for i := 1; i < count; i++ {
		parentID := ids[len(ids)-1]
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		m := Message{
			SessionID: sessionID,
			ParentID:  &parentID,
			Role:      role,
			Content:   fmt.Sprintf("message %d", i+1),
			Timestamp: now.Add(time.Duration(i) * time.Second),
			EntryType: "message",
			BranchID:  "main",
		}
		if err := store.SaveMessages(sessionID, []Message{m}); err != nil {
			t.Fatalf("failed to save message %d: %v", i+1, err)
		}
		retrieved, _ := store.GetMessages(sessionID, i, 1)
		ids = append(ids, retrieved[0].ID)
	}

	return ids
}

// helperMakeChatMessages builds []llm.ChatMessage from role/content pairs.
func helperMakeChatMessages(pairs []struct{ Role, Content string }) []llm.ChatMessage {
	result := make([]llm.ChatMessage, len(pairs))
	for i, p := range pairs {
		result[i] = llm.ChatMessage{
			Role:    llm.Role(p.Role),
			Content: p.Content,
		}
	}
	return result
}

func TestNavigateToBranch_BasicNavigation(t *testing.T) {
	ts := &testSummarizer{}
	bm, store := helperBranchManager(t, ts)
	defer store.Close()

	session, err := store.Create("test-nav")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create 7 messages
	ids := helperCreateMessages(t, store, session.ID, 7)

	// Set leaf to last message
	if err := store.SetLeafMessageID(session.ID, ids[6]); err != nil {
		t.Fatalf("failed to set leaf: %v", err)
	}

	// Navigate to message 3 (index 2)
	result, err := bm.NavigateToBranch(context.Background(), session.ID, ids[2])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.OldLeafID != ids[6] {
		t.Errorf("expected old leaf %d, got %d", ids[6], result.OldLeafID)
	}
	if result.NewLeafID != ids[2] {
		t.Errorf("expected new leaf %d, got %d", ids[2], result.NewLeafID)
	}
	if result.AbandonedMsgs != 4 { // messages 4,5,6,7 (indices 3,4,5,6)
		t.Errorf("expected 4 abandoned messages, got %d", result.AbandonedMsgs)
	}
	if result.NewBranchID == "" {
		t.Error("expected non-empty new branch ID")
	}
	if result.Summary != "" {
		t.Error("expected no summary when summarizer returns nil (no LLM)")
	}

	// Verify leaf was updated
	leafID, err := store.GetLeafMessageID(session.ID)
	if err != nil {
		t.Fatalf("failed to get leaf: %v", err)
	}
	if leafID != ids[2] {
		t.Errorf("expected leaf %d, got %d", ids[2], leafID)
	}
}

func TestNavigateToBranch_SessionNotFound(t *testing.T) {
	ts := &testSummarizer{}
	bm, store := helperBranchManager(t, ts)
	defer store.Close()

	_, err := bm.NavigateToBranch(context.Background(), "nonexistent", 1)
	if err == nil {
		t.Error("expected error for non-existent session")
	}
}

func TestNavigateToBranch_NoLeafSet(t *testing.T) {
	ts := &testSummarizer{}
	bm, store := helperBranchManager(t, ts)
	defer store.Close()

	session, _ := store.Create("test-no-leaf")
	helperCreateMessages(t, store, session.ID, 3)

	// Don't set a leaf - should error
	_, err := bm.NavigateToBranch(context.Background(), session.ID, 1)
	if err == nil {
		t.Error("expected error when no leaf is set")
	}
}

func TestNavigateToBranch_TargetNotInPath(t *testing.T) {
	ts := &testSummarizer{}
	bm, store := helperBranchManager(t, ts)
	defer store.Close()

	session, _ := store.Create("test-not-in-path")
	ids := helperCreateMessages(t, store, session.ID, 3)
	store.SetLeafMessageID(session.ID, ids[2])

	// Create a separate message not on this path (different parent chain)
	separateMsg := Message{
		SessionID: session.ID,
		Role:      "user",
		Content:   "orphan",
		Timestamp: time.Now().UTC(),
		EntryType: "message",
		BranchID:  "other",
	}
	store.SaveMessages(session.ID, []Message{separateMsg})

	// Get the orphan's ID
	msgs, _ := store.GetMessages(session.ID, 3, 1)
	orphanID := msgs[0].ID

	// Try to navigate to the orphan - it's not on the path from root to leaf
	_, err := bm.NavigateToBranch(context.Background(), session.ID, orphanID)
	if err == nil {
		t.Error("expected error when target is not in path")
	}
}

func TestNavigateToBranch_NoOpWhenTargetEqualsLeaf(t *testing.T) {
	ts := &testSummarizer{}
	bm, store := helperBranchManager(t, ts)
	defer store.Close()

	session, _ := store.Create("test-noop")
	ids := helperCreateMessages(t, store, session.ID, 3)
	store.SetLeafMessageID(session.ID, ids[2])

	result, err := bm.NavigateToBranch(context.Background(), session.ID, ids[2])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OldLeafID != ids[2] {
		t.Errorf("expected old leaf %d, got %d", ids[2], result.OldLeafID)
	}
	if result.NewLeafID != ids[2] {
		t.Errorf("expected new leaf %d, got %d", ids[2], result.NewLeafID)
	}
	if result.AbandonedMsgs != 0 {
		t.Errorf("expected 0 abandoned messages, got %d", result.AbandonedMsgs)
	}
	if result.NewBranchID != "" {
		t.Error("expected empty new branch ID for no-op")
	}
	if ts.called {
		t.Error("summarizer should not be called for no-op navigation")
	}
}

func TestNavigateToBranch_ShortBranchNoSummary(t *testing.T) {
	ts := &testSummarizer{
		result: &SummarizeBranchResult{
			Summary:  "test summary",
			BranchID: "main",
			MsgCount: 2,
		},
	}
	cfg := config.SessionConfig{
		BranchSummaryThreshold: 10, // High threshold so 2 messages won't trigger
	}
	store, _ := testHelper(t)
	defer store.Close()
	bm := NewBranchManager(store, ts, cfg, slog.Default())

	session, _ := store.Create("test-short")
	ids := helperCreateMessages(t, store, session.ID, 3)
	store.SetLeafMessageID(session.ID, ids[2])

	// Navigate to message 2 (abandons only 1 message)
	result, err := bm.NavigateToBranch(context.Background(), session.ID, ids[1])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AbandonedMsgs != 1 {
		t.Errorf("expected 1 abandoned message, got %d", result.AbandonedMsgs)
	}
	if result.Summary != "" {
		t.Error("expected no summary for short branch")
	}
	if ts.called {
		t.Error("summarizer should not be called for short branch")
	}
}

func TestNavigateToBranch_WithSummarization(t *testing.T) {
	ts := &testSummarizer{
		result: &SummarizeBranchResult{
			Summary:  "branch summary content",
			BranchID: "main",
			MsgCount: 4,
		},
	}
	bm, store := helperBranchManager(t, ts)
	defer store.Close()

	session, _ := store.Create("test-summarize")
	ids := helperCreateMessages(t, store, session.ID, 7)
	store.SetLeafMessageID(session.ID, ids[6])

	// Navigate to message 3 (abandons 4 messages: indices 3,4,5,6)
	// threshold is 3, 4 >= 3, so summarization should trigger
	result, err := bm.NavigateToBranch(context.Background(), session.ID, ids[2])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ts.called {
		t.Error("expected summarizer to be called")
	}
	if result.Summary != "branch summary content" {
		t.Errorf("expected summary 'branch summary content', got %q", result.Summary)
	}
	if result.AbandonedMsgs != 4 {
		t.Errorf("expected 4 abandoned messages, got %d", result.AbandonedMsgs)
	}

	// Verify summary message was persisted
	allMsgs, _ := store.GetMessages(session.ID, 0, 100)
	foundSummary := false
	for _, msg := range allMsgs {
		if msg.EntryType == "summary" && msg.Content == "branch summary content" {
			foundSummary = true
			if msg.ParentID == nil || *msg.ParentID != ids[2] {
				t.Errorf("summary parent should be %d (fork point)", ids[2])
			}
			break
		}
	}
	if !foundSummary {
		t.Error("expected summary message to be persisted")
	}

	// Verify branch_point message was also persisted
	foundBranchPoint := false
	for _, msg := range allMsgs {
		if msg.EntryType == "branch_point" {
			foundBranchPoint = true
			if msg.ParentID == nil || *msg.ParentID != ids[2] {
				t.Errorf("branch_point parent should be %d (fork point)", ids[2])
			}
			break
		}
	}
	if !foundBranchPoint {
		t.Error("expected branch_point message to be persisted")
	}
}

func TestNavigateToBranch_SummarizerError(t *testing.T) {
	ts := &testSummarizer{
		err: fmt.Errorf("summarizer failed"),
	}
	bm, store := helperBranchManager(t, ts)
	defer store.Close()

	session, _ := store.Create("test-summarizer-error")
	ids := helperCreateMessages(t, store, session.ID, 7)
	store.SetLeafMessageID(session.ID, ids[6])

	// Should succeed even if summarizer fails
	result, err := bm.NavigateToBranch(context.Background(), session.ID, ids[2])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ts.called {
		t.Error("expected summarizer to be called")
	}
	if result.Summary != "" {
		t.Error("expected empty summary when summarizer fails")
	}
	// Should still complete navigation
	if result.NewLeafID != ids[2] {
		t.Errorf("expected new leaf %d, got %d", ids[2], result.NewLeafID)
	}
}

func TestInsertCompaction_Basic(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, _ := store.Create("test-compaction")
	ids := helperCreateMessages(t, store, session.ID, 3)

	// Insert compaction
	compactionID, err := store.InsertCompaction(session.ID, ids[0], "summary of messages 1-2", []int64{ids[0], ids[1]})
	if err != nil {
		t.Fatalf("failed to insert compaction: %v", err)
	}
	if compactionID <= 0 {
		t.Errorf("expected positive compaction ID, got %d", compactionID)
	}

	// Verify compaction message was stored
	msgs, _ := store.GetMessages(session.ID, 0, 10)
	var compactionMsg *Message
	for i := range msgs {
		if msgs[i].ID == compactionID {
			compactionMsg = &msgs[i]
			break
		}
	}
	if compactionMsg == nil {
		t.Fatal("compaction message not found")
	}
	if compactionMsg.EntryType != "compaction" {
		t.Errorf("expected entry_type 'compaction', got %q", compactionMsg.EntryType)
	}
	if compactionMsg.ParentID == nil || *compactionMsg.ParentID != ids[0] {
		t.Errorf("expected parent_id %d", ids[0])
	}
	// Verify JSON content
	var content CompactionContent
	if err := json.Unmarshal([]byte(compactionMsg.Content), &content); err != nil {
		t.Fatalf("failed to unmarshal compaction content: %v", err)
	}
	if content.Summary != "summary of messages 1-2" {
		t.Errorf("expected summary 'summary of messages 1-2', got %q", content.Summary)
	}
	if len(content.CompressedIDs) != 2 {
		t.Fatalf("expected 2 compressed IDs, got %d", len(content.CompressedIDs))
	}
	if content.CompressedIDs[0] != ids[0] || content.CompressedIDs[1] != ids[1] {
		t.Errorf("compressed IDs mismatch: expected [%d, %d], got %v", ids[0], ids[1], content.CompressedIDs)
	}
}

func TestInsertCompaction_EmptyCompressedIDs(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, _ := store.Create("test-compaction-empty")
	ids := helperCreateMessages(t, store, session.ID, 1)

	compactionID, err := store.InsertCompaction(session.ID, ids[0], "empty compaction", []int64{})
	if err != nil {
		t.Fatalf("failed to insert compaction: %v", err)
	}
	if compactionID <= 0 {
		t.Errorf("expected positive compaction ID, got %d", compactionID)
	}

	msgs, _ := store.GetMessages(session.ID, 0, 10)
	var compactionMsg *Message
	for i := range msgs {
		if msgs[i].ID == compactionID {
			compactionMsg = &msgs[i]
			break
		}
	}
	if compactionMsg == nil {
		t.Fatal("compaction message not found")
	}

	var content CompactionContent
	if err := json.Unmarshal([]byte(compactionMsg.Content), &content); err != nil {
		t.Fatalf("failed to unmarshal compaction content: %v", err)
	}
	if len(content.CompressedIDs) != 0 {
		t.Errorf("expected empty compressed IDs, got %d", len(content.CompressedIDs))
	}
}

func TestSummarizeBranch_TooFewMessages(t *testing.T) {
	summarizer := NewSummarizer(nil, slog.Default())

	// Only 2 messages - below threshold of 3
	req := SummarizeBranchRequest{
		Messages: helperMakeChatMessages([]struct{ Role, Content string }{
			{"user", "hello"},
			{"assistant", "hi"},
		}),
		BranchID: "test",
	}

	result, err := summarizer.SummarizeBranch(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for fewer than 3 messages")
	}
}

func TestSummarizeBranch_FallbackWithoutLLM(t *testing.T) {
	summarizer := NewSummarizer(nil, slog.Default())
	msgs := helperMakeChatMessages([]struct{ Role, Content string }{
		{"user", "tell me about Go programming"},
		{"assistant", "Go is a statically typed language"},
		{"user", "what about generics?"},
	})

	req := SummarizeBranchRequest{
		Messages: msgs,
		BranchID: "test-branch",
	}

	result, err := summarizer.SummarizeBranch(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.MsgCount != 3 {
		t.Errorf("expected msg_count 3, got %d", result.MsgCount)
	}
	if result.BranchID != "test-branch" {
		t.Errorf("expected branch_id 'test-branch', got %q", result.BranchID)
	}
	if result.Summary == "" {
		t.Error("expected non-empty summary from fallback")
	}
	// Fallback should mention branch ID and message count
	if !strings.Contains(result.Summary, "test-branch") {
		t.Errorf("expected summary to contain branch ID, got %q", result.Summary)
	}
}

func TestSummarizeBranch_ExactlyThreeMessages(t *testing.T) {
	summarizer := NewSummarizer(nil, slog.Default())
	msgs := helperMakeChatMessages([]struct{ Role, Content string }{
		{"user", "first"},
		{"assistant", "second"},
		{"user", "third"},
	})

	req := SummarizeBranchRequest{
		Messages: msgs,
		BranchID: "exact-3",
	}

	result, err := summarizer.SummarizeBranch(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for exactly 3 messages")
	}
}

func TestAssembleBranchForMessages(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, _ := store.Create("test-assemble")
	helperCreateMessages(t, store, session.ID, 3)

	// Get messages back
	msgs, _ := store.GetMessages(session.ID, 0, 10)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// Assemble without tool calls
	chatMsgs := AssembleBranchForMessages(store, msgs)
	if len(chatMsgs) != 3 {
		t.Fatalf("expected 3 chat messages, got %d", len(chatMsgs))
	}

	// Verify first message
	if chatMsgs[0].Content != "message 1" {
		t.Errorf("expected 'message 1', got %q", chatMsgs[0].Content)
	}
}

func TestGetBranches(t *testing.T) {
	bm, store := helperBranchManager(t, nil)
	defer store.Close()

	session, _ := store.Create("test-get-branches")
	helperCreateMessages(t, store, session.ID, 3)

	branches, err := bm.GetBranches(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(branches) != 1 {
		t.Fatalf("expected 1 branch, got %d", len(branches))
	}
	if branches[0].ID != "main" {
		t.Errorf("expected branch ID 'main', got %q", branches[0].ID)
	}
	if branches[0].MessageCount != 3 {
		t.Errorf("expected 3 messages, got %d", branches[0].MessageCount)
	}
}

func TestGetBranches_SessionNotFound(t *testing.T) {
	bm, store := helperBranchManager(t, nil)
	defer store.Close()

	_, err := bm.GetBranches(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
}

func TestIntegration_NavigateAndSummaryPersistence(t *testing.T) {
	ts := &testSummarizer{
		result: &SummarizeBranchResult{
			Summary:  "integration test summary",
			BranchID: "main",
			MsgCount: 4,
		},
	}
	bm, store := helperBranchManager(t, ts)
	defer store.Close()

	session, _ := store.Create("integration-test")
	ids := helperCreateMessages(t, store, session.ID, 7)
	store.SetLeafMessageID(session.ID, ids[6])

	// Navigate from message 7 back to message 3
	result, err := bm.NavigateToBranch(context.Background(), session.ID, ids[2])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify navigation result
	if result.OldLeafID != ids[6] {
		t.Errorf("expected old leaf %d, got %d", ids[6], result.OldLeafID)
	}
	if result.NewLeafID != ids[2] {
		t.Errorf("expected new leaf %d, got %d", ids[2], result.NewLeafID)
	}
	if result.Summary != "integration test summary" {
		t.Errorf("expected summary, got %q", result.Summary)
	}

	// Verify leaf pointer is persisted
	leafID, _ := store.GetLeafMessageID(session.ID)
	if leafID != ids[2] {
		t.Errorf("expected leaf %d, got %d", ids[2], leafID)
	}

	// Verify all messages including summary and branch_point are persisted
	allMsgs, _ := store.GetMessages(session.ID, 0, 100)
	summaryCount := 0
	branchPointCount := 0
	for _, msg := range allMsgs {
		if msg.EntryType == "summary" {
			summaryCount++
			if msg.Content != "integration test summary" {
				t.Errorf("unexpected summary content: %q", msg.Content)
			}
		}
		if msg.EntryType == "branch_point" {
			branchPointCount++
		}
	}
	if summaryCount != 1 {
		t.Errorf("expected 1 summary message, got %d", summaryCount)
	}
	if branchPointCount != 1 {
		t.Errorf("expected 1 branch_point message, got %d", branchPointCount)
	}

	// Total: 7 original + 1 summary + 1 branch_point = 9
	if len(allMsgs) != 9 {
		t.Errorf("expected 9 total messages, got %d", len(allMsgs))
	}
}
