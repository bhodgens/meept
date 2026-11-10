package main

// voice_tui.go - the TUI voice dialer wiring (local-leg leaf 13a).
// internal/tui cannot import internal/voice (voice transitively imports
// tui via activity -> rpc), so the concrete leaf 08 audio shim is built
// HERE - the same construction cmd/meept/voice.go runVoiceLocalLive
// uses - and injected into the TUI as a dialer seam:
//
//	mic:   voice.NewRawMicCapture (ffmpeg live capture, darwin)
//	spk:   voice.NewSpeakerSink draining the .out socket into oto
//
// No new audio code: this is pure reuse of leaf 08's client shim.

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/caimlas/meept/internal/tui"
	"github.com/caimlas/meept/internal/tui/models"
	"github.com/caimlas/meept/internal/voice"
)

// tuiVoiceAudioSession is one live audio attachment for the TUI's
// voice session: both pair sockets dialed, mic streaming in, speaker
// sink draining out. Stop tears both directions down.
type tuiVoiceAudioSession struct {
	cancel   context.CancelFunc
	capture  *voice.RawMicCapture
	sink     *voice.SpeakerSink
	inConn   net.Conn
	outConn  net.Conn
	done     chan error
	stopOnce chan struct{}
}

// dialTUIVoiceAudio attaches the leaf 08 audio shim to the open call's
// pair sockets. Shapes mirror cmd/meept/voice.go runVoiceLocalLive
// (leaf 08 T5) - no new audio code.
func dialTUIVoiceAudio(inPath, outPath string) (models.VoiceAudioSession, error) {
	capture, err := voice.NewRawMicCapture("", "")
	if err != nil {
		return nil, err
	}
	player := voice.NewOtoPlayer()
	sink := voice.NewSpeakerSink(player, 0)

	inConn, err := net.Dial("unix", inPath)
	if err != nil {
		capture.Close() //nolint:errcheck // rollback of a fresh capture
		return nil, fmt.Errorf("dial mic socket %s: %w", inPath, err)
	}
	outConn, err := net.Dial("unix", outPath)
	if err != nil {
		inConn.Close()  //nolint:errcheck // rollback
		capture.Close() //nolint:errcheck // rollback
		return nil, fmt.Errorf("dial speaker socket %s: %w", outPath, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &tuiVoiceAudioSession{
		cancel:   cancel,
		capture:  capture,
		sink:     sink,
		inConn:   inConn,
		outConn:  outConn,
		done:     make(chan error, 2),
		stopOnce: make(chan struct{}),
	}

	// mic: capture -> in socket.
	go func() {
		_, err := io.Copy(inConn, capture)
		s.done <- err
	}()
	// speaker: out socket -> sink (raw PCM bursts -> oto), with the same
	// bounded lookahead as the CLI live loop (leaf 01: drainLiveSpeaker
	// is the single implementation - no new audio code here either).
	go drainLiveSpeaker(outConn, s.sink, s.done)
	// Drain pump errors into a quiet teardown; the TUI session is
	// ended by the user (/voice stop) or the daemon (call end), so a
	// pump error here just releases the resources.
	go func() {
		select {
		case err := <-s.done:
			if err != nil {
				_ = s.Stop() // release sockets + shim on pump failure
			}
		case <-ctx.Done():
		}
	}()
	return s, nil
}

// Stop tears the audio session down: capture closed (ffmpeg reaped via
// ctx cancel), sink flushed and released, both sockets closed. Safe to
// call multiple times (the model calls it exactly once; the pump-drain
// goroutine may race it).
func (s *tuiVoiceAudioSession) Stop() error {
	select {
	case <-s.stopOnce:
		return nil
	default:
		close(s.stopOnce)
	}
	s.cancel()
	_ = s.inConn.Close()  //nolint:errcheck // teardown
	_ = s.outConn.Close() //nolint:errcheck // teardown
	_ = s.capture.Close() //nolint:errcheck // teardown; ffmpeg reaped
	return s.sink.Close()
}

// wireTUIVoiceDialer injects the leaf 08 shim dialer into the app
// (control-only posture when this is skipped, e.g. tests).
func wireTUIVoiceDialer(app *tui.App) {
	app.SetVoiceAudioDialer(dialTUIVoiceAudio)
}
