package agent

import "testing"

// Pin for the 2026-11-09 voice-companion defect: a talk-only employee ran
// schedule_list mid-call (its tools_forbidden wildcard had been stripped at
// hire) and the raw {"jobs": [...]} result shipped as the reply. The voice
// talk loop then fed the JSON to TTS verbatim - the caller heard garbage
// audio. toolResultJSONKey must classify the generic platform-result shapes
// (jobs/results/items/records arrays, bare success objects, status strings)
// as machine dumps so the reply guard replaces them.
func TestToolResultJSONKey_SchedulerJobs(t *testing.T) {
	reply := "{\n  \"jobs\": [\n    {\n      \"id\": \"employee.periodic_audit\",\n      \"type\": \"interval\",\n      \"enabled\": true\n    }\n  ]\n}"
	key, ok := toolResultJSONKey(reply)
	if !ok {
		t.Fatalf("schedule_list jobs dump not detected as machine-shaped (key=%q)", key)
	}
	if key != `"jobs"` {
		t.Fatalf("expected jobs key, got %q", key)
	}
}

func TestToolResultJSONKey_GenericResultShapes(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  string
	}{
		{
			name:  "results array",
			reply: "{\"results\": [{\"id\": \"a\"}]}",
			want:  `"results"`,
		},
		{
			name:  "items array",
			reply: "{\"items\": []}",
			want:  `"items"`,
		},
		{
			name:  "records array",
			reply: "{\"records\": []}",
			want:  `"records"`,
		},
		{
			name:  "bare success object",
			reply: "{\"success\": true}",
			want:  `"success": true`,
		},
		{
			name:  "status string",
			reply: "{\"status\": \"completed\"}",
			want:  `"status": "`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := toolResultJSONKey(tt.reply)
			if !ok {
				t.Fatalf("%s not detected as machine-shaped", tt.reply)
			}
			if key != tt.want {
				t.Fatalf("key = %q, want %q", key, tt.want)
			}
		})
	}
}

func TestReplaceCatalogReply_SchedulerJobsReplaced(t *testing.T) {
	reply := "{\"jobs\": [{\"id\": \"x\", \"enabled\": true}]}"
	guarded, match, replaced := replaceCatalogReply(reply)
	if !replaced {
		t.Fatalf("raw jobs dump shipped to the user")
	}
	if match.Rule != "tool_result_json" {
		t.Fatalf("rule = %q, want tool_result_json", match.Rule)
	}
	if guarded == reply {
		t.Fatalf("guard returned the dump unchanged")
	}
}

// The memory_vote terminating-tool reply is evidence + a human "message"
// field: {"success": true, "voted": 1, "net_votes": 1, "message": "Recorded
// vote +1 on memory <id> (net votes: 1)"}. The generic tool-shape keys would
// trip on "success": true, but a tool result that carries its own
// user-facing message is the DESIGNED answer (memory_vote is a
// TerminatingTool whose result becomes the turn reply), not a bare dump.
// Pin: the guard must pass it through byte-identical.
func TestToolResultJSONKey_MessageBearingToolReplyPasses(t *testing.T) {
	reply := "{\n  \"message\": \"Recorded vote +1 on memory 7a4b-uuid (net votes: 1)\",\n  \"net_votes\": 1,\n  \"success\": true,\n  \"voted\": 1\n}"
	if _, _, replaced := replaceCatalogReply(reply); replaced {
		t.Fatal("message-bearing terminating-tool reply was replaced by the guard")
	}
	if _, ok := toolResultJSONKey(reply); ok {
		t.Fatal("toolResultJSONKey classified a message-bearing tool reply as a dump")
	}
}

// A bare success dump with NO message field (or an empty one) is still a
// machine dump: the message exception must not open a hole for the
// schedule_list-style replies.
func TestToolResultJSONKey_EmptyOrMissingMessageStillDump(t *testing.T) {
	cases := []string{
		`{"success": true}`,
		`{"success": true, "message": ""}`,
		`{"jobs": [{"id": "x"}]}`,
	}
	for _, reply := range cases {
		if _, ok := toolResultJSONKey(reply); !ok {
			t.Fatalf("%s not detected as machine-shaped", reply)
		}
	}
}

// Prose answers mentioning a schedule must NOT be replaced: the guard
// replaces only pure-JSON replies.
func TestReplaceCatalogReply_ProseMentioningJobsPasses(t *testing.T) {
	reply := "You have one job scheduled: the periodic audit runs every six hours."
	if _, _, replaced := replaceCatalogReply(reply); replaced {
		t.Fatalf("prose answer was replaced")
	}
}
