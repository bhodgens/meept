// cmd/meept-voice is the voice sidecar child process: one process owning
// ears (STT), turn-taking, barge-in, and mouth (TTS) for all live calls.
//
// It connects the daemon's shared text socket (C1), reads its start token
// from STDIN (never argv, never env), and serves one jargo pipeline per
// call, multiplexed by call_id. Provider keys AND the per-call vendor/
// model/base_url/voice selectors arrive over the text socket (extended
// keys frame, voice-tierA leaf 03) after the daemon validates the token;
// they are held in memory only and never logged. The C3 factories below
// dispatch on the keys frame's config, so provider selection is per call.
//
// Barge-in trigger (voice-tierA D1): STT partial transcripts from the
// bound STT session - a partial while playback is live emits interrupt
// before the partial's prompt (C5), once per barge-in. jargo's ONNX VAD
// is NOT required for this: the ONNX Runtime probe stays non-fatal and
// a missing library only means the (future) VAD upgrade is absent, not
// that barge-in is lost. See docs/workflows/voice-sidecar.md Tier A
// as-built notes.
//
// ONNX Runtime: jargo's VAD/turn models load it at runtime through purego.
// A missing library is not recoverable for a phone call (no barge-in), so
// the child exits non-zero with the missing path logged (no secrets).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/caimlas/meept/internal/voicechild"
	"github.com/caimlas/meept/internal/voicechild/providers"
)

func main() {
	textSocket := flag.String("text-socket", "", "path to the daemon text socket (C1)")
	audioDir := flag.String("audio-dir", "", "directory holding per-call audio socket pairs (C2)")
	flag.Parse()

	if *textSocket == "" {
		fmt.Fprintln(os.Stderr, "meept-voice: -text-socket is required")
		os.Exit(2)
	}
	if *audioDir == "" {
		fmt.Fprintln(os.Stderr, "meept-voice: -audio-dir is required")
		os.Exit(2)
	}

	if err := run(context.Background(), *textSocket, *audioDir); err != nil {
		fmt.Fprintln(os.Stderr, "meept-voice:", err)
		os.Exit(1)
	}
}

// run wires the child and blocks until ctx or a signal ends it.
func run(ctx context.Context, textSocket, audioDir string) error {
	// The daemon passes the ONNX Runtime library path as VOICE_ONNX_LIB
	// (a path, not a secret); jargo's onnxrt loader looks for
	// JARGO_ONNXRUNTIME_LIB (or the bare platform name). Translate here so
	// the Silero VAD / Smart Turn analyzers can load the real lib
	// regardless of the platform search path (this machine: homebrew's
	// /opt/homebrew/lib is not on the child's dyld default search path).
	if lib := os.Getenv("VOICE_ONNX_LIB"); lib != "" {
		if os.Getenv("JARGO_ONNXRUNTIME_LIB") == "" {
			_ = os.Setenv("JARGO_ONNXRUNTIME_LIB", lib) //nolint:errcheck // best-effort; a failure leaves the env hint
		}
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	child := voicechild.New(voicechild.ChildConfig{
		TextSocketPath: textSocket,
		AudioDir:       audioDir,
		TokenReader:    os.Stdin, // start token via stdin ONLY (never argv/env)
	})
	// Clip-playback rate converter (filler-clips leaf 03, A1): stored PCM
	// is canonical s16le@16k; a call negotiated at any other rate reads
	// clips through jargo's streaming resampler. voicechild cannot import
	// providers (cycle), so the seam is injected here.
	child.SetResampler(providers.ResampleS16Mono)
	// Real provider adapters behind the leaf-07 interfaces, dispatched on
	// the per-call config the child passes from the C1 keys frame (C3):
	// the vendor, model, base URL, and voice now come from the daemon per
	// call, not hardcoded here. Keys ride the frame and live in memory
	// only. An unknown vendor fails the call (named error); the child and
	// other calls stay up.
	child.SetProviderFactories(
		func(cfg providers.ProviderConfig, key string) (voicechild.STTProvider, error) {
			// NewSTT dispatches on cfg.Vendor: "deepgram" (cloud, key
			// required) or "whisper" (keyless local whisper.cpp
			// subprocess). A cfg naming anything else fails the call
			// with the named vendor error.
			return providers.NewSTT(cfg, key)
		},
		func(cfg providers.ProviderConfig, key string) (voicechild.TTSProvider, error) {
			// NewTTS dispatches on cfg.Vendor: "openai" (cloud, key
			// required) or "qwen3" (keyless local, cfg.BaseURL required).
			// Model/BaseURL/Voice ride the config untouched.
			return providers.NewTTS(cfg, key)
		},
	)
	// Input-side ML speech discriminator (jargo Silero VAD, local leg):
	// lets the pipeline gate the RMS barge-in on real SPEECH so loud noise
	// / self-echo never trips it. The factory errors only when the ONNX
	// runtime library is absent — the pipeline degrades to the legacy
	// RMS + STT-evidence gate for that call, never fails the call.
	child.SetVADFactory(func(sampleRate int) (voicechild.VADAnalyzer, error) {
		return providers.NewSileroVAD(sampleRate)
	})
	// Input-side ML complete-thought analyzer (jargo Smart Turn, local
	// leg): ML replacement for the whisper-punctuation endpointing oracle.
	// Degrades to the punctuation oracle when the ONNX lib is absent.
	child.SetTurnFactory(func(sampleRate int) (voicechild.TurnAnalyzer, error) {
		a, err := providers.NewSmartTurnVAD(sampleRate)
		if err == nil {
			// Inform the analyzer of the Silero VAD's onset delay (0.2 s
			// StartSecs) so its pre-speech buffer widens to cover what
			// the VAD held back before confirming speech.
			a.SetVADStartSecs(0.2)
		}
		return a, err
	})
	return child.Run(ctx)
}
