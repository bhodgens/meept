package voice

// Structured call brief (leaf 04, uptractor-ports tree; contract C4).
//
// Ported from uptractor phone/core/internal/agent/callbrief.go +
// phone/core/internal/jobfile/callbrief.go (branch
// fix/coordinator-missed-hangup-watchdog) — stub-at-answer /
// finalize-at-release / exactly-one-brief semantics kept, domain fields
// dropped (master A2: meept's brief schema is GENERIC; Uptractor-domain
// fields ride the opaque ext JSON that meept never parses).
//
// Lifecycle: WriteBriefStub at answer (status in_progress); FinalizeBrief
// at release (completed / failed / abandoned) — exactly one brief per
// call, idempotent across webhook replay AND restart (finalize after a
// terminal status is a no-op, never retried). The full C4 envelope JSON
// travels together in the voice_calls.brief column so status and ext are
// always consistent. The CommitmentFiler seam (brief_seam.go) keeps
// internal/voice free of any internal/ledger import (repo posture).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Brief statuses (frozen C4 enum).
const (
	BriefStatusInProgress = "in_progress"
	BriefStatusCompleted  = "completed"
	BriefStatusFailed     = "failed"
	BriefStatusAbandoned  = "abandoned"
)

// CallBrief is the frozen C4 envelope (master.md C4, byte-for-byte key
// set: status, error, from, to, brief, ext — no additions). Ext is opaque
// JSON meept never types or parses (assumption A2).
type CallBrief struct {
	Status string          `json:"status"` // in_progress|completed|failed|abandoned
	Error  string          `json:"error"`
	From   string          `json:"from"`
	To     string          `json:"to"`
	Brief  BriefBody       `json:"brief"`
	Ext    json.RawMessage `json:"ext"` // opaque; meept never parses
}

// BriefBody is the frozen C4 inner object (transcript, summary,
// action_items, commitments, risk_flags).
type BriefBody struct {
	Transcript  string   `json:"transcript"`
	Summary     string   `json:"summary"`
	ActionItems []string `json:"action_items"`
	Commitments []string `json:"commitments"`
	RiskFlags   []string `json:"risk_flags"`
}

// BriefAnalyzer is the LLM seam: the employee stack provides the
// implementation, tests fake it. It runs OFF the voice hot path
// (post-release, under the wiring's context timeout).
type BriefAnalyzer interface {
	Analyze(ctx context.Context, transcript string, ccid string) (*BriefBody, error)
}

// briefStub returns the in_progress stub envelope for a call.
func briefStub(from, to string) *CallBrief {
	return &CallBrief{
		Status: BriefStatusInProgress,
		From:   from,
		To:     to,
	}
}

// encodeBrief marshals the envelope for the brief column.
func encodeBrief(b *CallBrief) (string, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return "", fmt.Errorf("voice callbrief: encode: %w", err)
	}
	return string(raw), nil
}

// decodeBrief parses a stored brief column value; an empty or unparsable
// value yields a nil brief (no row content yet).
func decodeBrief(raw string) (*CallBrief, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var b CallBrief
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return nil, fmt.Errorf("voice callbrief: decode: %w", err)
	}
	return &b, nil
}

// WriteBriefStub writes the in_progress stub brief for callID (answer
// time). Idempotent in itself (a replayed answered webhook just rewrites
// the same stub); the terminal status is never overwritten here because
// finalize writes only from non-terminal states.
func (s *CallStore) WriteBriefStub(ctx context.Context, callID, from, to string) error {
	raw, err := encodeBrief(briefStub(from, to))
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE voice_calls SET brief = ? WHERE call_id = ?`,
		raw, callID,
	)
	if err != nil {
		return fmt.Errorf("voice callbrief: write stub for %s: %w", callID, err)
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n == 0 {
		return fmt.Errorf("voice callbrief: write stub for %s: %w", callID, ErrCallNotFound)
	}
	return nil
}

// FinalizeBrief stores the final brief. Exactly-one-brief guarantee: only
// an in_progress (or empty) brief is overwritten; a terminal status
// (completed/failed/abandoned) makes this a no-op, so a replayed hangup,
// a re-finalize after a failed analysis, or a restart race can never
// mutate or retry the stored brief.
func (s *CallStore) FinalizeBrief(ctx context.Context, callID string, b *CallBrief) error {
	if b == nil {
		return fmt.Errorf("voice callbrief: finalize %s: nil brief", callID)
	}
	raw, err := encodeBrief(b)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE voice_calls SET brief = ?
		 WHERE call_id = ?
		   AND (brief = '' OR brief IS NULL
		        OR json_extract(brief, '$.status') IN ('`+BriefStatusInProgress+`'))`,
		raw, callID,
	)
	if err != nil {
		return fmt.Errorf("voice callbrief: finalize %s: %w", callID, err)
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n == 0 {
		// Terminal brief already stored: the replay/retry lost, exactly
		// as pinned. Read the winner back only to distinguish "unknown
		// call" (an error) from "idempotent no-op" (success).
		cur, cerr := s.Brief(ctx, callID)
		if cerr != nil {
			return cerr
		}
		if cur == nil {
			return fmt.Errorf("voice callbrief: finalize %s: %w", callID, ErrCallNotFound)
		}
	}
	return nil
}

// Brief reads the stored brief for callID; ok=false (nil, nil error) when
// the call exists but has no brief content yet (old rows / pre-answer).
func (s *CallStore) Brief(ctx context.Context, callID string) (*CallBrief, error) {
	var raw string
	err := s.db.QueryRowContext(ctx,
		`SELECT brief FROM voice_calls WHERE call_id = ?`, callID,
	).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("voice callbrief: read %s: %w", callID, err)
	}
	return decodeBrief(raw)
}

// SetBriefExt merges ext into the stored brief without touching anything
// else (the meept-uptractor write lane entry point). A call with no brief
// content yet is an error: ext rides the C4 envelope, never a bare row.
func (s *CallStore) SetBriefExt(ctx context.Context, callID string, ext json.RawMessage) error {
	if len(ext) == 0 {
		return fmt.Errorf("voice callbrief: set ext %s: empty ext", callID)
	}
	if !json.Valid(ext) {
		return fmt.Errorf("voice callbrief: set ext %s: ext is not valid JSON", callID)
	}
	cur, err := s.Brief(ctx, callID)
	if err != nil {
		return err
	}
	if cur == nil {
		return fmt.Errorf("voice callbrief: set ext %s: %w", callID, ErrCallNotFound)
	}
	cur.Ext = ext
	raw, err := encodeBrief(cur)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE voice_calls SET brief = ? WHERE call_id = ?`, raw, callID,
	); err != nil {
		return fmt.Errorf("voice callbrief: set ext %s: %w", callID, err)
	}
	return nil
}

// abandonStubs marks every stub brief abandoned (single statement,
// single connection, no cursor).
func (s *CallStore) abandonStubs(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE voice_calls SET brief = json_set(brief, '$.status', json_quote('`+BriefStatusAbandoned+`'))
		 WHERE brief != '' AND json_valid(brief)
		   AND json_extract(brief, '$.status') = '`+BriefStatusInProgress+`'`,
	); err != nil {
		return fmt.Errorf("voice callbrief: abandon stubs: %w", err)
	}
	return nil
}
