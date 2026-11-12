package daemon

// brief_commitments.go — leaf 04 (uptractor-ports): the structured call
// brief (C4) wiring + the commitments-to-ledger adapter (C7).
//
// Analyzer port: phone/core/internal/agent/callbrief.go (branch
// fix/coordinator-missed-hangup-watchdog) — prompt shape and the
// fence/brace extraction kept, Uptractor-domain fields (follow_up_sms,
// crm_note) DROPPED (master A2: meept's brief schema is GENERIC;
// employees supply domain context). The prompt carries no jobs /
// work-orders vocabulary.
//
// POSTURE: internal/voice must not import internal/ledger, so the
// ledger-append adapter lives HERE (daemon wiring) behind the
// voice.CommitmentFiler seam; voice only sees the interface.
//
// Lifecycle (all ADDITIVE observer lanes — the frozen releaseOnce funnel
// and the single-occupancy release/answer hooks are untouched):
//   - answered observer  -> WriteBriefStub (status in_progress) at answer
//   - terminal observer  -> post-release goroutine: O1 outcome/reason
//     mapping (reaper => abandoned), analyzer => completed/failed,
//     FinalizeBrief (idempotent), then FileCommitmentsFromBrief on
//     completed briefs only. Every failure is WARN-only and NEVER fails
//     or delays the release fan-out (notifyCallRelay contract mirrored).

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/voice"
)

// briefFinalizeTimeout bounds the whole post-release finalize goroutine
// (row reads + analyzer + finalizes + filing). It never blocks teardown:
// the terminal observer spawns it and returns immediately.
const briefFinalizeTimeout = 30 * time.Second

// briefAnalyzeTimeout bounds one analyzer LLM call (same posture as
// voice_brief.callBriefLLM: a hung client cannot hold the goroutine).
const briefAnalyzeTimeout = 10 * time.Second

// maxBriefTranscript bounds what is sent to the analyzer; the TAIL of a
// long call is kept because commitments cluster at the end (uptractor
// posture, kept).
const maxBriefTranscript = 24000

// llmBriefAnalyzer adapts the daemon's prompt-in/text-out LLM seam onto
// voice.BriefAnalyzer. A nil llm is allowed: Analyze reports an error and
// the finalize path keeps the stub with a warning (feature off by
// absence, no failure).
type llmBriefAnalyzer struct {
	llm func(prompt string) (string, error)
}

// briefAnalyzeSystemPrompt is the generic analyzer system prompt (ported
// from the uptractor callbrief prompt, domain fields removed).
const briefAnalyzeSystemPrompt = "You are a professional business assistant. Your job is to turn phone call transcripts into structured follow-up records. Do not invent facts."

// briefAnalyzeUserPrompt is the generic analysis instruction + the JSON
// template (summary / action_items / commitments / risk_flags only).
const briefAnalyzeUserPrompt = `Analyze this phone call transcript:
1. Summarize the call.
2. Identify action items.
3. Identify commitments made by either party.
4. Flag any sensitive risks or client issues.
You MUST return the output STRICTLY in a JSON object using the following template:
{
  "summary": "High level brief of the call...",
  "action_items": ["Action item 1", "Action item 2"],
  "commitments": ["Commitment 1", "Commitment 2"],
  "risk_flags": ["Sensitive flags or blank array"]
}

Transcript:
`

// Analyze runs the analyzer over the transcript. The transcript is
// rendered "caller: ..." / "employee: ..." lines; the TAIL is kept when
// it exceeds maxBriefTranscript. Fenced or prose-wrapped JSON is
// tolerated (extract first '{' to last '}'); anything without a parsable
// object or a non-empty summary is an error.
func (a llmBriefAnalyzer) Analyze(ctx context.Context, transcript, ccid string) (*voice.BriefBody, error) {
	if a.llm == nil {
		return nil, fmt.Errorf("voice callbrief: no analyzer wired (call %s)", ccid)
	}
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return nil, fmt.Errorf("voice callbrief: empty transcript (call %s)", ccid)
	}
	sent := transcript
	if len(sent) > maxBriefTranscript {
		sent = sent[len(sent)-maxBriefTranscript:]
	}
	prompt := briefAnalyzeSystemPrompt + "\n\n" + briefAnalyzeUserPrompt + sent

	// Timeout guard: the seam call runs on a goroutine with a buffered
	// channel; a late return after ctx expiry is dropped (same pattern
	// as voice_brief.callBriefLLM).
	tctx, cancel := context.WithTimeout(ctx, briefAnalyzeTimeout)
	defer cancel()
	type outcome struct {
		text string
		err  error
	}
	ch := make(chan outcome, 1)
	go func() {
		text, err := a.llm(prompt)
		ch <- outcome{text: text, err: err}
	}()
	var resp string
	select {
	case <-tctx.Done():
		return nil, fmt.Errorf("voice callbrief: analyzer timed out (call %s): %w", ccid, tctx.Err())
	case o := <-ch:
		if o.err != nil {
			return nil, fmt.Errorf("voice callbrief: analyzer (call %s): %w", ccid, o.err)
		}
		resp = o.text
	}
	return parseBriefBody(resp, ccid)
}

// parseBriefBody decodes model output into a BriefBody: strip to the
// outermost {...} object, unmarshal, require a non-empty summary, nil
// lists become empty so stored briefs always carry arrays.
func parseBriefBody(raw, ccid string) (*voice.BriefBody, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("voice callbrief: model output has no JSON object (call %s)", ccid)
	}
	var body voice.BriefBody
	if err := json.Unmarshal([]byte(raw[start:end+1]), &body); err != nil {
		return nil, fmt.Errorf("voice callbrief: decode (call %s): %w", ccid, err)
	}
	body.Summary = strings.TrimSpace(body.Summary)
	if body.Summary == "" {
		return nil, fmt.Errorf("voice callbrief: model output has no summary (call %s)", ccid)
	}
	if body.ActionItems == nil {
		body.ActionItems = []string{}
	}
	if body.Commitments == nil {
		body.Commitments = []string{}
	}
	if body.RiskFlags == nil {
		body.RiskFlags = []string{}
	}
	return &body, nil
}

// renderBriefTranscript renders the stored transcript lines the way the
// analyzer prompt expects ("caller: ..." / "employee: ...").
func renderBriefTranscript(lines []voice.TranscriptLine) string {
	var sb strings.Builder
	for _, l := range lines {
		role := l.Role
		switch role {
		case "user":
			role = "caller"
		case "":
			role = "caller"
		default:
			role = "employee"
		}
		fmt.Fprintf(&sb, "%s: %s\n", role, l.Text)
	}
	return sb.String()
}

// ledgerCommitmentFiler adapts the employee ledger store onto
// voice.CommitmentFiler with the frozen C7 mapping: one
// ledger.Append(kind=commitment, source=voice, source_ref=ccid) per
// commitment, subject "call:"+ccid (the 03-voice subject posture),
// payload {"idx":i,"text":...} carrying the commitment index the
// exactly-once dedupe keys on.
type ledgerCommitmentFiler struct {
	store      *ledger.Store
	employeeID string
}

// commitmentPayload is the index-carrying payload shape (dedupe key).
type commitmentPayload struct {
	Idx  int    `json:"idx"`
	Text string `json:"text"`
}

// FileCommitment implements voice.CommitmentFiler.
func (f *ledgerCommitmentFiler) FileCommitment(ctx context.Context, row voice.CommitmentRow) error {
	if f == nil || f.store == nil {
		return fmt.Errorf("voice commitments: no ledger store wired")
	}
	payload, err := json.Marshal(commitmentPayload{Idx: row.Index, Text: row.Subject})
	if err != nil {
		return fmt.Errorf("voice commitments: encode payload: %w", err)
	}
	_, err = f.store.Append(ctx, ledger.DraftRecord{
		EmployeeID: f.employeeID,
		Kind:       ledger.KindCommitment,
		Subject:    "call:" + row.SourceRef,
		Payload:    payload,
		Source:     ledger.SourceVoice,
		SourceRef:  row.SourceRef,
	})
	if err != nil {
		return fmt.Errorf("voice commitments: ledger append: %w", err)
	}
	return nil
}

// HasCommitment implements the dedupe query: a commitment row already
// filed for (ccid, index). Rows are scoped by employee (QueryLog
// requirement), filtered to kind=commitment, then matched on
// source_ref=ccid + payload idx.
func (f *ledgerCommitmentFiler) HasCommitment(ctx context.Context, ccid string, index int) (bool, error) {
	if f == nil || f.store == nil {
		return false, fmt.Errorf("voice commitments: no ledger store wired")
	}
	rows, err := f.store.QueryLog(ctx, ledger.LogQuery{
		EmployeeID: f.employeeID,
		Kinds:      []ledger.Kind{ledger.KindCommitment},
		Limit:      1000,
	})
	if err != nil {
		return false, fmt.Errorf("voice commitments: query: %w", err)
	}
	for _, rec := range rows {
		if rec.SourceRef != ccid {
			continue
		}
		var p commitmentPayload
		if json.Unmarshal(rec.Payload, &p) != nil {
			continue
		}
		if p.Idx == index {
			return true, nil
		}
	}
	return false, nil
}

// wireBriefObservers registers the leaf-04 additive observers on the
// manager's answered + terminal lanes. Everything is fail-soft: a nil
// employee/store just skips registration; runtime failures WARN and never
// fail or delay the release fan-out. Nil analyzer => finalize keeps the
// stub and logs a warning (feature off by absence).
func wireBriefObservers(w *VoiceWiring, employee *VoiceEmployeeWiring, manager *voice.Manager, logger *slog.Logger) {
	if w == nil || employee == nil || employee.store == nil || manager == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	analyzer := llmBriefAnalyzer{llm: employee.llm}

	// Answered lane: write the in_progress stub (C4 stub-at-answer).
	manager.AddAnsweredObserver(func(ccid string) {
		writeBriefStubFor(w, employee, ccid, logger)
	})

	// Terminal lane: finalize post-release, OFF the hot path (the
	// observer returns immediately; the goroutine owns the 30s budget).
	manager.AddTerminalObserver(func(ccid string, outcome voice.Outcome, reason string) {
		go finalizeBriefFor(w, employee, analyzer, ccid, outcome, reason, logger)
	})
}

// writeBriefStubFor writes the C4 stub for an answered call. The call row
// (already inserted by the binding's OnCallStart) supplies the remote
// number + direction; our own number comes from the voice config.
func writeBriefStubFor(w *VoiceWiring, employee *VoiceEmployeeWiring, ccid string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rec, _, ok, err := employee.store.EndedCall(ctx, ccid)
	if err != nil {
		logger.Warn("voice callbrief: stub row read failed", "ccid", ccid, "error", err)
		return
	}
	if !ok {
		// No row (not bound yet, or an untracked id): the stub would have
		// nowhere to live; finalize tolerates its absence.
		logger.Info("voice callbrief: no call row for stub, skipping", "ccid", ccid)
		return
	}
	from, to := briefParties(w, rec)
	if err := employee.store.WriteBriefStub(ctx, ccid, from, to); err != nil {
		logger.Warn("voice callbrief: stub write failed", "ccid", ccid, "error", err)
	}
}

// briefParties resolves the C4 from/to envelope fields from the call row
// + config: incoming calls have from=remote number; outgoing calls are
// reversed. An empty configured from-number leaves the field empty.
func briefParties(w *VoiceWiring, rec voice.CallRecord) (from, to string) {
	own := ""
	if w != nil {
		own = w.cfg.FromNumber
	}
	if rec.Direction == voice.DirectionOutgoing {
		return own, rec.Number
	}
	return rec.Number, own
}

// finalizeBriefFor runs the post-release finalize: O1 outcome/reason
// mapping, analyzer, idempotent FinalizeBrief, then commitment filing
// (completed briefs only). Every failure WARN-only; the release that
// spawned this goroutine never depends on it.
func finalizeBriefFor(w *VoiceWiring, employee *VoiceEmployeeWiring, analyzer voice.BriefAnalyzer, ccid string, outcome voice.Outcome, reason string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), briefFinalizeTimeout)
	defer cancel()

	rec, lines, ok, err := employee.store.EndedCall(ctx, ccid)
	if err != nil {
		logger.Warn("voice callbrief: finalize row read failed", "ccid", ccid, "error", err)
		return
	}
	if !ok {
		logger.Info("voice callbrief: no call row at finalize, skipping", "ccid", ccid)
		return
	}
	from, to := briefParties(w, rec)

	// O1 mapping: reaped calls (hangup lost / max duration) => brief
	// abandoned; any other non-hangup terminal => failed. Both finalize
	// straight through the idempotent store path (never retried).
	if strings.HasPrefix(reason, "reaped:") {
		finalizeWithStatus(ctx, employee, ccid, voice.BriefStatusAbandoned, reason, from, to, logger)
		return
	}
	if outcome != voice.OutcomeHangup {
		finalizeWithStatus(ctx, employee, ccid, voice.BriefStatusFailed, string(outcome)+": "+reason, from, to, logger)
		return
	}

	// Normal hangup: run the analyzer. A nil-wired analyzer leaves the
	// stub in place with a warning (T2 posture), never a failure.
	body, err := analyzer.Analyze(ctx, renderBriefTranscript(lines), ccid)
	if err != nil {
		logger.Warn("voice callbrief: analysis failed, storing failed brief (never retried)", "ccid", ccid, "error", err)
		finalizeWithStatus(ctx, employee, ccid, voice.BriefStatusFailed, err.Error(), from, to, logger)
		return
	}
	b := &voice.CallBrief{
		Status: voice.BriefStatusCompleted,
		From:   from,
		To:     to,
		Brief:  *body,
	}
	if err := employee.store.FinalizeBrief(ctx, ccid, b); err != nil {
		logger.Warn("voice callbrief: finalize failed", "ccid", ccid, "error", err)
		return
	}
	logger.Info("voice callbrief: brief finalized", "ccid", ccid,
		"employee_id", rec.EmployeeID, "status", b.Status,
		"commitments", len(b.Brief.Commitments))

	// C7 filing: completed briefs only, exactly-once per (ccid, index).
	if rec.EmployeeID == "" {
		return
	}
	filer := &ledgerCommitmentFiler{store: employee.ledgerStore, employeeID: rec.EmployeeID}
	if err := voice.FileCommitmentsFromBrief(ctx, filer, ccid, b); err != nil {
		logger.Warn("voice callbrief: commitment filing failed", "ccid", ccid, "error", err)
	}
}

// finalizeWithStatus stores a terminal-status brief without a body
// (abandoned / failed paths). The from/to envelope is preserved from the
// stored stub when one exists (the stub captured the answer-time
// parties); a missing stub falls back to the row-derived parties.
func finalizeWithStatus(ctx context.Context, employee *VoiceEmployeeWiring, ccid, status, errText, from, to string, logger *slog.Logger) {
	if cur, err := employee.store.Brief(ctx, ccid); err == nil && cur != nil && cur.From != "" {
		from, to = cur.From, cur.To
	}
	b := &voice.CallBrief{
		Status: status,
		Error:  errText,
		From:   from,
		To:     to,
		Brief:  voice.BriefBody{ActionItems: []string{}, Commitments: []string{}, RiskFlags: []string{}},
	}
	if err := employee.store.FinalizeBrief(ctx, ccid, b); err != nil {
		logger.Warn("voice callbrief: finalize failed", "ccid", ccid, "status", status, "error", err)
		return
	}
	logger.Info("voice callbrief: brief finalized", "ccid", ccid, "status", status)
}
