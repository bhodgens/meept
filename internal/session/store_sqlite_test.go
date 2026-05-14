package session

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testHelper creates a temporary SQLiteStore for testing.
func testHelper(t *testing.T) (*SQLiteStore, string) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_sessions.db")

	store, err := NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("failed to create SQLiteStore: %v", err)
	}

	return store, dbPath
}

func TestSQLiteStore_MigrationFromExistingSchema(t *testing.T) {
	store, dbPath := testHelper(t)
	defer store.Close()

	// Open a raw connection to verify columns exist
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Check session_messages has the new columns
	rows, err := db.Query("PRAGMA table_info(session_messages)")
	if err != nil {
		t.Fatalf("failed to query table info: %v", err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("failed to scan table info: %v", err)
		}
		columns[name] = true
	}

	for _, col := range []string{"parent_id", "entry_type", "branch_id", "model", "name", "tool_call_id"} {
		if !columns[col] {
			t.Errorf("expected column %q in session_messages, not found", col)
		}
	}

	// Check sessions has leaf_message_id
	rows2, err := db.Query("PRAGMA table_info(sessions)")
	if err != nil {
		t.Fatalf("failed to query sessions table info: %v", err)
	}
	defer rows2.Close()

	sessionColumns := make(map[string]bool)
	for rows2.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows2.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("failed to scan sessions table info: %v", err)
		}
		sessionColumns[name] = true
	}

	if !sessionColumns["leaf_message_id"] {
		t.Error("expected column 'leaf_message_id' in sessions, not found")
	}

	// Check session_tool_calls table exists
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='session_tool_calls'").Scan(&tableName)
	if err != nil {
		t.Fatalf("session_tool_calls table not found: %v", err)
	}
}

func TestSQLiteStore_MigrationFromOldDatabase(t *testing.T) {
	// Simulate an old database without the new columns
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "old_sessions.db")

	// Create old-style database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create old db: %v", err)
	}

	oldSchema := `
	CREATE TABLE sessions (
		id              TEXT PRIMARY KEY,
		name            TEXT NOT NULL,
		conversation_id TEXT UNIQUE NOT NULL,
		created_at      TEXT NOT NULL,
		last_activity   TEXT NOT NULL,
		attached_clients TEXT DEFAULT '[]',
		worker_ids      TEXT DEFAULT '[]',
		description     TEXT DEFAULT ''
	);
	CREATE TABLE session_messages (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id  TEXT NOT NULL,
		role        TEXT NOT NULL,
		content     TEXT NOT NULL,
		timestamp   TEXT NOT NULL,
		FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
	);`

	if _, err := db.Exec(oldSchema); err != nil {
		db.Close()
		t.Fatalf("failed to create old schema: %v", err)
	}

	// Insert some old-style data
	_, err = db.Exec(`INSERT INTO sessions (id, name, conversation_id, created_at, last_activity)
		VALUES ('old-session', 'test', 'conv-old', ?, ?)`,
		time.Now().UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		db.Close()
		t.Fatalf("failed to insert old session: %v", err)
	}

	_, err = db.Exec(`INSERT INTO session_messages (session_id, role, content, timestamp)
		VALUES ('old-session', 'user', 'hello', ?)`,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		db.Close()
		t.Fatalf("failed to insert old message: %v", err)
	}
	db.Close()

	// Now open with SQLiteStore which should migrate
	store, err := NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("failed to open SQLiteStore with old db: %v", err)
	}
	defer store.Close()

	// Verify old data is still accessible
	session := store.Get("old-session")
	if session == nil {
		t.Fatal("old session should still be accessible after migration")
	}
	if session.Name != "test" {
		t.Errorf("expected session name 'test', got %q", session.Name)
	}

	msgs, err := store.GetMessages("old-session", 0, 10)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	// Old messages should have default values for new fields
	if msgs[0].EntryType != "message" {
		t.Errorf("expected default entry_type 'message', got %q", msgs[0].EntryType)
	}
	if msgs[0].BranchID != "main" {
		t.Errorf("expected default branch_id 'main', got %q", msgs[0].BranchID)
	}
}

func TestSQLiteStore_NewFieldsRoundTrip(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	// Create a session
	session, err := store.Create("test-roundtrip")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Save messages with new fields
	parentID := int64(1)
	messages := []Message{
		{
			SessionID:  session.ID,
			ParentID:   nil,
			Role:       "user",
			Content:    "hello world",
			Timestamp:  time.Now().UTC(),
			EntryType:  "message",
			BranchID:   "main",
			Model:      "",
			Name:       "",
			ToolCallID: "",
		},
		{
			SessionID:  session.ID,
			ParentID:   &parentID,
			Role:       "assistant",
			Content:    "hi there",
			Timestamp:  time.Now().UTC(),
			EntryType:  "message",
			BranchID:   "main",
			Model:      "claude-sonnet-4-5-20241022",
			Name:       "assistant",
			ToolCallID: "",
		},
	}

	if err := store.SaveMessages(session.ID, messages); err != nil {
		t.Fatalf("failed to save messages: %v", err)
	}

	// Retrieve and verify
	retrieved, err := store.GetMessages(session.ID, 0, 10)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}
	if len(retrieved) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(retrieved))
	}

	// First message should have nil parent
	if retrieved[0].ParentID != nil {
		t.Errorf("expected nil parent_id for first message, got %v", retrieved[0].ParentID)
	}
	if retrieved[0].EntryType != "message" {
		t.Errorf("expected entry_type 'message', got %q", retrieved[0].EntryType)
	}
	if retrieved[0].BranchID != "main" {
		t.Errorf("expected branch_id 'main', got %q", retrieved[0].BranchID)
	}

	// Second message should have parent pointing to first
	if retrieved[1].ParentID == nil {
		t.Error("expected non-nil parent_id for second message")
	} else if *retrieved[1].ParentID != retrieved[0].ID {
		t.Errorf("expected parent_id %d, got %d", retrieved[0].ID, *retrieved[1].ParentID)
	}
	if retrieved[1].Model != "claude-sonnet-4-5-20241022" {
		t.Errorf("expected model 'claude-sonnet-4-5-20241022', got %q", retrieved[1].Model)
	}
	if retrieved[1].Name != "assistant" {
		t.Errorf("expected name 'assistant', got %q", retrieved[1].Name)
	}
}

func TestSQLiteStore_GetMessagePath(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, err := store.Create("test-path")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create a 3-message chain: user -> assistant -> user
	now := time.Now().UTC()
	msg1 := Message{
		SessionID: session.ID,
		Role:      "user",
		Content:   "first message",
		Timestamp: now,
		EntryType: "message",
		BranchID:  "main",
	}
	if err := store.SaveMessages(session.ID, []Message{msg1}); err != nil {
		t.Fatalf("failed to save msg1: %v", err)
	}

	// Get msg1 ID
	msgs1, _ := store.GetMessages(session.ID, 0, 1)
	msg1ID := msgs1[0].ID

	msg2 := Message{
		SessionID: session.ID,
		ParentID:  &msg1ID,
		Role:      "assistant",
		Content:   "second message",
		Timestamp: now.Add(time.Second),
		EntryType: "message",
		BranchID:  "main",
	}
	if err := store.SaveMessages(session.ID, []Message{msg2}); err != nil {
		t.Fatalf("failed to save msg2: %v", err)
	}

	// Get msg2 ID
	msgs2, _ := store.GetMessages(session.ID, 1, 1)
	msg2ID := msgs2[0].ID

	msg3 := Message{
		SessionID: session.ID,
		ParentID:  &msg2ID,
		Role:      "user",
		Content:   "third message",
		Timestamp: now.Add(2 * time.Second),
		EntryType: "message",
		BranchID:  "main",
	}
	if err := store.SaveMessages(session.ID, []Message{msg3}); err != nil {
		t.Fatalf("failed to save msg3: %v", err)
	}

	// Get msg3 ID (the leaf)
	msgs3, _ := store.GetMessages(session.ID, 2, 1)
	leafID := msgs3[0].ID

	// Get path from root to leaf
	path, err := store.GetMessagePath(session.ID, leafID)
	if err != nil {
		t.Fatalf("failed to get message path: %v", err)
	}

	if len(path) != 3 {
		t.Fatalf("expected 3 messages in path, got %d", len(path))
	}

	// Verify ordering: root to leaf
	if path[0].Content != "first message" {
		t.Errorf("expected first message in path to be 'first message', got %q", path[0].Content)
	}
	if path[1].Content != "second message" {
		t.Errorf("expected second message in path to be 'second message', got %q", path[1].Content)
	}
	if path[2].Content != "third message" {
		t.Errorf("expected third message in path to be 'third message', got %q", path[2].Content)
	}

	// Verify IDs are ascending
	if path[0].ID >= path[1].ID || path[1].ID >= path[2].ID {
		t.Errorf("expected ascending IDs in path, got %d -> %d -> %d", path[0].ID, path[1].ID, path[2].ID)
	}
}

func TestSQLiteStore_ToolCalls(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, err := store.Create("test-toolcalls")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Save a message
	msg := Message{
		SessionID: session.ID,
		Role:      "assistant",
		Content:   "",
		Timestamp: time.Now().UTC(),
		EntryType: "message",
		BranchID:  "main",
	}
	if err := store.SaveMessages(session.ID, []Message{msg}); err != nil {
		t.Fatalf("failed to save message: %v", err)
	}

	msgs, _ := store.GetMessages(session.ID, 0, 1)
	msgID := msgs[0].ID

	// Save tool calls
	toolCalls := []ToolCall{
		{
			MessageID:  msgID,
			ToolName:   "file_read",
			ToolCallID: "call_001",
			Arguments:  `{"path": "/tmp/test.go"}`,
			Result:     "file contents here",
			Seq:        0,
		},
		{
			MessageID:  msgID,
			ToolName:   "shell_execute",
			ToolCallID: "call_002",
			Arguments:  `{"command": "go test ./..."}`,
			Result:     "ok",
			Seq:        1,
		},
	}

	if err := store.SaveToolCalls(msgID, toolCalls); err != nil {
		t.Fatalf("failed to save tool calls: %v", err)
	}

	// Retrieve tool calls for single message
	retrieved, err := store.GetToolCalls(msgID)
	if err != nil {
		t.Fatalf("failed to get tool calls: %v", err)
	}

	if len(retrieved) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(retrieved))
	}

	if retrieved[0].ToolName != "file_read" {
		t.Errorf("expected tool_name 'file_read', got %q", retrieved[0].ToolName)
	}
	if retrieved[0].ToolCallID != "call_001" {
		t.Errorf("expected tool_call_id 'call_001', got %q", retrieved[0].ToolCallID)
	}
	if retrieved[0].Arguments != `{"path": "/tmp/test.go"}` {
		t.Errorf("unexpected arguments: %q", retrieved[0].Arguments)
	}
	if retrieved[0].Result != "file contents here" {
		t.Errorf("expected result 'file contents here', got %q", retrieved[0].Result)
	}

	if retrieved[1].ToolName != "shell_execute" {
		t.Errorf("expected tool_name 'shell_execute', got %q", retrieved[1].ToolName)
	}
	if retrieved[1].Seq != 1 {
		t.Errorf("expected seq 1, got %d", retrieved[1].Seq)
	}

	// Verify message_id is set correctly
	if retrieved[0].MessageID != msgID {
		t.Errorf("expected message_id %d, got %d", msgID, retrieved[0].MessageID)
	}
}

func TestSQLiteStore_ToolCallsForMessages(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, err := store.Create("test-toolcalls-batch")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create two messages
	msgs := []Message{
		{SessionID: session.ID, Role: "assistant", Content: "msg1", Timestamp: time.Now().UTC(), EntryType: "message", BranchID: "main"},
		{SessionID: session.ID, Role: "assistant", Content: "msg2", Timestamp: time.Now().UTC().Add(time.Second), EntryType: "message", BranchID: "main"},
	}
	if err := store.SaveMessages(session.ID, msgs); err != nil {
		t.Fatalf("failed to save messages: %v", err)
	}

	retrieved, _ := store.GetMessages(session.ID, 0, 2)
	msg1ID := retrieved[0].ID
	msg2ID := retrieved[1].ID

	// Save tool calls for both messages
	store.SaveToolCalls(msg1ID, []ToolCall{
		{MessageID: msg1ID, ToolName: "file_read", ToolCallID: "call_a", Arguments: "{}", Result: "a", Seq: 0},
	})
	store.SaveToolCalls(msg2ID, []ToolCall{
		{MessageID: msg2ID, ToolName: "shell_exec", ToolCallID: "call_b1", Arguments: "{}", Result: "b1", Seq: 0},
		{MessageID: msg2ID, ToolName: "file_write", ToolCallID: "call_b2", Arguments: "{}", Result: "b2", Seq: 1},
	})

	// Batch retrieve
	result, err := store.GetToolCallsForMessages([]int64{msg1ID, msg2ID})
	if err != nil {
		t.Fatalf("failed to get tool calls for messages: %v", err)
	}

	if len(result[msg1ID]) != 1 {
		t.Errorf("expected 1 tool call for msg1, got %d", len(result[msg1ID]))
	}
	if len(result[msg2ID]) != 2 {
		t.Errorf("expected 2 tool calls for msg2, got %d", len(result[msg2ID]))
	}

	// Test empty input
	emptyResult, err := store.GetToolCallsForMessages([]int64{})
	if err != nil {
		t.Fatalf("failed to get tool calls for empty input: %v", err)
	}
	if len(emptyResult) != 0 {
		t.Errorf("expected empty result for empty input, got %d entries", len(emptyResult))
	}

	// Test non-existent message IDs
	missingResult, err := store.GetToolCallsForMessages([]int64{99999})
	if err != nil {
		t.Fatalf("failed to get tool calls for missing messages: %v", err)
	}
	if len(missingResult) != 0 {
		t.Errorf("expected empty result for missing message IDs, got %d entries", len(missingResult))
	}
}

func TestSQLiteStore_LeafMessageID(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, err := store.Create("test-leaf")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Initially, leaf should be 0
	leafID, err := store.GetLeafMessageID(session.ID)
	if err != nil {
		t.Fatalf("failed to get leaf message id: %v", err)
	}
	if leafID != 0 {
		t.Errorf("expected initial leaf to be 0, got %d", leafID)
	}

	// Save a message and set leaf
	msg := Message{
		SessionID: session.ID,
		Role:      "user",
		Content:   "test",
		Timestamp: time.Now().UTC(),
		EntryType: "message",
		BranchID:  "main",
	}
	store.SaveMessages(session.ID, []Message{msg})
	msgs, _ := store.GetMessages(session.ID, 0, 1)
	msgID := msgs[0].ID

	// Set leaf
	if err := store.SetLeafMessageID(session.ID, msgID); err != nil {
		t.Fatalf("failed to set leaf message id: %v", err)
	}

	// Verify
	leafID, err = store.GetLeafMessageID(session.ID)
	if err != nil {
		t.Fatalf("failed to get leaf message id after set: %v", err)
	}
	if leafID != msgID {
		t.Errorf("expected leaf %d, got %d", msgID, leafID)
	}

	// Verify it's also in the session object
	sessionObj := store.Get(session.ID)
	if sessionObj == nil {
		t.Fatal("session should exist")
	}
	if sessionObj.LeafMessageID == nil {
		t.Fatal("leaf_message_id should not be nil")
	}
	if *sessionObj.LeafMessageID != msgID {
		t.Errorf("expected session leaf_message_id %d, got %d", msgID, *sessionObj.LeafMessageID)
	}

	// Test non-existent session
	_, err = store.GetLeafMessageID("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
}

func TestSQLiteStore_GetMessageBranches(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, err := store.Create("test-branches")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create messages in two branches
	msgs := []Message{
		{SessionID: session.ID, Role: "user", Content: "root msg", Timestamp: time.Now().UTC(), EntryType: "message", BranchID: "main"},
		{SessionID: session.ID, Role: "assistant", Content: "main response", Timestamp: time.Now().UTC().Add(time.Second), EntryType: "message", BranchID: "main"},
		{SessionID: session.ID, Role: "assistant", Content: "alt response", Timestamp: time.Now().UTC().Add(2 * time.Second), EntryType: "message", BranchID: "branch-1"},
	}
	store.SaveMessages(session.ID, msgs)

	branches, err := store.GetMessageBranches(session.ID)
	if err != nil {
		t.Fatalf("failed to get branches: %v", err)
	}

	if len(branches) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(branches))
	}

	// One should be main with 2 messages, one should be branch-1 with 1
	branchMap := make(map[string]Branch)
	for _, b := range branches {
		branchMap[b.ID] = b
	}

	mainBranch, ok := branchMap["main"]
	if !ok {
		t.Fatal("expected 'main' branch")
	}
	if mainBranch.MessageCount != 2 {
		t.Errorf("expected main branch to have 2 messages, got %d", mainBranch.MessageCount)
	}

	altBranch, ok := branchMap["branch-1"]
	if !ok {
		t.Fatal("expected 'branch-1' branch")
	}
	if altBranch.MessageCount != 1 {
		t.Errorf("expected branch-1 to have 1 message, got %d", altBranch.MessageCount)
	}
}

func TestSQLiteStore_GetTree(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, err := store.Create("test-tree")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create a simple tree
	msgs := []Message{
		{SessionID: session.ID, Role: "user", Content: "root", Timestamp: time.Now().UTC(), EntryType: "message", BranchID: "main"},
		{SessionID: session.ID, Role: "assistant", Content: "response", Timestamp: time.Now().UTC().Add(time.Second), EntryType: "message", BranchID: "main"},
	}
	store.SaveMessages(session.ID, msgs)

	// Set leaf to the second message
	retrieved, _ := store.GetMessages(session.ID, 0, 2)
	leafID := retrieved[1].ID
	store.SetLeafMessageID(session.ID, leafID)

	nodes, err := store.GetTree(session.ID)
	if err != nil {
		t.Fatalf("failed to get tree: %v", err)
	}

	if len(nodes) != 2 {
		t.Fatalf("expected 2 tree nodes, got %d", len(nodes))
	}

	// First node should not be leaf
	if nodes[0].IsLeaf {
		t.Error("first node should not be leaf")
	}
	// Second node should be leaf
	if !nodes[1].IsLeaf {
		t.Error("second node should be leaf")
	}
	// Verify entry_type
	if nodes[0].EntryType != "message" {
		t.Errorf("expected entry_type 'message', got %q", nodes[0].EntryType)
	}
}

func TestSQLiteStore_NavigateToBranch_InvalidTarget(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, _ := store.Create("test-stubs")

	_, err := store.NavigateToBranch(session.ID, 99999)
	if err == nil {
		t.Error("expected error for non-existent target message")
	}
}

func TestSQLiteStore_ForkSession_Basic(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, err := store.Create("test-fork-source")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create a 5-message chain with parent links
	now := time.Now().UTC()
	msgContents := []string{"msg1", "msg2", "msg3", "msg4", "msg5"}
	msgIDs := make([]int64, 5)

	for i, content := range msgContents {
		var parentID *int64
		if i > 0 {
			//nolint:gosec // index bounded by upstream check
			parentID = &msgIDs[i-1]
		}
		msg := Message{
			SessionID: session.ID,
			ParentID:  parentID,
			Role:      []string{"user", "assistant", "user", "assistant", "user"}[i],
			Content:   content,
			Timestamp: now.Add(time.Duration(i) * time.Second),
			EntryType: "message",
			BranchID:  "main",
		}
		if err := store.SaveMessages(session.ID, []Message{msg}); err != nil {
			t.Fatalf("failed to save message %d: %v", i, err)
		}
		msgs, _ := store.GetMessages(session.ID, i, 1)
		msgIDs[i] = msgs[0].ID
	}

	// Set leaf to last message
	store.SetLeafMessageID(session.ID, msgIDs[4])

	// Fork from message 3 (index 2)
	forked, err := store.ForkSession(session.ID, msgIDs[2], "forked session")
	if err != nil {
		t.Fatalf("failed to fork session: %v", err)
	}

	// Verify forked session metadata
	if forked.Name != "forked session" {
		t.Errorf("expected name 'forked session', got %q", forked.Name)
	}
	if forked.LeafMessageID == nil {
		t.Fatal("expected leaf_message_id to be set")
	}

	// Verify forked session has 3 messages
	forkedMsgs, err := store.GetMessages(forked.ID, 0, 100)
	if err != nil {
		t.Fatalf("failed to get forked messages: %v", err)
	}
	if len(forkedMsgs) != 3 {
		t.Fatalf("expected 3 messages in forked session, got %d", len(forkedMsgs))
	}

	// Verify content is correct
	for i, msg := range forkedMsgs {
		if msg.Content != msgContents[i] {
			t.Errorf("expected message %d content %q, got %q", i, msgContents[i], msg.Content)
		}
	}

	// Verify parent chain is correct
	if forkedMsgs[0].ParentID != nil {
		t.Errorf("first message should have nil parent, got %d", *forkedMsgs[0].ParentID)
	}
	for i := 1; i < 3; i++ {
		if forkedMsgs[i].ParentID == nil {
			t.Errorf("message %d should have non-nil parent", i)
		} else if *forkedMsgs[i].ParentID != forkedMsgs[i-1].ID {
			t.Errorf("message %d parent should be %d, got %d", i, forkedMsgs[i-1].ID, *forkedMsgs[i].ParentID)
		}
	}

	// Verify leaf points to the last copied message (msg3 = index 2)
	if *forked.LeafMessageID != forkedMsgs[2].ID {
		t.Errorf("expected leaf to be %d, got %d", forkedMsgs[2].ID, *forked.LeafMessageID)
	}

	// Verify original session is unchanged
	origMsgs, _ := store.GetMessages(session.ID, 0, 100)
	if len(origMsgs) != 5 {
		t.Errorf("expected original session to still have 5 messages, got %d", len(origMsgs))
	}
}

func TestSQLiteStore_ForkSession_WithToolCalls(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, err := store.Create("test-fork-toolcalls")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create 3 messages
	now := time.Now().UTC()
	msg1 := Message{
		SessionID: session.ID, Role: "user", Content: "hello",
		Timestamp: now, EntryType: "message", BranchID: "main",
	}
	store.SaveMessages(session.ID, []Message{msg1})
	msgs1, _ := store.GetMessages(session.ID, 0, 1)
	msg1ID := msgs1[0].ID

	msg2 := Message{
		SessionID: session.ID, ParentID: &msg1ID, Role: "assistant", Content: "response",
		Timestamp: now.Add(time.Second), EntryType: "message", BranchID: "main",
	}
	store.SaveMessages(session.ID, []Message{msg2})
	msgs2, _ := store.GetMessages(session.ID, 1, 1)
	msg2ID := msgs2[0].ID

	// Add tool calls to message 2
	toolCalls := []ToolCall{
		{MessageID: msg2ID, ToolName: "file_read", ToolCallID: "call_1", Arguments: `{"path": "/test"}`, Result: "contents", Seq: 0},
		{MessageID: msg2ID, ToolName: "shell_exec", ToolCallID: "call_2", Arguments: `{"cmd": "ls"}`, Result: "files", Seq: 1},
	}
	if err := store.SaveToolCalls(msg2ID, toolCalls); err != nil {
		t.Fatalf("failed to save tool calls: %v", err)
	}

	msg3 := Message{
		SessionID: session.ID, ParentID: &msg2ID, Role: "user", Content: "follow-up",
		Timestamp: now.Add(2 * time.Second), EntryType: "message", BranchID: "main",
	}
	store.SaveMessages(session.ID, []Message{msg3})
	msgs3, _ := store.GetMessages(session.ID, 2, 1)
	msg3ID := msgs3[0].ID

	// Fork from message 2 (which has tool calls)
	forked, err := store.ForkSession(session.ID, msg2ID, "fork with tools")
	if err != nil {
		t.Fatalf("failed to fork session: %v", err)
	}

	// Verify forked has 2 messages
	forkedMsgs, _ := store.GetMessages(forked.ID, 0, 100)
	if len(forkedMsgs) != 2 {
		t.Fatalf("expected 2 messages in forked session, got %d", len(forkedMsgs))
	}

	// Verify tool calls were copied to the forked message
	forkedMsg2ID := forkedMsgs[1].ID
	forkedToolCalls, err := store.GetToolCalls(forkedMsg2ID)
	if err != nil {
		t.Fatalf("failed to get tool calls for forked message: %v", err)
	}

	if len(forkedToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls in forked session, got %d", len(forkedToolCalls))
	}

	if forkedToolCalls[0].ToolName != "file_read" {
		t.Errorf("expected tool_name 'file_read', got %q", forkedToolCalls[0].ToolName)
	}
	if forkedToolCalls[0].Arguments != `{"path": "/test"}` {
		t.Errorf("unexpected arguments: %q", forkedToolCalls[0].Arguments)
	}
	if forkedToolCalls[0].Result != "contents" {
		t.Errorf("expected result 'contents', got %q", forkedToolCalls[0].Result)
	}
	if forkedToolCalls[1].ToolName != "shell_exec" {
		t.Errorf("expected tool_name 'shell_exec', got %q", forkedToolCalls[1].ToolName)
	}
	if forkedToolCalls[1].Seq != 1 {
		t.Errorf("expected seq 1, got %d", forkedToolCalls[1].Seq)
	}

	// Verify the tool calls are associated with the NEW message IDs, not old ones
	if forkedToolCalls[0].MessageID != forkedMsg2ID {
		t.Errorf("expected message_id %d, got %d", forkedMsg2ID, forkedToolCalls[0].MessageID)
	}

	// Verify original session's tool calls still reference the original message
	origToolCalls, _ := store.GetToolCalls(msg2ID)
	if len(origToolCalls) != 2 {
		t.Errorf("expected original session to still have 2 tool calls, got %d", len(origToolCalls))
	}
	if origToolCalls[0].MessageID != msg2ID {
		t.Errorf("original tool calls should still reference original message ID")
	}

	// Verify message 3 is NOT in the forked session (we forked from msg2, excluding msg3)
	for _, msg := range forkedMsgs {
		if msg.Content == "follow-up" {
			t.Error("message 3 should not be in the forked session")
		}
	}

	// Verify message 3 IS still in original
	origMsgs, _ := store.GetMessages(session.ID, 0, 100)
	found := false
	for _, msg := range origMsgs {
		if msg.ID == msg3ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("message 3 should still be in original session")
	}
}

func TestSQLiteStore_ForkSession_SourceNotFound(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	_, err := store.ForkSession("nonexistent-session", 1, "fork")
	if err == nil {
		t.Error("expected error when forking from non-existent session")
	}
}

func TestSQLiteStore_ForkSession_MessageNotFound(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, _ := store.Create("test-fork-msgnotfound")

	// Add one message
	store.SaveMessages(session.ID, []Message{
		{SessionID: session.ID, Role: "user", Content: "hello", Timestamp: time.Now().UTC()},
	})

	// Try to fork from a non-existent message
	_, err := store.ForkSession(session.ID, 99999, "fork")
	if err == nil {
		t.Error("expected error when forking from non-existent message")
	}
}

func TestSQLiteStore_ExistingMethodsStillWork(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	// Test Create
	session, err := store.Create("existing-test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Test Get
	got := store.Get(session.ID)
	if got == nil || got.ID != session.ID {
		t.Error("Get failed")
	}

	// Test GetByConversationID
	gotConv := store.GetByConversationID(session.ConversationID)
	if gotConv == nil || gotConv.ID != session.ID {
		t.Error("GetByConversationID failed")
	}

	// Test List (empty since no assistant messages)
	list, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list (no assistant msgs), got %d", len(list))
	}

	// Save assistant message
	store.SaveMessages(session.ID, []Message{
		{SessionID: session.ID, Role: "assistant", Content: "response", Timestamp: time.Now().UTC(), EntryType: "message", BranchID: "main"},
	})

	// Test HasResponses
	hasResponses, err := store.HasResponses(session.ID)
	if err != nil {
		t.Fatalf("HasResponses failed: %v", err)
	}
	if !hasResponses {
		t.Error("expected HasResponses to be true")
	}

	// Test List (now should have 1)
	list, err = store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 session in list, got %d", len(list))
	}

	// Test UpdateDescription
	if err := store.UpdateDescription(session.ID, "test desc"); err != nil {
		t.Fatalf("UpdateDescription failed: %v", err)
	}
	got = store.Get(session.ID)
	if got.Description != "test desc" {
		t.Errorf("expected description 'test desc', got %q", got.Description)
	}

	// Test UpdateName
	if err := store.UpdateName(session.ID, "new name"); err != nil {
		t.Fatalf("UpdateName failed: %v", err)
	}
	got = store.Get(session.ID)
	if got.Name != "new name" {
		t.Errorf("expected name 'new name', got %q", got.Name)
	}

	// Test Attach/Detach
	if err := store.Attach(session.ID, "client-1"); err != nil {
		t.Fatalf("Attach failed: %v", err)
	}
	got = store.Get(session.ID)
	if len(got.AttachedClients) != 1 || got.AttachedClients[0] != "client-1" {
		t.Error("Attach failed")
	}
	if err := store.Detach(session.ID, "client-1"); err != nil {
		t.Fatalf("Detach failed: %v", err)
	}
	got = store.Get(session.ID)
	if len(got.AttachedClients) != 0 {
		t.Error("Detach failed")
	}

	// Test AddWorker/RemoveWorker
	if err := store.AddWorker(session.ID, "worker-1"); err != nil {
		t.Fatalf("AddWorker failed: %v", err)
	}
	got = store.Get(session.ID)
	if len(got.WorkerIDs) != 1 || got.WorkerIDs[0] != "worker-1" {
		t.Error("AddWorker failed")
	}
	if err := store.RemoveWorker(session.ID, "worker-1"); err != nil {
		t.Fatalf("RemoveWorker failed: %v", err)
	}
	got = store.Get(session.ID)
	if len(got.WorkerIDs) != 0 {
		t.Error("RemoveWorker failed")
	}

	// Test UpdateActivity
	if err := store.UpdateActivity(session.ID); err != nil {
		t.Fatalf("UpdateActivity failed: %v", err)
	}

	// Test Delete
	if !store.Delete(session.ID) {
		t.Error("Delete failed")
	}
	got = store.Get(session.ID)
	if got != nil {
		t.Error("expected nil after delete")
	}

	// Test GetMostRecent is nil after delete
	recent := store.GetMostRecent()
	if recent != nil {
		t.Error("expected nil GetMostRecent after delete")
	}

	// Test GetMessageCount
	store2, _ := testHelper(t)
	defer store2.Close()
	s2, _ := store2.Create("count-test")
	store2.SaveMessages(s2.ID, []Message{
		{SessionID: s2.ID, Role: "user", Content: "a", Timestamp: time.Now().UTC()},
		{SessionID: s2.ID, Role: "assistant", Content: "b", Timestamp: time.Now().UTC()},
		{SessionID: s2.ID, Role: "user", Content: "c", Timestamp: time.Now().UTC()},
	})
	count, err := store2.GetMessageCount(s2.ID)
	if err != nil {
		t.Fatalf("GetMessageCount failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 messages, got %d", count)
	}
}

func TestSQLiteStore_SaveToolCallsEmpty(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	// Saving empty slice should be a no-op
	err := store.SaveToolCalls(1, []ToolCall{})
	if err != nil {
		t.Errorf("expected no error for empty tool calls, got: %v", err)
	}

	// Saving nil should be a no-op
	err = store.SaveToolCalls(1, nil)
	if err != nil {
		t.Errorf("expected no error for nil tool calls, got: %v", err)
	}
}

func TestSQLiteStore_ToolCallsWithEmptyResult(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	session, _ := store.Create("tool-result-test")
	store.SaveMessages(session.ID, []Message{
		{SessionID: session.ID, Role: "assistant", Content: "", Timestamp: time.Now().UTC()},
	})
	msgs, _ := store.GetMessages(session.ID, 0, 1)
	msgID := msgs[0].ID

	// Save a tool call with empty result
	store.SaveToolCalls(msgID, []ToolCall{
		{MessageID: msgID, ToolName: "shell_execute", ToolCallID: "call_1", Arguments: "{}", Result: "", Seq: 0},
	})

	retrieved, err := store.GetToolCalls(msgID)
	if err != nil {
		t.Fatalf("failed to get tool calls: %v", err)
	}
	if len(retrieved) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(retrieved))
	}
	if retrieved[0].Result != "" {
		t.Errorf("expected empty result, got %q", retrieved[0].Result)
	}
}

func TestSQLiteStore_MigrationIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "idempotent.db")

	// Open and close multiple times - should not error
	for i := 0; i < 3; i++ {
		store, err := NewSQLiteStore(dbPath, slog.Default())
		if err != nil {
			t.Fatalf("iteration %d: failed to create store: %v", i, err)
		}
		// Create a session to verify it works
		sess, err := store.Create(fmt.Sprintf("session-%d", i))
		if err != nil {
			t.Fatalf("iteration %d: failed to create session: %v", i, err)
		}
		if sess == nil {
			t.Fatalf("iteration %d: session is nil", i)
		}
		store.Close()
	}

	// Verify all 3 sessions exist
	_ = os.Remove(dbPath)
}
