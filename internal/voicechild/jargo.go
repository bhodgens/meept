package voicechild

import (
	"context"
	"errors"

	"github.com/gojargo/jargo/frames"
	jargo "github.com/gojargo/jargo/pipeline"
	"github.com/gojargo/jargo/processor"
)

// buildJargoWorker constructs the per-call jargo worker. One live call =
// one jargo pipeline (pipeline.New + pipeline.NewWorker), concurrent with
// every other call's worker (jargo is concurrent by design).
//
// Processor chain (leaf 07 skeleton; leaf 08 replaces the stub services):
//
//	mccpIn (MeeptCallInProcessor)  - transport input seam: daemon .in PCM
//	  would enter here; STT final transcripts get converted to C1 prompt
//	  frames by mccp.processTranscript.
//	stubSource                     - terminal/sink: consumes frames.
//
// The transport seam is deliberately NOT jargo's rtc transport (leaf 07
// keeps the C2 unix-socket contract; master.md Notes): the pipeline exists
// so leaf 08 can slot jargo service processors (stt.NewSegment, tts.Base,
// VAD, turn-taking) into this chain without reshaping the child.
//
// API drift vs the leaf brief (jargo v0.1.0, pinned):
//   - leaf says "pipeline.New(procs...), pipeline.NewWorker" - matches.
//   - leaf's stub interfaces (STTProvider/TTSProvider) do NOT exist in
//     jargo; jargo v0.1.0 composes service processors (stt.SegmentService
//     with a Transcriber; tts.Base with a Synthesizer). We keep the leaf's
//     interfaces as OUR seams (leaf 08 will bridge them to jargo service
//     processors); the skeleton worker therefore runs a minimal chain and
//     records the wiring, not the full STT->TTS topology.
//   - Worker.Run(ctx) blocks until EndFrame/CancelFrame; the skeleton
//     stores the run func so close() can cancel via the pipeline context.
func buildJargoWorker(p *pipeline) error {
	in := newMeeptCallInProcessor(p)
	sink := newStubSink()

	pipe := jargo.New(in, sink)
	worker := jargo.NewWorker(pipe, jargo.WorkerConfig{
		Name: "meept-call-" + p.ccid,
		Params: jargo.Params{
			AudioInSampleRate:  p.sampleRt,
			AudioOutSampleRate: p.sampleRt,
			StartMetadata:      map[string]any{"call_id": p.ccid},
		},
	})

	// Run on the pipeline's own context; cancel stops the worker (CancelFrame
	// path). Run returns when the pipeline ends.
	pctx := p.ctx
	p.workerRun = func() {
		if err := worker.Run(pctx); err != nil && !errors.Is(err, context.Canceled) && pctx.Err() == nil {
			p.logger.Error("voicechild: jargo worker ended", "err", err, "call_id", p.ccid)
		}
	}
	return nil
}

// MeeptCallInProcessor is the child-side entry processor: it would receive
// jargo frames from the transport input stage (leaf 08 wires the C2 .in
// pump into it). In the skeleton it forwards everything downstream.
type MeeptCallInProcessor struct {
	*processor.Base
	p *pipeline
}

func newMeeptCallInProcessor(p *pipeline) *MeeptCallInProcessor {
	m := &MeeptCallInProcessor{p: p}
	m.Base = processor.New("MeeptCallIn", m)
	return m
}

// ProcessFrame forwards every frame downstream (skeleton pass-through).
// Leaf 08 intercepts TranscriptionFrame here to call
// p.child.emitPrompt (final -> last:true, interim -> last:false) and
// UserStartedSpeakingFrame during playback to arm the interrupt latch.
func (m *MeeptCallInProcessor) ProcessFrame(ctx context.Context, f frames.Frame, dir processor.Direction) error {
	return m.PushFrame(ctx, f, dir)
}

// StubSink terminates the downstream chain (skeleton; leaf 08 replaces the
// TTS output stage with the .out socket writer).
type StubSink struct {
	*processor.Base
}

func newStubSink() *StubSink {
	s := &StubSink{}
	s.Base = processor.New("MeeptSink", s)
	return s
}

// ProcessFrame consumes frames (returns without pushing: end of chain).
func (s *StubSink) ProcessFrame(ctx context.Context, f frames.Frame, dir processor.Direction) error {
	return nil
}

var (
	_ processor.Processor = (*MeeptCallInProcessor)(nil)
	_ processor.Processor = (*StubSink)(nil)
)
