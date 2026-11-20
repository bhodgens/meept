package daemon

// voice_link_rpc.go - the daemon-side adapter for the voice.link.* RPC
// surface (deadair-link plan leaf 03). Maps *voice.LinkManager (+ the
// call store + the employee store) onto rpc.VoiceLinkSeams. internal/rpc
// must not import internal/voice (the voice -> activity -> rpc chain),
// so the concrete types stop HERE - mirror of voice_local_rpc.go.
//
// The LinkManager is LAZILY constructed on the first open (the local leg
// is wired at wiring time; a manager built at boot against a nil leg
// would be a typed-nil trap - the repo rule). Closed once per process:
// one manager per daemon, links keyed by id inside it.
//
// Transcript reading: voice.link.status counts per-side turns through
// the CallStore seam. VoiceEmployeeWiring's store field is private but
// this file is in the SAME package (internal/daemon), so the adapter
// reads w.Employee.store directly - no new exported seam needed and no
// edit of voice_employee.go (outside this leaf's fence). A nil wiring /
// nil employee wiring degrades the reader to nil: status still answers
// with routing ids and zero counts, and the CLI says so honestly.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/voice"
)

// linkTranscriptSource is the minimal surface the adapter needs from
// *voice.CallStore (satisfied by it; nil tolerated). Declared here (not
// in internal/voice) to keep the seam explicit.
type linkTranscriptSource interface {
	EndedCall(ctx context.Context, callID string) (voice.CallRecord, []voice.TranscriptLine, bool, error)
}

// voiceLinkState owns the lazily-constructed per-daemon LinkManager.
type voiceLinkState struct {
	mu    sync.Mutex
	mgr   *voice.LinkManager
	leg   *voice.LocalLeg
	store linkTranscriptSource
	turns *voice.LinkTurnRegistry
	cfg   config.VoiceConfig
	// cfgLive resolves the LIVE [voice] snapshot (wired to
	// VoiceWiring.currentVoiceCfg): the backchannel-soften resolver must
	// follow daemon.reloaded republishes, never freeze the boot snapshot
	// (the manager is constructed once, lazily). nil (hand-built state)
	// falls back to the cfg snapshot. The seam is a pure RLock-guarded
	// pointer read - no I/O, safe from any lock context.
	cfgLive func() config.VoiceConfig
	logger  *slog.Logger
}

// voiceCfg returns the [voice] config the link resolvers read: the live
// snapshot when the seam is armed, the construction snapshot otherwise.
func (s *voiceLinkState) voiceCfg() config.VoiceConfig {
	if s.cfgLive != nil {
		return s.cfgLive()
	}
	return s.cfg
}

// backchannelSoftenFor is the per-employee soften resolver handed to
// LinkDeps.BackchannelSoftenFor (as this method value, so the lazily
// constructed-once manager keeps reading the LIVE config on every Open).
func (s *voiceLinkState) backchannelSoftenFor(empID string) bool {
	return voice.BackchannelSoftenFor(s.voiceCfg(), empID)
}

// manager returns the link manager, constructing it on first use. A nil
// leg means the local leg is disabled: no manager, ever (opens refuse
// via the nil seams below).
func (s *voiceLinkState) manager() *voice.LinkManager {
	if s == nil || s.leg == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mgr == nil {
		m, err := voice.NewLinkManager(voice.LinkDeps{
			Leg:         s.leg,
			Store:       s.store,
			Turns:       s.turns,
			DialTimeout: 5 * time.Second,
			RetainEnded: 30 * time.Minute,
			Logger:      s.logger.With("sub", "voice-link"),
			// Backchannel soften (#130/#143): the PER-EMPLOYEE
			// resolver — a link spans two employees, so each
			// side's voice.employee_leg row (row > global)
			// resolves against that side's employee id at
			// LinkManager.Open. No row on either side falls
			// through to the global voice.backchannel_soften
			// (default false = today's coordinator, byte-
			// identical). Reads the LIVE snapshot through
			// voiceCfg (bughunt 2026-11-19 wave 3): the manager
			// is constructed once, so a boot-snapshot read froze
			// the toggle across daemon.reloaded republishes.
			BackchannelSoftenFor: s.backchannelSoftenFor,
		})
		if err != nil {
			s.logger.Error("voice link manager construction failed", "err", err)
			return nil
		}
		s.mgr = m
	}
	return s.mgr
}

// newVoiceLinkState builds the per-daemon link state over the wiring.
// Split out of voiceLinkSeams so the reload-live soften pin can drive the
// EXACT production construction (cfgLive arming included).
func newVoiceLinkState(w *VoiceWiring, store linkTranscriptSource, logger *slog.Logger) *voiceLinkState {
	return &voiceLinkState{
		leg:   w.Local,
		store: store,
		turns: w.linkTurns,
		cfg:   w.cfg,
		// Reload-live backchannel soften (bughunt 2026-11-19 wave 3):
		// the resolver reads the LIVE snapshot seam, so a config swap
		// via reloadVoiceConfig rebinds the toggle without touching the
		// constructed-once LinkManager (mirrors the wave-1 SetWindow
		// precedent; nil-safe: currentVoiceCfg falls back to w.cfg).
		cfgLive: w.currentVoiceCfg,
		logger:  logger,
	}
}

// voiceLinkSeams adapts the wiring's voice pieces onto the RPC seam set.
// w == nil or w.Local == nil (voice.local.enabled false, wiring
// degraded): seams all-nil = the handler's disabled posture. store may
// be nil (transcript counts degrade to zero; status still routes).
func voiceLinkSeams(w *VoiceWiring, store linkTranscriptSource, mgr *employee.Manager, logger *slog.Logger) rpc.VoiceLinkSeams {
	if logger == nil {
		logger = slog.Default()
	}
	if w == nil || w.Local == nil {
		return rpc.VoiceLinkSeams{}
	}
	state := newVoiceLinkState(w, store, logger)
	// Registry rules (busy / duplicate target) surface from inside
	// LocalLeg.Open as voice.ErrBusy / voice.ErrDuplicateTarget and are
	// mapped by mapVoiceLinkOpenErr - no registry read needed here.

	open := func(ctx context.Context, employeeA, employeeB string, opts rpc.VoiceLinkOpenOptions) (rpc.VoiceLinkOpen, error) {
		m := state.manager()
		if m == nil {
			return rpc.VoiceLinkOpen{}, rpc.ErrVoiceLinkDisabled
		}
		link, err := m.Open(ctx, employeeA, employeeB, voice.LinkOpenOptions{Monitor: opts.Monitor})
		if err != nil {
			return rpc.VoiceLinkOpen{}, mapVoiceLinkOpenErr(err)
		}
		return rpc.VoiceLinkOpen{
			LinkID:       link.ID,
			ACCID:        link.ACCID,
			BCCID:        link.BCCID,
			MonitorAPath: link.MonitorAPath,
			MonitorBPath: link.MonitorBPath,
		}, nil
	}

	closeLink := func(linkID, reason string) error {
		m := state.manager()
		if m == nil {
			return rpc.ErrVoiceLinkDisabled
		}
		if err := m.Close(linkID, reason); err != nil {
			if errors.Is(err, voice.ErrLinkNotFound) {
				return fmt.Errorf("%w: %w", rpc.ErrVoiceLinkUnknownLink, err)
			}
			return err
		}
		return nil
	}

	status := func(ctx context.Context, linkID string) (rpc.VoiceLinkStatusResult, error) {
		m := state.manager()
		if m == nil {
			return rpc.VoiceLinkStatusResult{}, rpc.ErrVoiceLinkDisabled
		}
		res, err := m.Status(ctx, linkID)
		if err != nil {
			if errors.Is(err, voice.ErrLinkNotFound) {
				return rpc.VoiceLinkStatusResult{}, fmt.Errorf("%w: %w", rpc.ErrVoiceLinkUnknownLink, err)
			}
			return rpc.VoiceLinkStatusResult{}, err
		}
		return rpc.VoiceLinkStatusResult{
			LinkID: res.LinkID,
			Ended:  res.Ended,
			A:      voiceLinkSide(res.A),
			B:      voiceLinkSide(res.B),
		}, nil
	}

	exists := func(ctx context.Context, employeeID string) (bool, error) {
		if mgr == nil {
			// No employee store wired: skip the existence gate (the
			// leg's own open gates still apply downstream), exactly
			// like voiceLocalSeams.
			return true, nil
		}
		emp, err := mgr.GetEmployee(ctx, employeeID)
		if err != nil {
			if errors.Is(err, employee.ErrEmployeeNotFound) {
				return false, nil
			}
			return false, err
		}
		return emp != nil, nil
	}

	return rpc.VoiceLinkSeams{
		Open:           open,
		Close:          closeLink,
		Status:         status,
		EmployeeExists: exists,
	}
}

// voiceLinkSide maps one voice-side LinkSide onto the RPC row (lines
// always non-nil).
func voiceLinkSide(s voice.LinkSide) rpc.VoiceLinkSideStatus {
	lines := make([]rpc.VoiceLinkTranscriptLine, 0, len(s.Transcript))
	for _, l := range s.Transcript {
		lines = append(lines, rpc.VoiceLinkTranscriptLine{
			Role: l.Role,
			Text: l.Text,
			At:   l.At.Format(time.RFC3339),
		})
	}
	return rpc.VoiceLinkSideStatus{
		CallID:         s.CallID,
		EmployeeID:     s.EmployeeID,
		AssistantTurns: s.AssistantTurns,
		UserTurns:      s.UserTurns,
		Transcript:     lines,
	}
}

// mapVoiceLinkOpenErr maps the link manager's named refusals onto the
// RPC surface's named errors (mirror of mapVoiceLocalOpenErr). Unknown
// errors pass through wrapped.
func mapVoiceLinkOpenErr(err error) error {
	switch {
	case errors.Is(err, voice.ErrLinkSameEmployee):
		return fmt.Errorf("%w: %w", rpc.ErrVoiceLinkSameEmployee, err)
	case errors.Is(err, voice.ErrLocalDisabled):
		return fmt.Errorf("%w", rpc.ErrVoiceLinkDisabled)
	case errors.Is(err, voice.ErrNoEmployee):
		return fmt.Errorf("%w: %w", rpc.ErrVoiceLinkUnknownEmployee, err)
	case errors.Is(err, voice.ErrBusy):
		return fmt.Errorf("%w: %w", rpc.ErrVoiceLocalBusy, err)
	case errors.Is(err, voice.ErrDuplicateTarget):
		return fmt.Errorf("%w: %w", rpc.ErrVoiceLocalDuplicate, err)
	default:
		return err
	}
}

// linkTranscriptSourceFor exposes the voice employee wiring's CallStore
// as the link status transcript seam, with the typed-nil guard this
// package's rule requires (a nil *voice.CallStore boxed into the
// interface would pass a != nil check and deref on first use).
// registerVoiceLinkRPC calls it; keeping it here (this leaf's file)
// means voice_employee.go needs no edit.
func linkTranscriptSourceFor(w *VoiceWiring) linkTranscriptSource {
	if w == nil || w.Employee == nil || w.Employee.store == nil {
		return nil
	}
	return w.Employee.store
}

// registerVoiceLinkRPC registers the voice.link.* surface on the RPC
// server. Always registers (nil wiring / disabled local leg = the named
// disabled refusal, never "method not found"). Pattern:
// registerVoiceLocalRPC - a wiring helper called from daemon New before
// the listeners serve. THE ORCHESTRATOR MUST ADD the one-line call in
// internal/daemon/daemon.go next to registerVoiceLocalRPC (that file is
// outside this leaf's fence):
//
//	registerVoiceLinkRPC(components, rpcServer, logger)
func registerVoiceLinkRPC(c *Components, server *rpc.Server, logger *slog.Logger) {
	if server == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	var w *VoiceWiring
	if c != nil {
		w = c.Voice
	}
	seams := voiceLinkSeams(w, linkTranscriptSourceFor(w), employeeManagerFor(c), logger)
	handler := rpc.NewVoiceLinkHandler(seams)
	handler.RegisterVoiceLinkMethods(server)
	if w != nil && w.Local != nil {
		logger.Info("voice.link RPC handlers registered")
	} else {
		logger.Info("voice.link RPC handlers registered (disabled local leg: opens refuse)")
	}
}
