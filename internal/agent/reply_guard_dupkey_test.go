package agent

// M-5 (bughunt 2026-11-14 round 2) pin: the "message"-field exemption must
// decide on the FIRST top-level "message" key. Go's encoding/json
// map-unmarshal used duplicate-key LAST-WINS semantics, so
// {"message":"","x":1,"message":"prose"} re-opened the exemption for a
// machine dump whose payload then leaked to the user. The streamed decoder
// keeps FIRST-WINS (conservative): once the exemption is decided on the
// first key, later duplicate keys cannot change the verdict.

import "testing"

// TestHasNonEmptyMessageField_DuplicateKeyFirstWins pins the adversarial
// duplicate-key shapes.
func TestHasNonEmptyMessageField_DuplicateKeyFirstWins(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  bool
	}{
		{
			// The adversarial re-open: empty first, prose second.
			// First-wins => NOT exempt => machine-shaped.
			name:  "duplicate message empty-then-prose does not re-open the exemption",
			reply: `{"message":"","x":1,"message":"prose answer for the user"}`,
			want:  false,
		},
		{
			// Conservative direction: prose first, empty second still
			// exempts (first decision stands).
			name:  "duplicate message prose-then-empty keeps the exemption",
			reply: `{"message":"the real answer","x":1,"message":""}`,
			want:  true,
		},
		{
			// No trailing garbage after the decided key: the decoder must
			// stop at the first decision.
			name:  "first-key decision stops before sibling keys",
			reply: `{"message":"answer","memory_id":"abc","success":true}`,
			want:  true,
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

// TestReplyGuard_DuplicateKeyReopenCaught pins the end-to-end guard
// behaviour for the adversarial shape: it must be replaced, not shipped.
func TestReplyGuard_DuplicateKeyReopenCaught(t *testing.T) {
	dump := `{"message":"","memory_id":"3a0dae49","success":true,"message":"memory saved and verified, all done!"}`
	if hasNonEmptyMessageField(dump) {
		t.Fatal("duplicate-key shape cleared the message exemption — first-wins must hold")
	}
	if _, ok := toolResultJSONKey(dump); !ok {
		t.Fatal("duplicate-key dump not detected as machine-shaped")
	}
	guarded, _, replaced := replaceCatalogReply(dump)
	if !replaced {
		t.Fatal("duplicate-key dump shipped to the user unguarded")
	}
	if guarded == dump {
		t.Fatal("guard returned the dump unchanged")
	}
}
