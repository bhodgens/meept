package rpc

// groups_diagnosis.go — the groups.check / explain-attribution RPC surface
// (agent-groups leaf 06-diagnosis, docs/plans/20261030-agent-groups/06-diagnosis.md).
//
// The diagnosis ENGINE lives in internal/groups (groups.Explain +
// groups.Check — report-only, table-tested, shared by every surface);
// the assembly service is internal/services.GroupsService. internal/rpc
// can import neither (the pre-existing services -> scheduler -> rpc
// cycle), so the dependency is the local DiagnosisService interface —
// satisfied structurally by *services.GroupsService and wired at daemon
// registration (internal/daemon/daemon.go is the only place that sees
// both concrete types).
//
// Read-only: groups.check mutates nothing (doctor norm — report-only).

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/groups"
)

// DiagnosisService is the RPC-side view of the report-only diagnosis
// service. *services.GroupsService satisfies this structurally (the
// compile-time pin lives in the daemon wiring file that assigns it).
type DiagnosisService interface {
	// Explain resolves one agent with per-entry source attribution
	// (which binding + policy produced each grant/deny).
	Explain(ctx context.Context, agentID string) (*groups.ExplainResult, error)
	// Check enumerates every R5 defect class with names. DefectCount>0
	// means defects found; the CLI maps that to exit code 1.
	Check(ctx context.Context, knownTools map[string]struct{}, overrides groups.OverridesReader) (*groups.CheckResult, error)
}

// SetDiagnosisService wires the diagnosis surface (leaf 06). Nil-guarded
// per the setter convention; nil keeps the legacy resolution-only explain
// and makes groups.check answer a descriptive error.
func (h *GroupsHandler) SetDiagnosisService(svc DiagnosisService) {
	if svc != nil {
		h.checkSvc = svc
	}
}

// SetOverridesLoader wires the leaf-07 overrides reader seam (leaf
// 07-overrides): groups.check loads the overrides file through this at
// request time, so active + expired override counts (and the named
// expired_override findings) reflect the file an operator last wrote.
// loader may return (nil, nil) when the file is absent (zero overrides).
// Nil-guarded per the setter convention; nil keeps the check-time
// "reader not wired" note.
func (h *GroupsHandler) SetOverridesLoader(loader func() (groups.OverridesReader, error)) {
	if loader != nil {
		h.overridesLoader = loader
	}
}

// overridesReader answers handleCheck's reader parameter: the freshly
// loaded set, or nil when no loader is wired (the check then reports the
// reader-not-wired note instead of counts).
func (h *GroupsHandler) overridesReader() (groups.OverridesReader, error) {
	if h.overridesLoader == nil {
		return nil, nil
	}
	return h.overridesLoader()
}

// handleCheck answers groups.check: the full defect enumeration (R5).
// Read-only — no gate beyond transport auth (same posture as
// groups.list/explain). Without a wired diagnosis service the method
// degrades to a descriptive error (nil-safe handler convention).
func (h *GroupsHandler) handleCheck(ctx context.Context, params json.RawMessage) (any, error) {
	if h.checkSvc == nil {
		return nil, fmt.Errorf("groups.check: diagnosis service not wired (SetDiagnosisService)")
	}
	// Optional params: {} | {"known_tools": ["shell", ...]} — the known
	// tool set lets a caller inject the live registry view; absent means
	// the C6 tool-name leg is inert (structural classes still run).
	var req struct {
		KnownTools []string `json:"known_tools"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("groups.check: %w", err)
		}
	}
	known := make(map[string]struct{}, len(req.KnownTools))
	for _, t := range req.KnownTools {
		if t != "" {
			known[t] = struct{}{}
		}
	}
	overrides, overridesErr := h.overridesReader()
	if overridesErr != nil {
		return nil, fmt.Errorf("groups.check: load overrides: %w", overridesErr)
	}
	res, err := h.checkSvc.Check(ctx, known, overrides)
	if err != nil {
		return nil, fmt.Errorf("groups.check: %w", err)
	}
	return map[string]any{
		"ok":             res.Ok,
		"defect_count":   res.DefectCount,
		"defects":        res.Defects,
		"agents_checked": res.AgentsChecked,
		"overrides":      res.Overrides,
	}, nil
}
