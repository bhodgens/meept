package ledger

import "slices"

// AllowedTransitions is the legal state machine for subject status,
// enforced by Append (never by readers):
//
//	open            -> done | cancelled | waiting_on_me | waiting_on_them
//	waiting_on_me   -> open | done | cancelled
//	waiting_on_them -> open | done | cancelled
//	done            -> open          (reopen; store logs INFO "ledger reopen")
//	cancelled       -> open          (reopen; same log)
//	"" (new subject) -> any
var AllowedTransitions = map[Status][]Status{
	StatusOpen:        {StatusDone, StatusCancelled, StatusWaitingMe, StatusWaitingThem},
	StatusWaitingMe:   {StatusOpen, StatusDone, StatusCancelled},
	StatusWaitingThem: {StatusOpen, StatusDone, StatusCancelled},
	StatusDone:        {StatusOpen},
	StatusCancelled:   {StatusOpen},
}

// legalTransition reports whether moving a subject's status from `from` to
// `to` is allowed. A from of "" (new subject, no ledger_state row yet) is
// always legal per the frozen contract.
func legalTransition(from, to Status) bool {
	if from == "" {
		return true
	}
	return slices.Contains(AllowedTransitions[from], to)
}

// validStatus reports membership in the closed Status set. Append
// pre-validates SetStatus so rejections surface as typed 'ledger:'-prefixed
// errors instead of SQLite CHECK constraint failures.
func validStatus(st Status) bool {
	switch st {
	case StatusOpen, StatusDone, StatusCancelled, StatusWaitingMe, StatusWaitingThem:
		return true
	}
	return false
}
