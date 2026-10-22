package agent

// Classifier boot-readiness signal (boot-window misroute fix).
//
// Defect (observed live 2026-10-22): the daemon's RPC/HTTP listeners go live
// in Run() at registry.StartAll -- BEFORE the F-D8 classifier boot gate
// (internal/daemon/classifier_boot_gate.go) finishes waiting for the local
// classifier runtimes' health. A chat submitted inside that boot window hits
// LLMClassifier.Classify while the classifier endpoints still refuse
// connections: the call fails, arms the 60s unavailability cooldown, and the
// dispatcher silently degrades to keyword/heuristic routing
// (classification_method=heuristic_fallback, confidence=0.55). The user gets
// no signal that routing was degraded, and the degraded route can pick the
// wrong agent.
//
// The fix is NOT to delay listener readiness (socket readiness must not
// block on slow model loads -- that is the documented StartAll design) and NOT
// to hard-fail the turn (warmup failures must never fail a turn). Instead the
// boot gate's outcome is handed to the classifier as a one-shot readiness
// signal: classification calls that arrive while the gate is still open wait
// -- bounded and context-cancellable -- for the gate to resolve before touching
// the wire. The gate resolves in one of two ways:
//
//   - SignalReady:   every local classifier runtime answered /health (or the
//     chain is cloud-only / the gate is disabled). The waiting classify call
//     proceeds against a healthy endpoint -- full-fidelity routing.
//   - SignalFailed:  the gate tripped; the daemon is shutting down. Waiters
//     are released immediately with ErrClassifierBootGateFailed and proceed
//     to the ordinary attempt path, which degrades to keyword fallback as
//     before -- a turn is never hard-failed by the boot gate.
//
// A Wait that exceeds its caller-provided bound (or whose context is
// cancelled) also proceeds to the ordinary attempt path: the wait can only
// ever shorten the degraded window, never extend a failure into one.
//
// Steady state (after boot) costs one non-blocking select per classify call.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrClassifierBootGateFailed is returned by ClassifierBootReadiness.Wait
// when the F-D8 boot gate tripped (the local classifier never became healthy
// and the daemon is shutting down). Callers must treat it as "proceed with
// the ordinary classification attempt", never as a turn failure.
var ErrClassifierBootGateFailed = errors.New("classifier boot gate failed")

// ClassifierBootReadiness is a one-shot, goroutine-safe signal carrying the
// daemon's classifier boot-gate outcome to the classification path. The zero
// value is unusable; construct with NewClassifierBootReadiness. SignalReady
// and SignalFailed are idempotent and mutually first-wins (the gate has
// exactly one outcome).
type ClassifierBootReadiness struct {
	ch     chan struct{}
	once   sync.Once
	failed atomic.Bool
}

// NewClassifierBootReadiness returns an unresolved readiness signal.
func NewClassifierBootReadiness() *ClassifierBootReadiness {
	return &ClassifierBootReadiness{ch: make(chan struct{})}
}

// SignalReady reports that the boot gate resolved successfully: every local
// classifier runtime is healthy (or no gate applies). Idempotent.
func (b *ClassifierBootReadiness) SignalReady() {
	if b == nil {
		return
	}
	b.once.Do(func() { close(b.ch) })
}

// SignalFailed reports that the boot gate tripped. Waiters are released with
// ErrClassifierBootGateFailed so they fall through to the ordinary
// attempt/degrade path instead of blocking on a dying daemon. Idempotent;
// a prior SignalReady wins (first outcome is final).
func (b *ClassifierBootReadiness) SignalFailed() {
	if b == nil {
		return
	}
	b.once.Do(func() {
		b.failed.Store(true)
		close(b.ch)
	})
}

// Done reports whether the signal has resolved (either outcome). A nil
// receiver reports true: no signal wired means nothing to wait for.
func (b *ClassifierBootReadiness) Done() bool {
	if b == nil {
		return true
	}
	select {
	case <-b.ch:
		return true
	default:
		return false
	}
}

// Failed reports whether the gate outcome was a failure.
func (b *ClassifierBootReadiness) Failed() bool {
	return b != nil && b.failed.Load()
}

// Wait blocks until the signal resolves or ctx is done. It returns nil on
// SignalReady, ErrClassifierBootGateFailed on SignalFailed, and ctx.Err() on
// cancellation/timeout. A nil receiver returns nil immediately.
func (b *ClassifierBootReadiness) Wait(ctx context.Context) error {
	if b == nil {
		return nil
	}
	select {
	case <-b.ch:
		if b.failed.Load() {
			return ErrClassifierBootGateFailed
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// awaitClassifierBoot is the shared bounded wait used by every component
// that dials the local classifier endpoint (LLMClassifier, IntentAnalyzer).
// It blocks -- capped by wait and ctx -- while the boot gate is still open,
// and NEVER fails a call: on gate failure, wait expiry, or cancellation the
// caller proceeds through its ordinary attempt/degrade path. component names
// the caller in the log lines.
func awaitClassifierBoot(ctx context.Context, logger Logger, readiness *ClassifierBootReadiness, wait time.Duration, component string) {
	if readiness == nil || readiness.Done() {
		return
	}
	if wait <= 0 {
		wait = defaultClassifierBootWait
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	if logger != nil {
		logger.Info("Classification call waiting for boot gate (classifiers still warming up)",
			"component", component, "max_wait", wait)
	}
	if err := readiness.Wait(waitCtx); err != nil && logger != nil {
		logger.Warn("Classifier boot-gate wait ended without readiness; proceeding with ordinary classification path",
			"component", component, "error", err)
	}
}
