package daemon

// voice_brief.go — branch 03-voice impl leaf 03-voice/01: the per-call
// brief builder (briefForCall), the frozen refusal rule (usableCheck),
// and the ReleaseHook brief runner.
//
// The brief JSON contract (frozen, mirrors uptractor turn.ts:848-901) is
// owned by leaf 03-02 (briefResult in ledger_brief_records.go); this file
// only produces it. The LLM seam is the plain func form
// func(prompt string) (string, error) — production adapters are built by
// ledger_wiring.go over the daemon's EXISTING llm client (no new HTTP
// client; the leaf pins that).
//
// Frozen refusal rule (port of the uptractor heardEnough rule): usable
// speech = >= 3 non-filler words across the transcript. Filler list
// (case-insensitive): um, uh, okay, yeah, hmm, like. Unusable or empty
// transcript -> NO LLM call, no brief, INFO log only.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/voice"
)

// errBriefUnavailable is the give-up sentinel: after the initial call plus
// ONE parse-failure retry the brief is unavailable (nil, errBriefUnavailable).
var errBriefUnavailable = errors.New("voice brief: LLM did not return a parseable brief")

// voiceBriefMaxTokens / voiceBriefTemperature / voiceBriefTimeout pin the
// frozen brief call posture (max_tokens 500, temp 0.2, no tools, 10s).
// The first two apply to the production adapter (ledger_wiring.go); the
// seam function here cannot see them, but the timeout is enforced around
// every seam call below.
const (
	voiceBriefMaxTokens = 500
	voiceBriefTemp      = 0.2
	voiceBriefTimeout   = 10 * time.Second
)

// voiceBriefFillers is the frozen filler word set (case-insensitive).
var voiceBriefFillers = map[string]bool{
	"um": true, "uh": true, "okay": true, "yeah": true, "hmm": true, "like": true,
}

// usableCheck implements the frozen refusal rule: at least 3 non-filler
// words across all transcript lines. Punctuation is trimmed off word
// edges before the filler comparison ("um," is still a filler).
func usableCheck(lines []voice.TranscriptLine) bool {
	count := 0
	for _, l := range lines {
		for word := range strings.FieldsSeq(strings.ToLower(l.Text)) {
			trimmed := strings.Trim(word, ".,!?;:'\"()[]{}-—…")
			if trimmed == "" {
				continue
			}
			if voiceBriefFillers[trimmed] {
				continue
			}
			count++
			if count >= 3 {
				return true
			}
		}
	}
	return false
}

// buildVoiceBriefPrompt renders the frozen system prompt (the exact lines
// pinned by 00T §1) plus the transcript. Kept as a pure func so the
// prompt posture is testable through the recorded seam either way.
func buildVoiceBriefPrompt(lines []voice.TranscriptLine) string {
	var sb strings.Builder
	sb.WriteString("You turn one employee phone call into a ledger brief.\n")
	sb.WriteString("Return only JSON with keys summary, action_items, commitments, follow_up_message, risk_flags.\n")
	sb.WriteString("Each action_item has text, owner (me or them), and due (may be empty).\n")
	sb.WriteString("Do not invent facts.\n")
	sb.WriteString("action_items are only things still undone. commitments are only promises actually made.\n")
	sb.WriteString("follow_up_message is a draft text in the caller's voice of the job, specific to what changed. Empty string if there is nothing to send. Never a generic 'let me know'.\n")
	sb.WriteString("risk_flags is an array, empty if none.\n")
	sb.WriteString("\nTranscript:\n")
	for _, l := range lines {
		role := l.Role
		if role == "" {
			role = "user"
		}
		fmt.Fprintf(&sb, "%s: %s\n", role, l.Text)
	}
	return sb.String()
}

// extractBriefJSON applies the frozen extraction rule: first '{' to last
// '}'. Returns ok=false when the response carries no JSON object span.
func extractBriefJSON(resp string) (string, bool) {
	start := strings.IndexByte(resp, '{')
	if start < 0 {
		return "", false
	}
	end := strings.LastIndexByte(resp, '}')
	if end <= start {
		return "", false
	}
	return resp[start : end+1], true
}

// parseBriefResult unmarshals the frozen brief JSON contract.
func parseBriefResult(raw string) (*briefResult, error) {
	var br briefResult
	if err := json.Unmarshal([]byte(raw), &br); err != nil {
		return nil, err
	}
	return &br, nil
}

// briefForCall runs the frozen brief builder against the LLM seam:
// one call; on parse failure exactly ONE retry; then give-up with
// (nil, errBriefUnavailable). The llm parameter MUST accept a plain
// func(prompt string) (string, error) value (00T seam pin). Each seam
// call is bounded by voiceBriefTimeout and by the caller's ctx.
func briefForCall(ctx context.Context, llm func(prompt string) (string, error), lines []voice.TranscriptLine) (*briefResult, error) {
	if llm == nil {
		return nil, errBriefUnavailable
	}
	prompt := buildVoiceBriefPrompt(lines)

	for range 2 {
		resp, err := callBriefLLM(ctx, llm, prompt)
		if err != nil {
			// Transport/timeout error on the first attempt gets the same
			// one-retry budget as a parse failure.
			continue
		}
		if raw, ok := extractBriefJSON(resp); ok {
			if br, perr := parseBriefResult(raw); perr == nil {
				return br, nil
			}
		}
	}
	return nil, errBriefUnavailable
}

// callBriefLLM invokes the seam under a timeout guard so a hung LLM
// client cannot block past voiceBriefTimeout (or the caller's ctx,
// whichever is shorter). The seam call itself runs on a goroutine with a
// buffered channel, so a late return after ctx expiry is simply dropped.
func callBriefLLM(ctx context.Context, llm func(string) (string, error), prompt string) (string, error) {
	tctx, cancel := context.WithTimeout(ctx, voiceBriefTimeout)
	defer cancel()

	type outcome struct {
		text string
		err  error
	}
	ch := make(chan outcome, 1)
	go func() {
		text, err := llm(prompt)
		ch <- outcome{text: text, err: err}
	}()
	select {
	case <-tctx.Done():
		return "", tctx.Err()
	case o := <-ch:
		return o.text, o.err
	}
}

// runCallBrief is the ReleaseHook best-effort brief path (contract order:
// store.EndedCall -> usableCheck -> briefForCall -> appendBriefRecordsFn).
// Every failure is WARN/INFO only and NEVER fails or delays the teardown
// it rides on — the notifyCallRelay "must never delay or fail the
// teardown" contract mirrored. Feature-off posture: nil ledgerStore (or
// nil llm) -> complete no-op, zero LLM calls (off by absence, no config
// knob in v1).
func (w *VoiceEmployeeWiring) runCallBrief(ctx context.Context, ccid string) {
	if w.ledgerStore == nil || w.llm == nil {
		return
	}

	callRec, lines, ok, err := w.store.EndedCall(ctx, ccid)
	if err != nil {
		w.logger.Warn("voice brief: ended-call read failed", "ccid", ccid, "error", err)
		return
	}
	if !ok {
		w.logger.Info("voice brief: no ended call row, skipping brief", "ccid", ccid)
		return
	}
	if len(lines) == 0 || !usableCheck(lines) {
		// Frozen refusal rule: unusable or empty transcript -> NO LLM
		// call, no brief, INFO log.
		w.logger.Info("voice brief: transcript not usable, skipping brief",
			"ccid", ccid, "employee_id", callRec.EmployeeID, "lines", len(lines))
		return
	}

	brief, err := briefForCall(ctx, w.llm, lines)
	if err != nil {
		w.logger.Warn("voice brief: brief unavailable, writing nothing", "ccid", ccid, "error", err)
		return
	}

	n, err := appendBriefRecordsFn(ctx, w.ledgerStore, callRec.EmployeeID, ccid, brief)
	if err != nil {
		w.logger.Warn("voice brief: ledger append failed", "ccid", ccid,
			"employee_id", callRec.EmployeeID, "appended", n, "error", err)
		return
	}
	w.logger.Info("voice brief: call brief appended", "ccid", ccid,
		"employee_id", callRec.EmployeeID, "rows", n)
}
