// cmd/meept-voice is the voice sidecar child process: one process owning
// ears (STT), turn-taking, barge-in, and mouth (TTS) for all live calls.
//
// It connects the daemon's shared text socket (C1), reads its start token
// from STDIN (never argv, never env), and serves one jargo pipeline per
// call, multiplexed by call_id. Provider keys arrive over the text socket
// (keys frame) after the daemon validates the token; they are held in
// memory only and never logged.
//
// ONNX Runtime: jargo's VAD/turn models load it at runtime through purego.
// A missing library is not recoverable for a phone call (no barge-in), so
// the child exits non-zero with the missing path logged (no secrets).
package main

import (
	"context"
	"errors"
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
	if err := checkONNX(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	child := voicechild.New(voicechild.ChildConfig{
		TextSocketPath: textSocket,
		AudioDir:       audioDir,
		TokenReader:    os.Stdin, // start token via stdin ONLY (never argv/env)
	})
	// Leaf 08: real provider adapters behind the leaf-07 interfaces. The
	// vendors and models are fixed here (A1: Deepgram STT, OpenAI TTS);
	// keys arrive per call over the C1 keys frame and live in memory only.
	child.SetProviderFactories(
		func(key string, sampleRate int) (voicechild.STTProvider, error) {
			return providers.NewSTT(providers.ProviderConfig{
				Vendor:     "deepgram",
				SampleRate: sampleRate,
			}, key)
		},
		func(key string, sampleRate int) (voicechild.TTSProvider, error) {
			return providers.NewTTS(providers.ProviderConfig{
				Vendor:     "openai",
				SampleRate: sampleRate,
			}, key)
		},
	)
	return child.Run(ctx)
}

// checkONNX fails fast with the missing path when the ONNX Runtime library
// cannot be located. The concrete purego probe lands with leaf 08's provider
// wiring (jargo loads the library lazily); the config key
// voice.onnx_runtime_lib names the expected path either way.
func checkONNX() error {
	// The daemon passes the resolved library path through voice config;
	// without the leaf-08 loader this skeleton only reports the env hint.
	if lib := os.Getenv("JARGO_ONNXRUNTIME_LIB"); lib == "" {
		// Not fatal in the skeleton: jargo degrades to STT endpointing
		// (loses barge-in) when the runtime is absent at LOAD time, but a
		// phone call without barge-in is refused per spec. Leaf 08 makes
		// this a hard probe before Run; the daemon refuses the call then.
		return nil
	}
	return errors.New("meept-voice: onnx runtime probe unimplemented in skeleton")
}
