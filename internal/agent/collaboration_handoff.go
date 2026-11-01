// collaboration_handoff.go implements the shared inter-agent handoff
// boundary seam used by all three collaboration drivers (plan:
// docs/plans/20261031-agent-boundary-hardening/01-inter-agent-taint/
// 02-driver-wiring.md; research decisions D1/D2/D4 in
// docs/research/agent-boundary-taint-findings.md).
//
// RAW AGENT OUTPUT is untrusted (D4: provenance is attached where the
// data enters). Before a driver embeds a peer's raw output into the next
// agent's prompt, it records that exact span as an untrusted segment on
// the taint tracker (keyed "collab:<driver>:<role>") and scans it with
// the inter-agent deny patterns. The driver's OWN framing text stays
// clean: it is never recorded and never scanned.
package agent

import (
	"log/slog"

	"github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/security/taint"
)

// collaborationHandoffMode mirrors [security.agent_handoff_mode]
// (R0 decision D3): off = labeling only, warn = scan + log, block =
// scan + deny. Zero value is warn (audit-first default).
type collaborationHandoffMode int

const (
	collabHandoffOff collaborationHandoffMode = iota
	collabHandoffWarn
	collabHandoffBlock
)

// parseCollabHandoffMode maps the config string to the mode enum.
// Empty and unrecognized values fall back to warn.
func parseCollabHandoffMode(s string) collaborationHandoffMode {
	switch s {
	case "off":
		return collabHandoffOff
	case "block":
		return collabHandoffBlock
	default:
		return collabHandoffWarn
	}
}

// handoffBoundary carries the per-driver collaboration seams: the shared
// taint tracker (nil when [security.taint] is disabled -> labeling and
// scanning are skipped silently), the deny-pattern boundary (nil when
// construction failed -> scanning skipped, never panics), and the
// configured handoff mode. Every field is nil-safe at use time (typed-nil
// discipline).
//
// Daemon code (internal/daemon) references the exported alias
// HandoffBoundary; the unexported name is the in-package spelling.
type handoffBoundary = HandoffBoundary

// HandoffBoundary is the exported driver seam type (leaf 02): carries the
// shared taint tracker and inter-agent deny boundary. Constructed only
// through NewHandoffBoundary.
type HandoffBoundary struct {
	tracker  *taint.ExtendedTracker
	boundary *security.InterAgentBoundary
	mode     collaborationHandoffMode
	logger   *slog.Logger
}

// NewHandoffBoundary builds the seam from config. All arguments are
// optional: a nil tracker disables segment recording, a nil boundary
// disables deny-pattern scanning.
func NewHandoffBoundary(tracker *taint.ExtendedTracker, boundary *security.InterAgentBoundary, mode string, logger *slog.Logger) *HandoffBoundary {
	if logger == nil {
		logger = slog.Default()
	}
	return &handoffBoundary{
		tracker:  tracker,
		boundary: boundary,
		mode:     parseCollabHandoffMode(mode),
		logger:   logger.With("component", "collab-handoff"),
	}
}

// PrepareHandoff is the exported form of prepareHandoff for daemon-side
// wiring tests; drivers call the in-package name.
func (h *HandoffBoundary) PrepareHandoff(driver, role, rawOutput string) (blocked bool, reason string) {
	return h.prepareHandoff(driver, role, rawOutput)
}

// prepareHandoff records rawOutput as an untrusted segment (unless the
// tracker or the value is empty) and, in warn/block mode, scans it with
// the inter-agent deny patterns. Returns blocked=true only in block mode
// on a deny-pattern hit; the caller must then fail the handoff with a
// CollaborationError instead of invoking the next agent.
//
// Nil seams degrade silently: nil tracker = no recording, nil boundary =
// no scanning, off mode = no scanning. Driver framing is never passed
// here, so it can never be recorded or blocked.
func (h *HandoffBoundary) prepareHandoff(driver, role, rawOutput string) (blocked bool, reason string) {
	if h == nil || rawOutput == "" {
		return false, ""
	}

	// Segment labeling (D1): free, never blocks on its own.
	if h.tracker != nil {
		h.tracker.RecordUntrustedSegment("collab:"+driver+":"+role, rawOutput)
	}

	if h.mode == collabHandoffOff || h.boundary == nil {
		return false, ""
	}

	matches := h.boundary.InspectHandoffContent(rawOutput)
	if len(matches) == 0 {
		return false, ""
	}

	if h.mode == collabHandoffWarn {
		for i := range matches {
			h.logger.Warn("inter-agent handoff deny-pattern hit (monitor: not blocked)",
				"driver", driver,
				"role", role,
				"class", matches[i].Class,
				"pattern", matches[i].Pattern,
			)
		}
		return false, ""
	}

	// block mode: the first hit denies the handoff.
	h.logger.Warn("inter-agent handoff blocked by deny pattern",
		"driver", driver,
		"role", role,
		"class", matches[0].Class,
		"pattern", matches[0].Pattern,
	)
	return true, "handoff blocked: " + matches[0].Class + " pattern in peer output from " + driver + " (" + role + ")"
}
