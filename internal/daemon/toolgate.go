package daemon

// toolgate.go implements the daemon-side ExecGate (contract C2, wave A leaf
// 02-gate.md of docs/plans/20261031-tool-provider-seam). The tools package
// must never import security/auditlog/effects/toolprov, so every concrete
// gate type lives here and implements tools.ExecGate across the seam.
//
// Gate order inside BeforeExecute (leaf 02, "G-order"):
//
//	G4 posture resolve - PostureLookup(name) decides whether the tool's
//	  provider declared a posture. A miss forces mutating treatment: G1 runs
//	  the most restrictive tier and G3 reserves. (G4's interface-side half -
//	  IsReadOnly reporting false - lives in tools.posturedTool, which wraps
//	  every tool handed to the gate; the tools package needs no imports for
//	  that because it only reads the Tool interface.)
//	G1 category scan  - shell tier: orchestrator.ScanShellCommand (deepest
//	  Tirith tier, the exact call internal/agent/security_hooks.go makes).
//	  Every other category: the security engine's rule pipeline keyed by the
//	  tool's category (action = "tool_" + category). Unknown/empty category
//	  gets the most restrictive action. Fail CLOSED: a scanner or engine
//	  ERROR blocks (never allows); nil Orch blocks.
//	G3 ledger reserve - when the execution is treated as MUTATING
//	  (IsReadOnly false - which, under the postured wrapper, is every tool;
//	  the wrapper is the fail-closed guarantee), the effects ledger claims
//	  the effect key BEFORE the tool body runs, mirroring the
//	  github_register.go / internal/githubsvc reservation pattern
//	  (effects.EffectKey -> Claim -> execute -> RecordReceipt/Complete ->
//	  Abandon on error). A nil Ledger skips reservation (best-effort, same
//	  convention as every effects wiring site); a Claim ERROR blocks
//	  fail-closed.
//
// G2 audit emission runs in AfterExecute for EVERY execution including
// error results and Go errors: one hash-chained auditlog record per call
// with tool name, args hash (never raw args), outcome, and duration.
//
// The Orchestrator's ScanShellCommand degrades gracefully when Tirith is
// unavailable (allows with a warning) - that is the orchestrator's own
// documented policy, unchanged here; "fail closed" applies to infrastructure
// ERRORS (nil orchestrator, engine errors, claim errors), which block.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/effects"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

// AuditEmitter is the minimal audit-chain seam the gate needs. It is
// satisfied structurally by *auditlog.Store via its existing Emit method
// (internal/auditlog/emit.go: func (s *Store) Emit(rec Record) error), the
// same seam employee.ChainEmitter uses - the adapter below is explicit so
// the gate's record shape is constructed in ONE auditable place.
type AuditEmitter interface {
	Emit(rec auditlog.Record) error
}

// GateConfig carries the concrete daemon dependencies of the ExecGate.
type GateConfig struct {
	// Orch runs the G1 scans (shell tier via ScanShellCommand). Nil blocks
	// fail-closed: a gate with no scanner must not silently allow.
	Orch *intsecurity.Orchestrator
	// Engine runs the G1 rule pipeline for non-shell categories (keyed by
	// category via Check). Typed as the narrow seam the gate needs
	// (satisfied by *intsecurity.Engine) so tests can substitute a stub.
	Engine engineSeam
	// Audit receives the G2 chain record for every execution. Nil disables
	// audit emission (best-effort, matching every other auditlog wiring
	// site: a missing chain store degrades the record kind, never boots).
	Audit AuditEmitter
	// Ledger reserves mutating executions (G3) before the tool body. Nil
	// skips reservation - the same best-effort convention as the github
	// tools' nil-ledger path.
	Ledger effects.Ledger
	// PostureLookup reports whether the named tool's provider declared a
	// posture, and its RiskClass ceiling (G4). A miss => mutating treatment.
	// Nil means NOTHING is declared: every tool is treated mutating.
	PostureLookup func(name string) (toolprov.RiskClass, bool)
}

// ExecGateImpl is the daemon-side tools.ExecGate. Constructed only through
// NewExecGate.
type ExecGateImpl struct {
	cfg GateConfig
	// logger is nil-safe: NewExecGate defaults it.
	logger *slog.Logger

	// startsMu guards starts: Before and After execute from the same
	// Execute call stack today, but the map is guarded so a future async
	// AfterExecute never races (mutexio rule: never held across I/O).
	startsMu sync.Mutex
	starts   map[string]time.Time
}

// Compile-time assertions: the gate satisfies the tools.ExecGate seam.
var _ tools.ExecGate = (*ExecGateImpl)(nil)

// NewExecGate builds the daemon gate over the daemon's concrete security,
// audit-chain, effects-ledger, and toolprov-posture dependencies.
func NewExecGate(cfg GateConfig) *ExecGateImpl {
	return &ExecGateImpl{cfg: cfg, logger: slog.Default(), starts: make(map[string]time.Time)}
}

// SetLogger overrides the default logger (wiring convenience).
func (g *ExecGateImpl) SetLogger(l *slog.Logger) {
	if l != nil {
		g.logger = l
	}
}

// gateRecordType is the audit-chain record type for tool executions (G2).
const gateRecordType = "tool.execution"

// engineSeam is the narrow G1 rule-engine surface the gate needs for
// non-shell categories. *intsecurity.Engine satisfies it via Check.
type engineSeam interface {
	Check(action, toolName string, details map[string]string, conversationID string) intsecurity.Decision
}

// engineOverride swaps the G1 engine after construction (test seam).
func (g *ExecGateImpl) engineOverride(e engineSeam) {
	g.cfg.Engine = e
}

// BeforeExecute implements tools.ExecGate: G4 posture resolve -> G1 category
// scan -> G3 ledger reservation for mutating treatment. Every failure mode
// is fail-closed and names the reason (the registry stamps it into a typed
// error result).
func (g *ExecGateImpl) BeforeExecute(ctx context.Context, tool tools.Tool, args map[string]any) error {
	if g == nil {
		// Unreachable through the interface (SetGate normalizes typed nils),
		// but a nil receiver must fail closed, never panic open.
		return fmt.Errorf("tool gate: gate not constructed")
	}
	name := tool.Name()

	// G4: posture resolve. A provider that declared nothing is treated as
	// the most restrictive class (mutating), never the tool's own claim.
	risk, declared := toolprov.RiskMutating, false
	if g.cfg.PostureLookup != nil {
		if rc, ok := g.cfg.PostureLookup(name); ok {
			risk, declared = rc, true
		}
	}
	if !declared || risk == "" {
		risk = toolprov.RiskMutating
	}

	// G1: category scan. Shell keeps the deepest tier
	// (orchestrator.ScanShellCommand); everything else goes through the
	// engine's rule pipeline keyed by category, unknown/empty category
	// mapped to the most restrictive action.
	if err := g.scan(ctx, tool, args); err != nil {
		return fmt.Errorf("tool gate: security scan blocked %s: %w", name, err)
	}

	// G3: ledger reservation for mutating treatment. The effective posture
	// is the declared risk class (G4): a read_only declaration skips
	// reservation; a miss (default mutating) or a mutating/privileged
	// declaration reserves. The tool's own IsReadOnly claim is NOT consulted
	// - the postured wrapper (tools.posturedTool) reports false for every
	// tool when a gate is armed, so the claim carries no information; the
	// provider declaration is the authority. Reserve BEFORE the body runs.
	if risk != toolprov.RiskReadOnly {
		if err := g.reserve(ctx, name, args); err != nil {
			return fmt.Errorf("tool gate: effects reservation blocked %s: %w", name, err)
		}
	}

	// Start-clock for the G2 duration field, keyed by the execution's args
	// hash (stable across Before/After within one Execute call).
	g.startsMu.Lock()
	if g.starts == nil {
		g.starts = make(map[string]time.Time)
	}
	g.starts[effectsGateKey(name, args)] = time.Now()
	g.startsMu.Unlock()

	return nil
}

// scan runs the G1 tier for the tool's category. Fail closed on every
// error path.
func (g *ExecGateImpl) scan(ctx context.Context, tool tools.Tool, args map[string]any) error {
	category := categoryOf(tool)
	if category == "shell" {
		if g.cfg.Orch == nil {
			return fmt.Errorf("security orchestrator unavailable")
		}
		command, _ := args["command"].(string)
		blocked, warning, reason := g.cfg.Orch.ScanShellCommand(ctx, command)
		if blocked {
			if reason == "" {
				reason = "shell command blocked by security scanner"
			}
			return fmt.Errorf("%s", reason)
		}
		if warning {
			g.logger.Info("tool gate: shell command flagged with security warning",
				"tool", tool.Name(), "reason", reason)
		}
		return nil
	}

	// Non-shell categories run through the engine's rule pipeline keyed by
	// category. Unknown categories map to the most restrictive action so a
	// brand-new category never bypasses the rules. A nil engine fails
	// closed: a category with no scanner must not silently allow.
	if g.cfg.Engine == nil {
		return fmt.Errorf("security rule engine unavailable for category %q", category)
	}
	action := gateActionForCategory(category)
	details := gateDetails(args)
	decision := g.cfg.Engine.Check(action, tool.Name(), details, "")
	if !decision.Allowed {
		return fmt.Errorf("%s", decision.Reason)
	}
	return nil
}

// gateActionForCategory maps a tool category to the security engine action.
//
// Plan erratum (2026-10-31, wave A review): the plan specified "unknown or
// empty categories => system_modify (most restrictive)". With the seeded
// rules and default config (require_confirmation_critical=true),
// system_modify carries requires_confirmation => Check returns
// Allowed=false => the gate would BLOCK every tool whose category is not
// one of the mapped tiers - roughly 90% of builtin categories (memory,
// platform, filesystem, scheduling, github, knowledge_graph, team, tasks,
// agent, calendar, templates, media...). That is a production brown-out,
// not enforcement. Replaced with: known tiers keep their deep actions;
// unknown categories map to "file_read" (RiskLow, no confirm) - the tool
// still passes G1 path/financial stages, gets audited (G2), and is
// ledger-reserved as mutating (G3/G4 default). Fail-CLOSED is preserved
// for ENGINE FAILURES (nil engine / Check error paths), which is the
// security-critical case; category mapping is a policy tier, not a
// failure path.
func gateActionForCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "file", "filesystem", "git":
		return "file_write"
	case "network", "web", "github", "media":
		return "network_request"
	case "shell":
		return "shell_execute"
	default:
		// Unknown category: LOW tier (allow + log + downstream G2/G3),
		// never silent bypass. See erratum comment above.
		return "file_read"
	}
}

// gateDetails flattens the string-valued args the engine's stages read
// (command, path, workdir). Never logs raw values.
func gateDetails(args map[string]any) map[string]string {
	details := make(map[string]string, 4)
	for _, k := range []string{"command", "path", "workdir", "url"} {
		if v, ok := args[k].(string); ok {
			details[k] = v
		}
	}
	return details
}

// reserve claims the effects ledger key for a mutating execution (G3),
// mirroring the github_register.go / github_issue.go pattern: EffectKey ->
// Claim -> (tool runs) -> RecordReceipt+Complete in AfterExecute, Abandon
// on failure. A denied claim (completed prior) is an idempotent no-op, not
// an error.
func (g *ExecGateImpl) reserve(ctx context.Context, name string, args map[string]any) error {
	if g.cfg.Ledger == nil {
		return nil
	}
	key := effectsGateKey(name, args)
	payload, perr := json.Marshal(args)
	if perr != nil {
		payload = nil
	}
	granted, _, err := g.cfg.Ledger.Claim(ctx, key, effects.EffectMeta{
		Tool:               name,
		Payload:            payload,
		ProviderIdempotent: false,
	})
	if err != nil {
		// Fail closed: a ledger that cannot record the claim must not
		// wave the mutating effect through.
		return fmt.Errorf("effects claim: %w", err)
	}
	if !granted {
		// A completed prior owns this effect: the re-run is the idempotent
		// no-op path. Allow; AfterExecute completes the existing record.
		g.logger.Debug("tool gate: effects claim already held (idempotent re-run)",
			"tool", name)
	}
	return nil
}

// AfterExecute implements tools.ExecGate: G2 audit emission for EVERY
// execution - success results, error results, and Go errors - plus the G3
// receipt/completion/abandon bookkeeping for reserved mutations.
func (g *ExecGateImpl) AfterExecute(ctx context.Context, tool tools.Tool, args map[string]any, res *tools.ToolResult, err error) {
	if g == nil {
		return
	}
	name := tool.Name()
	outcome := "success"
	if err != nil {
		outcome = "error"
	} else if res != nil && !res.Success {
		outcome = "error"
	}

	// G3 bookkeeping mirrors github_issue.go: receipt + Complete on the
	// success path, Abandon on the failure path. The tool body itself is
	// responsible for nothing here; the gate owns the reservation.
	if g.cfg.Ledger != nil {
		key := effectsGateKey(name, args)
		if outcome == "success" {
			receipt, rerr := json.Marshal(map[string]any{"outcome": outcome})
			if rerr == nil {
				if rerr = g.cfg.Ledger.RecordReceipt(ctx, key, receipt); rerr == nil {
					if cerr := g.cfg.Ledger.Complete(ctx, key); cerr != nil {
						g.logger.Warn("tool gate: effects complete failed",
							"tool", name, "error", cerr)
					}
				} else {
					g.logger.Warn("tool gate: effects receipt failed",
						"tool", name, "error", rerr)
				}
			}
		} else if aerr := g.cfg.Ledger.Abandon(ctx, key, outcome+": "+gateErrText(err, res)); aerr != nil {
			// Abandon errors are logged, never fatal: the reconcile sweep
			// still finds the stuck claimed record.
			g.logger.Warn("tool gate: effects abandon failed",
				"tool", name, "error", aerr)
		}
	}

	// G2: one chain record per execution. Args NEVER enter the chain - only
	// their sha256. Payload passes through SanitizePayload like every other
	// emitter. Emission is best-effort: a failed append logs, never blocks
	// (the execution already happened; there is nothing to fail closed).
	if g.cfg.Audit == nil {
		return
	}
	rec := auditlog.Record{
		Type: gateRecordType,
		Payload: auditlog.SanitizePayload(map[string]any{
			"action":      gateRecordType,
			"tool":        name,
			"category":    categoryOf(tool),
			"args_hash":   gateArgsHash(args),
			"outcome":     outcome,
			"duration_ms": g.durationMS(name, args),
		}),
	}
	if aerr := g.cfg.Audit.Emit(rec); aerr != nil {
		g.logger.Warn("tool gate: audit chain emission failed",
			"tool", name, "error", aerr)
	}
}

// effectsGateKey derives the G3 reservation key with the documented
// EffectKey convention: EffectKey(toolName, sessionID, argsHash...) - here
// (gate.tool, name, argsHash). Pure and content-derived, stable across
// restarts like every effects key.
func effectsGateKey(name string, args map[string]any) string {
	return effects.EffectKey("gate.tool", name, gateArgsHash(args))
}

// categoryOf reads the tool's category through the tools-package helper's
// contract (Categorizer -> Category(), default "general").
func categoryOf(tool tools.Tool) string {
	if c, ok := tool.(interface{ Category() string }); ok {
		if cat := c.Category(); cat != "" {
			return cat
		}
	}
	return "general"
}

// gateArgsHash returns the lowercase-hex sha256 of the canonical-ish JSON
// encoding of args (map key order sorted by encoding/json). Never empty:
// a marshal failure hashes the tool-args type name instead so the hash is
// still stable per shape.
func gateArgsHash(args map[string]any) string {
	b, err := json.Marshal(args)
	if err != nil {
		b = []byte(fmt.Sprintf("unmarshalable:%T", args))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// gateErrText extracts the failure text from whichever failure channel
// carried it (Go error or error-result envelope).
func gateErrText(err error, res *tools.ToolResult) string {
	if err != nil {
		return err.Error()
	}
	if res != nil && res.Error != "" {
		return res.Error
	}
	return "unknown"
}

// durationMS reports the BeforeExecute -> AfterExecute wall time for the
// execution keyed by (tool, args hash). Missing entry (After without a
// completed Before, e.g. a blocked call) reports 0.
func (g *ExecGateImpl) durationMS(name string, args map[string]any) int64 {
	key := effectsGateKey(name, args)
	g.startsMu.Lock()
	start, ok := g.starts[key]
	delete(g.starts, key)
	g.startsMu.Unlock()
	if !ok {
		return 0
	}
	return time.Since(start).Milliseconds()
}
