package daemon

// Signal leg wiring (contact-channels plan, leaf 11a T6): constructs the
// signalleg.Leg when [signal].enabled=true, arms its call-event state
// machine, and adapts the phone-posture seams (effects reservation,
// audit row, allowlist) onto the daemon components. Nil-safe degrade
// posture mirrors holdPhoneCall: a disabled signal config leaves the
// Leg nil, the daemon boots clean, and every call path stays refused.
// A construction failure is a loud WARN (not fatal) - the rest of the
// voice stack keeps running, mirroring the wireSlackChannel
// optional-channel shape.
//
// The [signal] sidecar itself (signal.Supervisor, leaf 10) is started
// by the caller that owns the sidecar lifecycle; this file only binds
// the VOICE leg onto the existing client the supervisor hands out. A
// supervisor that has not armed (no client yet) degrades the leg to
// nil with a logged warning - the leg never dials through a half-open
// transport.
//
// Signal-leg parity seams (audit 2026-11-09 voice, plan 20261109-
// statemachine-remediation leaves 02 + 03; leaf 02 deferred this note
// here because internal/voice/AGENTS.md is a sibling-scope file): this
// wiring is where the signal leg inherits the phone/local contract —
// FreshSession resolves voice.fresh_session_per_call + the employee_leg
// override per call (finding 5; the leg's nil-seam posture is one
// session per call, so an unwired leg must not silently share),
// DayBriefBuilder feeds the same daemon-built brief the phone/local legs
// carry, the effect key is (employee, target, requestID) via Place's
// requestIDs variadic so a retried request reuses the ledger receipt
// (finding 6 / C-V2), and RingingTimeout passes [voice].timeout_secs —
// the phone dialer's single no-answer duration source — into the leg's
// bounded ringing watchdog (finding 3 / C-V3: an unanswered signal call
// FailOutcomes(no_answer) and releases its registry slot without a
// carrier event). Changing any parity seam here or in signalleg must
// keep the leg_gate_test.go pins green.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/signal"
	"github.com/caimlas/meept/internal/signalleg"
	"github.com/caimlas/meept/internal/voice"
)

// effectsEffectsSeam adapts an effects.Ledger onto signalleg.EffectsSeam
// (the narrow three-method view; keys fold through effects.EffectKey so
// ledger rows collide with the phone dialer's identical parts). The
// CONCRETE ledger satisfies the view; the wiring keeps the concrete
// handle in components (deps.Effects rides the voice.EffectsLedger
// view, which is the same four methods minus Close - the concrete
// *MemoryLedger/*SQLiteLedger carry all of them).
type effectsEffectsSeam struct {
	claim    func(ctx context.Context, key, employeeID string) (bool, error)
	complete func(ctx context.Context, key, employeeID string) error
	abandon  func(ctx context.Context, key, employeeID, reason string) error
}

func (a effectsEffectsSeam) Reserve(ctx context.Context, key, employeeID string) (bool, error) {
	return a.claim(ctx, key, employeeID)
}

func (a effectsEffectsSeam) Complete(ctx context.Context, key, employeeID string) error {
	return a.complete(ctx, key, employeeID)
}

func (a effectsEffectsSeam) Abandon(ctx context.Context, key, employeeID, reason string) error {
	return a.abandon(ctx, key, employeeID, reason)
}

// newEffectsEffectsSeam folds the voice.EffectsLedger view onto the
// seam, hashing keys through effects.EffectKey so reservation rows
// collide with identical parts across retries.
func newEffectsEffectsSeam(ledger voice.EffectsLedger) effectsEffectsSeam {
	return effectsEffectsSeam{
		claim: func(ctx context.Context, key, employeeID string) (bool, error) {
			granted, _, err := ledger.Claim(ctx, effects.EffectKey("signal.voice_call", key), effects.EffectMeta{
				Tool:      "signal.voice_call",
				SessionID: employeeID,
			}, effects.Claimant{AgentID: employeeID})
			return granted, err
		},
		complete: func(ctx context.Context, key, employeeID string) error {
			return ledger.Complete(ctx, effects.EffectKey("signal.voice_call", key), effects.Claimant{AgentID: employeeID})
		},
		abandon: func(ctx context.Context, key, employeeID, reason string) error {
			return ledger.Abandon(ctx, effects.EffectKey("signal.voice_call", key), effects.Claimant{AgentID: employeeID}, reason)
		},
	}
}

// auditAuditSeam adapts *auditlog.Store onto signalleg.AuditSeam (the
// auditWriterAdapter shape, re-used with a signal-specific record type).
type auditAuditSeam struct {
	w voice.AuditWriter
}

func (a auditAuditSeam) WriteAudit(ctx context.Context, row map[string]any) error {
	return a.w.WriteAudit(ctx, row)
}

// WireSignalLegResult carries the armed leg + its supervisor handle so
// the caller can stop the sidecar in dependency order.
type WireSignalLegResult struct {
	Leg        *signalleg.Leg
	Supervisor *signal.Supervisor
}

// WireSignalLeg constructs the Signal voice leg when [signal].enabled.
// It returns (nil, nil) when the block is disabled (the caller treats
// that as the clean disabled boot - no Leg, no supervisor, no warnings
// beyond an info line). The deps mirror WireVoice's: the SAME registry,
// manager, text server, binding, effects ledger, and audit store the
// phone/local legs run on (one child, one registry, one call store).
// The supervisor is owned by the CALLER for lifecycle; this function
// starts it (idempotent while running) and binds the client.
//
// Degrade posture: an unresolvable signal client (supervisor failed to
// arm) logs a loud WARN and returns a nil Leg with a non-nil result
// struct - the daemon boots, the Signal call paths stay refused. Only a
// disabled [signal] block returns the fully-nil result (silent boot).
func WireSignalLeg(ctx context.Context, cfg config.SignalConfig, deps WireVoiceDeps, registry voice.Registry, manager *voice.Manager, textServer *voice.TextServer, binding *voice.Binding, logger *slog.Logger) (*WireSignalLegResult, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if !cfg.Enabled {
		logger.Info("signal leg disabled ([signal].enabled=false)")
		return nil, nil //nolint:nilnil // documented disabled-boot posture: (nil, nil) means "leg not armed"
	}
	socketPath := cfg.SocketPath
	if socketPath == "" {
		socketPath = config.MeeptPath("signal", "signal.sock")
	}
	sup := signal.NewSupervisor(cfg.Binary, cfg.Account, socketPath, signal.SupervisorConfig{
		IdleStopSeconds:     cfg.IdleStopSeconds,
		RestartBackoffMs:    cfg.RestartBackoffMs,
		RestartMaxBackoffMs: cfg.RestartMaxBackoffMs,
	}, logger.With("component", "signal"))
	if err := sup.Start(ctx); err != nil {
		// Degrade, not fatal: the daemon boots without Signal calls
		// (scratch boots must come up; the wireSlackChannel shape).
		logger.Warn("signal wiring: sidecar failed to arm; signal voice leg degraded to disabled",
			"err", err)
		return &WireSignalLegResult{Leg: nil, Supervisor: sup}, nil
	}
	client, err := sup.Client()
	if err != nil {
		logger.Warn("signal wiring: sidecar client unresolvable; signal voice leg degraded to disabled",
			"err", err)
		return &WireSignalLegResult{Leg: nil, Supervisor: sup}, nil
	}
	if err := client.SubscribeCallEvents("meept-voice"); err != nil {
		logger.Warn("signal wiring: call-event subscription failed; signal voice leg degraded to disabled",
			"err", err)
		return &WireSignalLegResult{Leg: nil, Supervisor: sup}, nil
	}

	allow := signalAllowlist(cfg.AllowedUUIDs)
	var fx signalleg.EffectsSeam
	if deps.Effects != nil {
		fx = newEffectsEffectsSeam(deps.Effects)
	} else {
		logger.Warn("signal wiring: no effects ledger; outbound signal calls run UNRESERVED (scratch-boot posture)")
	}
	var audit signalleg.AuditSeam
	if deps.Audit != nil {
		audit = auditAuditSeam{w: auditWriterAdapter{store: deps.Audit}}
	}
	leg, err := signalleg.New(signalleg.Deps{
		Client:          client,
		Registry:        registry,
		Manager:         manager,
		TextServer:      textServer,
		Binding:         binding,
		Allowlist:       allow,
		Effects:         fx,
		Audit:           audit,
		EmployeeID:      deps.Config.Voice.DefaultEmployee,
		DayBriefBuilder: deps.DayBriefBuilder,
		// Fresh-session flag (audit 2026-11-09 finding 5, signal-leg
		// parity): resolve voice.fresh_session_per_call + the
		// employee_leg override once per call, daemon-side, exactly
		// like the local leg (voice_wiring.go LocalDeps.FreshSession).
		// Without this closure the leg's nil-seam posture (fresh=true)
		// would ignore the global false default.
		FreshSession: func(employeeID string) bool {
			return voice.FreshSessionFor(deps.Config.Voice, employeeID)
		},
		// Ringing watchdog (audit 2026-11-09 finding 3, C-V3): the SAME
		// config source the phone dialer reads — [voice].timeout_secs,
		// folded into DialConfig.TimeoutSecs at voice_wiring.go:416. One
		// knob, both legs; 0/unset resolves leg-side to
		// voice.DefaultNoAnswerSecs (dialer parity).
		RingingTimeout: time.Duration(deps.Config.Voice.TimeoutSecs) * time.Second,
		Logger:         logger.With("component", "signalleg"),
	})
	if err != nil {
		return nil, fmt.Errorf("signal wiring: leg construction: %w", err)
	}
	leg.RegisterCallEvents()
	logger.Info("signal leg armed", "account_set", cfg.Account != "", "allowed_uuids", len(cfg.AllowedUUIDs))
	return &WireSignalLegResult{Leg: leg, Supervisor: sup}, nil
}

// signalAllowlist builds the fail-closed allowlist seam over the
// configured uuids. An empty list admits NOTHING (the caller-facing
// refusal is ErrNotAllowedlisted; the config docs pin the posture).
func signalAllowlist(allowed []string) func(uuid string) bool {
	set := make(map[string]struct{}, len(allowed))
	for _, u := range allowed {
		set[u] = struct{}{}
	}
	return func(uuid string) bool {
		_, ok := set[uuid]
		return ok
	}
}

// StopSignalLeg tears the armed signal stack down in dependency order:
// the leg's calls end through the manager's own fan-out (the daemon
// lifecycle already cancelled the loops); only the sidecar needs an
// explicit stop. Nil-safe (a disabled boot passes nil).
func StopSignalLeg(ctx context.Context, res *WireSignalLegResult) {
	if res == nil || res.Supervisor == nil {
		return
	}
	if err := res.Supervisor.Stop(ctx); err != nil {
		slog.Default().Warn("signal wiring: sidecar stop", "err", err)
	}
}
