package session

import (
	"path/filepath"
	"testing"
)

// L5 pin: the message metadata side channel must survive the three SQLite
// paths that omit it in the original leaf-04 change: ForkSession's copy
// INSERT, GetMessagePath's recursive SELECT, and InsertCompaction's INSERT.
// The marker (e.g. planner_interview) is the only consumer today, so losing
// it silently turns a forked/compacted/path-read interview message into a
// normal one.
func TestSessionMetadataSurvivesPaths(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "sessions.db"), testLoggerOrNil())
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	const convID = "conv-metadata-paths"
	if _, err := s.Ensure(convID, "metadata paths"); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// Two messages: a root carrying the marker and a child without one.
	rootMsg := Message{
		SessionID: convID,
		Role:      "assistant",
		Content:   "interview question",
		Metadata:  map[string]string{"planner_interview": "task-42"},
	}
	if err := s.SaveMessages(convID, []Message{rootMsg}); err != nil {
		t.Fatalf("save root: %v", err)
	}
	roots, err := s.GetMessages(convID, 0, 10)
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("roots = %d, want 1", len(roots))
	}
	rootID := roots[0].ID
	if err := s.SaveMessages(convID, []Message{
		{SessionID: convID, Role: "user", Content: "my answers", ParentID: &rootID},
	}); err != nil {
		t.Fatalf("save child: %v", err)
	}

	all, err := s.GetMessages(convID, 0, 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d messages, want 2", len(all))
	}

	// (1) GetMessagePath must carry metadata for both root and child.
	path, err := s.GetMessagePath(convID, all[1].ID)
	if err != nil {
		t.Fatalf("get message path: %v", err)
	}
	if len(path) != 2 {
		t.Fatalf("path len = %d, want 2", len(path))
	}
	if path[0].Metadata["planner_interview"] != "task-42" {
		t.Errorf("GetMessagePath lost root metadata: %#v", path[0].Metadata)
	}

	// (2) ForkSession's copy INSERT must carry metadata onto the fork.
	forked, err := s.ForkSession(convID, all[1].ID, "metadata fork")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	forkedMsgs, err := s.GetMessages(forked.ID, 0, 10)
	if err != nil {
		t.Fatalf("get forked: %v", err)
	}
	if len(forkedMsgs) != 2 {
		t.Fatalf("forked messages = %d, want 2", len(forkedMsgs))
	}
	if forkedMsgs[0].Metadata["planner_interview"] != "task-42" {
		t.Errorf("ForkSession lost root metadata: %#v", forkedMsgs[0].Metadata)
	}
	if len(forkedMsgs[1].Metadata) != 0 {
		t.Errorf("ForkSession invented metadata on child: %#v", forkedMsgs[1].Metadata)
	}

	// (3) A compaction row must be storable alongside a marked root without
	// disturbing it: the INSERT names the metadata column explicitly.
	if _, err := s.InsertCompaction(convID, rootID, "summary", []int64{rootID}); err != nil {
		t.Fatalf("insert compaction: %v", err)
	}
	compacted, err := s.GetMessages(convID, 0, 10)
	if err != nil {
		t.Fatalf("get after compaction: %v", err)
	}
	found := false
	for _, m := range compacted {
		if m.Metadata["planner_interview"] == "task-42" {
			found = true
		}
	}
	if !found {
		t.Error("marked message metadata lost after compaction insert")
	}
}
