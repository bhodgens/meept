package daemon

// Voice plain-listener boot (audit HIGH: voice.loopback_plain_addr was a
// DEAD KNOB — StartVoicePlainListener had no production caller, only
// tests). VoiceWiring.Start calls startPlainListener below whenever the
// config names an address, so the second cleartext loopback listener for
// TLS-terminating tunnels (leaf 10 / plan 20261102-voice-sidecar T6)
// actually runs. The listener rides w.stopCtx: VoiceWiring.Stop cancels it,
// and the subsystem's 5s graceful shutdown closes it with the rest of the
// voice stack.
//
// A non-loopback bind (ErrNonLoopbackVoiceAddr) fails the boot LOUDLY —
// pre-validated synchronously before any goroutine starts, matching the
// voice block's rule that a half-configured voice never degrades silently.

import (
	"context"
	"errors"
	"fmt"
	"time"

	commhttp "github.com/caimlas/meept/internal/comm/http"
)

// plainBootProbeWindow bounds how long the boot waits for an immediate
// listener failure (non-loopback re-check after bind, address in use)
// before declaring the listener armed. Bind failures surface in
// milliseconds; anything slower than this window is a live listener.
// Keep this small — it is paid on every voice boot that sets the knob.
const plainBootProbeWindow = 250 * time.Millisecond

// startPlainListener boots the optional plain-HTTP loopback voice listener
// when voice.loopback_plain_addr is set (no-op when empty — the knob is
// opt-in, default off). Returns an error ONLY for boot-fatal conditions:
// a refused address (wrapping commhttp.ErrNonLoopbackVoiceAddr) or an
// immediate bind failure. Post-boot serve failures land on the Error log.
func (w *VoiceWiring) startPlainListener(ctx context.Context) error {
	addr := w.cfg.LoopbackPlainAddr
	if addr == "" {
		return nil
	}
	if w.Routes == nil {
		return errors.New("voice wiring: voice.loopback_plain_addr set but voice routes are nil")
	}
	// Synchronous pre-validation: ErrNonLoopbackVoiceAddr must fail the
	// boot loudly, before any goroutine exists. The actual-bind re-check
	// (localhost resolving to a routable IP) still happens inside the
	// listener and lands on the early-failure path below.
	if err := commhttp.StartVoicePlainListenerWith(ctx, nil, nil, addr); err != nil {
		return fmt.Errorf("voice wiring: plain listener: %w", err)
	}

	opts := w.plainOpts
	resCh := make(chan error, 1)
	go func() { resCh <- commhttp.StartVoicePlainListener(ctx, addr, w.Routes, &opts) }()

	select {
	case err := <-resCh:
		// Listener already returned: either a bind/refusal error (fatal
		// for boot) or an immediate clean exit — report both loudly.
		if err != nil {
			return fmt.Errorf("voice wiring: plain listener: %w", err)
		}
		w.logger.Warn("voice wiring: plain listener exited immediately", "addr", addr)
		return nil
	case <-time.After(plainBootProbeWindow):
	}

	// Still serving after the probe window: hand the eventual result to a
	// logger (exactly one send rides resCh, so this read always drains
	// when Serve returns — ctx cancel ends it with nil).
	go func() {
		err := <-resCh
		if err != nil {
			w.logger.Error("voice wiring: plain listener serve ended with error", "addr", addr, "error", err)
			return
		}
		w.logger.Info("voice wiring: plain listener stopped", "addr", addr)
	}()
	w.logger.Info("voice wiring: plain listener armed", "addr", addr)
	return nil
}
