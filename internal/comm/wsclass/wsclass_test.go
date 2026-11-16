package wsclass

import "testing"

// TestWSClassConstants pins iota ordering: WSChatMessage==0 .. WSEvent==5,
// WSBoard==6 (appended after WSEvent by kanban-board C8 leaf 05/07 so the
// 0..5 prefix stays byte-stable for any persisted/serialized class ints).
func TestWSClassConstants(t *testing.T) {
	if WSChatMessage != 0 {
		t.Errorf("WSChatMessage = %d, want 0", WSChatMessage)
	}
	if WSProgress != 1 {
		t.Errorf("WSProgress = %d, want 1", WSProgress)
	}
	if WSMetricsUpdate != 2 {
		t.Errorf("WSMetricsUpdate = %d, want 2", WSMetricsUpdate)
	}
	if WSJobUpdate != 3 {
		t.Errorf("WSJobUpdate = %d, want 3", WSJobUpdate)
	}
	if WSPlanUpdate != 4 {
		t.Errorf("WSPlanUpdate = %d, want 4", WSPlanUpdate)
	}
	if WSEvent != 5 {
		t.Errorf("WSEvent = %d, want 5", WSEvent)
	}
	if WSBoard != 6 {
		t.Errorf("WSBoard = %d, want 6", WSBoard)
	}
}

// TestWSClassStringMapping pins the exact wire strings for every constant
// (the frontend "type" field).
func TestWSClassStringMapping(t *testing.T) {
	cases := []struct {
		c    WSClass
		want string
	}{
		{WSChatMessage, "chat_message"},
		{WSProgress, "agent_progress"},
		{WSMetricsUpdate, "metrics_update"},
		{WSJobUpdate, "job_update"},
		{WSPlanUpdate, "plan_update"},
		{WSEvent, "event"},
		{WSBoard, "board_event"},
	}
	for _, tc := range cases {
		if got := tc.c.String(); got != tc.want {
			t.Errorf("WSClass(%d).String() = %q, want %q", tc.c, got, tc.want)
		}
	}
}
