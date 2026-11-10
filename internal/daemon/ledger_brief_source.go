package daemon

// ledger_brief_source.go — branch 03-voice impl leaf 03-voice/03: the
// daemon-side OpenCommitmentsSource over the employee ledger. internal/voice
// owns the seam types (dependency direction: daemon implements voice's
// interface; voice never imports internal/ledger).

import (
	"context"
	"regexp"
	"time"

	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/voice"
)

// ledgerBriefSource implements voice.OpenCommitmentsSource over
// ledger.Store.QueryOpen (frozen newest-updated-first order). A nil store
// must never be wrapped (typed-nil trap): wiring goes through
// ledgerCommitmentsSource below.
type ledgerBriefSource struct {
	store *ledger.Store
}

var _ voice.OpenCommitmentsSource = (*ledgerBriefSource)(nil)

// ledgerBriefDigitRun is the belt-and-braces display scrub for commitment
// summaries: 03-02 masks phone-shaped runs at WRITE time, but rows already
// in the store predate that — the day-brief path must still never surface
// a "+digits" run in the LLM prompt.
var ledgerBriefDigitRun = regexp.MustCompile(`\+\d+`)

// OpenCommitments maps the employee's open ledger states onto brief rows.
// LastAt is the state's last-write time, RFC3339 (the house TEXT format).
func (s *ledgerBriefSource) OpenCommitments(ctx context.Context, employeeID string) ([]voice.CommitmentBrief, error) {
	states, err := s.store.QueryOpen(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	out := make([]voice.CommitmentBrief, 0, len(states))
	for _, st := range states {
		out = append(out, voice.CommitmentBrief{
			Subject: st.Subject,
			Summary: ledgerBriefDigitRun.ReplaceAllString(st.Summary, "(number on file)"),
			Status:  string(st.Status),
			LastAt:  st.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// ledgerCommitmentsSource adapts the (possibly nil) ledger store onto the
// day-brief seam: nil store -> nil interface -> the seam section is simply
// absent and the PromptBlock keeps the pre-edit transcript rendering.
func ledgerCommitmentsSource(store *ledger.Store) voice.OpenCommitmentsSource {
	if store == nil {
		return nil
	}
	return &ledgerBriefSource{store: store}
}
