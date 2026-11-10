package daemon

// ledger_brief_records.go — branch 03-voice impl leaf 03-voice/02:
// the frozen brief -> ledger-row mapping (00T-voice §2 / 02-commitments.md).
//
// This file is the SINGLE OWNER of briefResult + ActionItem (03-voice/01
// imports-not-redeclares them) and of appendBriefRecords, exposed to the
// ReleaseHook wiring through the package var seam appendBriefRecordsFn
// (same package, init-order safe).
//
// Frozen mapping (00T §2):
//   - one committal row, subject "call:"+ccid, payload = full brief;
//   - per action_item a commitment row, same subject;
//   - the FIRST commitment also sets status: owner "them" ->
//     waiting_on_them, else open; Summary = first item text;
//   - non-empty follow_up_message -> a commitment row with payload
//     {"type":"follow_up_sms","text":...};
//   - risk_flags -> one observation row payload {"flags":[...]};
//   - ALL rows Source=voice, SourceRef=ccid; ordering committal ->
//     commitments -> observation.
//
// Summary hygiene rationale (task 3): a phone-shaped run inside an item's
// text gets replaced by "(number on file)" BEFORE the text reaches the
// ledger_state Summary, because Summary is rendered into day-brief prompts
// and surfaces downstream where a raw number must never appear. The
// payload JSON keeps the ORIGINAL text: masking is display-path only —
// the ledger is the immutable source of truth and must not lose data.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/caimlas/meept/internal/ledger"
)

// briefResult is the frozen brief JSON contract (mirrors uptractor
// turn.ts:848-901):
//
//	{"summary":"", "action_items":[{"text":"","owner":"me|them","due":""}],
//	 "commitments":[""], "follow_up_message":"", "risk_flags":[""]}
type briefResult struct {
	Summary         string       `json:"summary"`
	ActionItems     []ActionItem `json:"action_items"`
	Commitments     []string     `json:"commitments"`
	FollowUpMessage string       `json:"follow_up_message"`
	RiskFlags       []string     `json:"risk_flags"`
}

// ActionItem is one entry of briefResult.ActionItems (nested item type
// name is this leaf's choice per 00T; the gate reads it only via json).
type ActionItem struct {
	Text  string `json:"text"`
	Owner string `json:"owner"`
	Due   string `json:"due"`
}

// appendBriefRecordsFn is the package seam 03-voice/01's ReleaseHook
// calls (task 4). Same package, init-order safe.
var appendBriefRecordsFn = appendBriefRecords

// ledgerBriefPhoneRun matches a phone-shaped run: '+' then a long digit
// sequence (03-voice/02 task 3).
var ledgerBriefPhoneRun = regexp.MustCompile(`\+\d{7,}`)

// maskLedgerBriefPhones replaces phone-shaped runs with the
// "(number on file)" placeholder. Display-path only — never call this on
// payload bytes.
func maskLedgerBriefPhones(s string) string {
	return ledgerBriefPhoneRun.ReplaceAllString(s, "(number on file)")
}

// appendBriefRecords maps a call brief onto ledger rows per the frozen
// §2 mapping and returns the number of rows appended. Ordering is
// committal -> commitments (action items, then follow_up) -> observation;
// each Append is its own durable transaction (no cross-row atomicity is
// pinned by the contract).
func appendBriefRecords(ctx context.Context, store *ledger.Store,
	employeeID, ccid string, brief *briefResult) (int, error) {

	if store == nil {
		return 0, fmt.Errorf("appendBriefRecords: nil ledger store")
	}
	if brief == nil {
		return 0, fmt.Errorf("appendBriefRecords: nil brief")
	}

	subject := "call:" + ccid
	written := 0

	appendRow := func(d ledger.DraftRecord) error {
		d.EmployeeID = employeeID
		d.Source = ledger.SourceVoice
		d.SourceRef = ccid
		if _, err := store.Append(ctx, d); err != nil {
			return fmt.Errorf("appendBriefRecords: append %s row: %w", d.Kind, err)
		}
		written++
		return nil
	}

	// 1. Committal row: the full brief verbatim (original text preserved —
	// no masking anywhere near the payload).
	briefJSON, err := json.Marshal(brief)
	if err != nil {
		return written, fmt.Errorf("appendBriefRecords: encode brief: %w", err)
	}
	if err := appendRow(ledger.DraftRecord{
		Kind:    ledger.KindCommittal,
		Subject: subject,
		Payload: briefJSON,
	}); err != nil {
		return written, err
	}

	// 2. Commitment rows, one per action item. The FIRST one carries
	// SetStatus (owner them -> waiting_on_them, else open) with Summary =
	// the first item's text, phone-masked (display-path hygiene).
	for i, item := range brief.ActionItems {
		d := ledger.DraftRecord{
			Kind:    ledger.KindCommitment,
			Subject: subject,
			Payload: mustLedgerBriefJSON(item),
		}
		if i == 0 {
			d.SetStatus = ledger.StatusOpen
			if item.Owner == "them" {
				d.SetStatus = ledger.StatusWaitingThem
			}
			d.Summary = maskLedgerBriefPhones(item.Text)
		}
		if err := appendRow(d); err != nil {
			return written, err
		}
	}

	// 3. Follow-up: a non-empty follow_up_message is its own commitment
	// row with the frozen {"type":"follow_up_sms","text":...} payload.
	if brief.FollowUpMessage != "" {
		payload := mustLedgerBriefJSON(map[string]string{
			"type": "follow_up_sms",
			"text": brief.FollowUpMessage,
		})
		if err := appendRow(ledger.DraftRecord{
			Kind:    ledger.KindCommitment,
			Subject: subject,
			Payload: payload,
		}); err != nil {
			return written, err
		}
	}

	// 4. Risk flags: one observation row (log-only, no state implication).
	if len(brief.RiskFlags) > 0 {
		payload := mustLedgerBriefJSON(map[string][]string{"flags": brief.RiskFlags})
		if err := appendRow(ledger.DraftRecord{
			Kind:    ledger.KindObservation,
			Subject: subject,
			Payload: payload,
		}); err != nil {
			return written, err
		}
	}

	return written, nil
}

// mustLedgerBriefJSON marshals v, falling back to a JSON error object.
// The inputs are plain structs/maps of strings, so marshal cannot fail in
// practice; the fallback keeps the mapping total rather than panicking on
// a teardown-adjacent path.
func mustLedgerBriefJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"error":"ledger brief payload encode failed"}`)
	}
	return b
}
