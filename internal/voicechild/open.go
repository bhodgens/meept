package voicechild

// The C1 open frame (local-leg plan CL1): the daemon's per-call trigger.
// Before this frame existed, a pipeline was built only by handleSetup, and
// handleSetup only ran on a setup the CHILD sends - which requires a
// pipeline. Production therefore armed no pipeline, no setup, and no talk
// loop. open closes that loop: daemon opens, child builds and dials, child
// answers with the existing setup frame the daemon's router already arms
// a talk loop on.

import (
	"context"
	"errors"

	"github.com/caimlas/meept/internal/voiceframe"
)

// handleOpen creates the call's pipeline from the open frame's identity,
// stores the leg profile (local marker + RMS threshold; provider selection
// keeps arriving over the keys frame, Tier A's wire), and answers with a
// setup frame mirroring the open frame's identity. The daemon's routeFrame
// EventSetup branch then arms the talk loop.
//
// Idempotent: an open for an already-live ccid is a debug no-op, mirroring
// handleSetup (bughunt 2026-11-07 voice F-6 is the daemon-side mirror).
// handleSetup stays able to build a pipeline standalone so an old daemon
// that sends no open frame keeps working unchanged.
func (c *Child) handleOpen(ctx context.Context, frame voiceframe.Frame) error {
	if frame.CallID == "" {
		return errors.New("voicechild: open frame with empty call_id")
	}
	if _, ok := c.getPipeline(frame.CallID); ok {
		c.logger.Debug("voicechild: open for live call ignored", "call_id", frame.CallID)
		return nil
	}
	p, err := c.newPipeline(frame)
	if err != nil {
		return err
	}
	// Leg profile only: zero values on a phone open stay zero, so nothing
	// downstream behaves differently from before this frame existed.
	p.local = frame.Local
	p.bargeInRms = frame.BargeInRms
	c.registerPipeline(p)
	c.logger.Info("voicechild: pipeline registered from open",
		"call_id", frame.CallID, "codec", frame.Codec, "sample_rate", frame.SampleRate,
		"local", frame.Local)

	// Answer with setup so the existing daemon router arms the talk loop.
	// Identity mirrors the open frame the daemon just sent (the daemon
	// never needs to parse it back - routeFrame resolves identity from
	// its own registry instance).
	if err := p.start(ctx); err != nil {
		return err
	}
	if err := c.sendFrame(ctx, voiceframe.Frame{
		CallID:     frame.CallID,
		Event:      voiceframe.EventSetup,
		From:       frame.From,
		To:         frame.To,
		Direction:  frame.Direction,
		Codec:      frame.Codec,
		SampleRate: frame.SampleRate,
	}); err != nil {
		c.logger.Warn("voicechild: setup answer failed", "call_id", frame.CallID, "error", err)
		return err
	}
	return nil
}
