package acp

import (
	"context"
	"strconv"
)

// Approver identifies WHO answered a human permission relay request. It is
// carried on the resolve path (RPC handler → Relay → session wait →
// notifier → bus event) so every approval is attributable, and it is the
// enforcement input for the self-approval gate (audit M5): a resolve whose
// approver is a daemon-spawned process is the agent approving itself.
//
// Kind values:
//   - "peercred": kernel-verified Unix-socket peer (PID/UID populated).
//   - "http-key": HTTP API caller; Detail is a short fingerprint (sha256
//     prefix) of the API key, never the key itself.
//   - "slack": Slack approval-channel user (hitl-notifications leaf 01);
//     Detail is the Slack user ID, verified by the channel's request
//     signature + allowed_user_ids allowlist before the resolve (the
//     internal/acp side never imports the slack package - the kind is a
//     plain string).
//   - "in-process": daemon-internal caller (no socket peer, no HTTP key).
//   - "test": test-injected approver.
type Approver struct {
	Kind   string `json:"kind"`
	PID    int    `json:"pid,omitempty"`
	UID    int    `json:"uid,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Approver Kind constants. String() renders "kind:detail" for kinds that
// carry a Detail identity.
const (
	// ApproverKindPeercred is the kernel-verified Unix-socket peer kind.
	ApproverKindPeercred = "peercred"
	// ApproverKindHTTPKey is the authenticated HTTP API-key caller kind.
	ApproverKindHTTPKey = "http-key"
	// ApproverKindInProcess is the daemon-internal caller kind.
	ApproverKindInProcess = "in-process"
	// ApproverKindTest is the test-injected approver kind.
	ApproverKindTest = "test"
	// ApproverKindSlack is the Slack approval-channel user kind (Detail =
	// Slack user ID). Set by internal/comm/slack after signature +
	// allowlist verification; this package holds no slack dependency.
	ApproverKindSlack = "slack"
)

// String renders the approver for logs and the bus event payload. It never
// includes raw credential material — Detail is already a fingerprint.
func (a Approver) String() string {
	switch a.Kind {
	case "peercred":
		return "peercred:pid=" + strconv.Itoa(a.PID) + ",uid=" + strconv.Itoa(a.UID)
	case "slack":
		// slack:<uid> per hitl-notifications leaf 01 Contract C4.
		if a.Detail != "" {
			return "slack:" + a.Detail
		}
		return "slack"
	case "":
		return "unknown"
	default:
		if a.Detail != "" {
			return a.Kind + ":" + a.Detail
		}
		return a.Kind
	}
}

// approverContextKey is the context key carrying the resolve-path approver.
type approverContextKey struct{}

// WithApprover returns a context carrying a. The RPC dispatch path
// (internal/rpc) attaches the kernel-verified socket peer; the HTTP
// boundary (internal/comm/http) attaches the key fingerprint before the
// in-process dispatch. When both are present the socket peer wins — the
// kernel-verified identity is the stronger claim.
func WithApprover(ctx context.Context, a Approver) context.Context {
	return context.WithValue(ctx, approverContextKey{}, a)
}

// ApproverFromContext extracts the approver attached by WithApprover.
// ok is false when the context carries none.
func ApproverFromContext(ctx context.Context) (Approver, bool) {
	if ctx == nil {
		return Approver{}, false
	}
	a, ok := ctx.Value(approverContextKey{}).(Approver)
	return a, ok
}
