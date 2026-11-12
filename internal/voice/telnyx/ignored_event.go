package telnyx

// Ported from uptractor phone/core/internal/telnyx/callstatus.go (branch
// fix/coordinator-missed-hangup-watchdog). Contract C2.

import (
	"errors"
	"fmt"
)

// IgnoredEventError is returned by ParseCallEvent for a Telnyx "call.*"
// event that is not part of the lifecycle this service drives, such as
// call.conversation.created or call.analyzed. It is not a fault: the event
// is real Telnyx traffic we have no use for. Callers acknowledge it (so
// Telnyx does not retry) and log it at debug, not error. Event types
// outside "call." stay plain errors so a misrouted webhook is still
// refused.
type IgnoredEventError struct{ EventType string }

func (e *IgnoredEventError) Error() string {
	return fmt.Sprintf("telnyx call event: event %q is not a call lifecycle event", e.EventType)
}

// IsIgnoredEvent reports whether err is an IgnoredEventError.
func IsIgnoredEvent(err error) bool {
	var ie *IgnoredEventError
	return errors.As(err, &ie)
}
