package agent

// Pins for the bughunt 2026-11-14 HIGH finding: the "message"-field
// exemption in toolResultJSONKey was a raw strings.Index byte scan, so a
// "message" key at ANY nesting depth cleared the exemption. A machine dump
// like {"memory_id": ..., "details": {"message": "..."}} passed the guard
// and leaked to the user (and was spoken aloud through the voice lane).
// The fix parses the reply with encoding/json and consults ONLY the
// top-level "message" key; a nested message inside an otherwise
// machine-shaped dump must be caught.

import "testing"

func TestHasNonEmptyMessageField_TopLevelOnly(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  bool
	}{
		{
			name:  "top-level message string",
			reply: `{"success": true, "voted": 1, "message": "Recorded vote +1 on memory 7a4b"}`,
			want:  true,
		},
		{
			name:  "nested message inside a details object",
			reply: `{"memory_id": "3a0dae49", "details": {"message": "leaked prose"}}`,
			want:  false,
		},
		{
			name:  "nested message in an array element",
			reply: `{"jobs": [{"id": "x", "message": "leaked"}]}`,
			want:  false,
		},
		{
			name:  "empty top-level message",
			reply: `{"success": true, "message": ""}`,
			want:  false,
		},
		{
			name:  "non-string top-level message",
			reply: `{"message": {"nested": "object"}}`,
			want:  false,
		},
		{
			name:  "message key holding only whitespace",
			reply: `{"message": "   "}`,
			want:  false,
		},
		{
			name:  "parse failure falls back to machine-shaped",
			reply: `{"message": "unterminated`,
			want:  false,
		},
		{
			name:  "json array is not a message-bearing object",
			reply: `[{"message": "first"}]`,
			want:  false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasNonEmptyMessageField(tt.reply); got != tt.want {
				t.Fatalf("hasNonEmptyMessageField(%s) = %v, want %v", tt.reply, got, tt.want)
			}
		})
	}
}

// End-to-end pin: the nested-message dump must be replaced by the guard
// (the old byte-scan let it through), while the genuinely top-level
// message-bearing terminating-tool reply still passes byte-identical.
func TestReplaceCatalogReply_NestedMessageDumpCaught(t *testing.T) {
	dump := "{\n  \"memory_id\": \"3a0dae49-96c3-4f33-b888-285e57fd39f2\",\n  \"success\": true,\n  \"details\": {\n    \"message\": \"memory saved and verified\"\n  }\n}"
	if hasNonEmptyMessageField(dump) {
		t.Fatal("nested-message dump cleared the message exemption")
	}
	if _, ok := toolResultJSONKey(dump); !ok {
		t.Fatal("nested-message dump not detected as machine-shaped")
	}
	guarded, match, replaced := replaceCatalogReply(dump)
	if !replaced {
		t.Fatal("nested-message dump shipped to the user unguarded")
	}
	if match.Rule != "tool_result_json" {
		t.Fatalf("rule = %q, want tool_result_json", match.Rule)
	}
	if guarded == dump {
		t.Fatal("guard returned the dump unchanged")
	}
}

func TestSpokenGuard_NestedMessageDumpIsSpokenViolation(t *testing.T) {
	// The voice lane shares classifyReplyGuard, so the same nested dump
	// must be classified as a spoken violation before it reaches TTS.
	dump := `{"memory_id": "abc", "success": true, "details": {"message": "memory saved and verified"}}`
	_, trips := SpokenReplyViolation(dump)
	if !trips {
		t.Fatal("nested-message dump must trip SpokenReplyViolation (voice lane)")
	}
}
