package voice

// clientaudio.go - the CLI client's audio shims (local-leg leaf 08, T3
// + leaf 13 reuse): RawMicCapture feeds the .in socket (mic via the
// ffmpeg pipe, or a --in file as raw s16le), SpeakerSink drains the
// .out socket into per-burst WAVs handed to a player seam.
//
// Burst contract (leaf 08 spec, amended 2026-11-09): buffer PCM between
// playback bursts and hand each burst to the player as ONE raw s16le@16k
// PCM block. Bursts end on Flush() OR on a gap longer than SinkGap - the
// child paces chunks continuously while speaking and the barge-in clear
// opens a real gap, so the gap heuristic bounds a burst to one spoken
// sentence-group. Clear() drops the pending burst (barge-in: never play
// the interrupted tail) AND silences the burst already handed to the
// player (leaf 01, recallable playback: tts.AudioPlayer retains the live
// oto player and its Stop pauses it, so the old tail stops within ~20 ms
// instead of playing out under the new answer - the talk-over defect of
// transcript local-2d84c384fd93f447).
//
// WHY RAW PCM (not per-burst WAV headers): the players are raw-PCM sinks
// (oto v3 plays bytes at the context rate, parsing NO headers; the file
// player appends into a file that already carries the one canonical
// header written at open and patched at close). Prepending a 44-byte
// header per burst played those bytes as garbage samples between every
// burst - the live "jittery" noise - and corrupted --out files with
// nested RIFF headers (live defect 2026-11-09, whisper heard "(buzzing)").
// The pre-amendment per-read Flush in the CLI drain loops made EVERY
// 640-byte paced chunk its own header-wrapped burst (stride 684); the
// CLI loops no longer flush per read. File mode flushes only on the gap
// heuristic and at stream end (byte-compatible output); live mode bounds
// the not-yet-played backlog to ~150 ms and flushes at the bound (leaf
// 01, recallable playback - an unbounded backlog kept talking over the
// new answer after a barge-in).

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/tts"
)

// micSampleRate / micChannels are exported for CLI clients building
// WAV containers around the raw pair PCM (leaf 08's file mode).
const (
	MicSampleRate = 16000
	MicChannels   = 1
)

// RawMicCapture is the client mic source: raw s16le 16k mono on Read,
// from a file (--in mode: WAV header stripped) or the ffmpeg pipe
// (live mode). Exactly one of the two sources is armed at construction.
type RawMicCapture struct {
	mu       sync.Mutex
	file     io.Reader // file mode
	cmd      *exec.Cmd // live mode (darwin)
	pipe     io.Reader // live mode stdout
	cancel   context.CancelFunc
	closeErr error
}

// micSampleRate / micChannels are the leg's wire format (CL1/CL5:
// L16@16000 mono raw between the pair sockets).
const (
	micSampleRate = 16000
	micChannels   = 1
)

// NewRawMicCapture opens the mic source. An inPath arms file mode; an
// empty inPath arms live capture, which is darwin-only today - every
// other GOOS refuses with the named error (the spec pins the wording)
// pointing at the --in fallback. A populated inPath wins on any GOOS
// (file mode keeps CI and Linux users unblocked).
func NewRawMicCapture(inPath, _ string) (*RawMicCapture, error) {
	if inPath != "" {
		f, err := os.Open(inPath)
		if err != nil {
			return nil, fmt.Errorf("voice: client mic: %w", err)
		}
		// Strip the canonical 44-byte WAV header: the socket carries
		// raw s16le, the same bytes the child pipeline would read.
		// (Odd headers keep their first 44 bytes stripped too - the
		// CLI's own inputs are ffmpeg-canonical; a non-WAV file yields
		// garbage audio, not a crash, which is acceptable for a shim.)
		header := make([]byte, 44)
		if _, err := io.ReadFull(f, header); err != nil {
			f.Close()
			return nil, fmt.Errorf("voice: client mic: read wav header: %w", err)
		}
		return &RawMicCapture{file: f}, nil
	}
	return newRawMicCaptureCtx(context.Background(), "")
}

// newRawMicCaptureCtx arms the live ffmpeg pipe bound to ctx (cancel
// kills ffmpeg - the no-orphan guarantee the leaf checklist pins).
func newRawMicCaptureCtx(ctx context.Context, _ string) (*RawMicCapture, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("voice: mic capture unsupported on %s; use --in file mode", runtime.GOOS)
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, errors.New("voice: mic capture needs ffmpeg on PATH; use --in file mode")
	}
	// avfoundation audio-only default device -> raw s16le 16k mono on
	// stdout (the Recorder pattern, pipe-shaped for the live leg).
	cctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cctx, "ffmpeg",
		"-f", "avfoundation",
		"-i", ":0", // ":default" per leaf 01/08; ":0" is the canonical first-audio shape the Recorder pins
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", micSampleRate),
		"-ac", fmt.Sprintf("%d", micChannels),
		"-loglevel", "error",
		"-",
	)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("voice: mic capture: stdout pipe: %w", err)
	}
	cmd.Stderr = nil // loud ffmpeg logs stay out of the CLI's stdout
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("voice: mic capture: start ffmpeg: %w", err)
	}
	return &RawMicCapture{cmd: cmd, pipe: pipe, cancel: cancel}, nil
}

// Read implements io.Reader over the armed source.
func (c *RawMicCapture) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file != nil {
		return c.file.Read(p)
	}
	if c.pipe != nil {
		return c.pipe.Read(p)
	}
	return 0, io.EOF
}

// Close tears the source down: the ffmpeg process is killed via ctx
// cancel and waited (no orphan ffmpeg in the process table), file
// handles close. Safe to call multiple times.
func (c *RawMicCapture) Close() error {
	c.mu.Lock()
	if c.closeErr != nil || (c.file == nil && c.cmd == nil) {
		c.mu.Unlock()
		return c.closeErr
	}
	if c.cancel != nil {
		c.cancel()
		if c.cmd != nil {
			_ = c.cmd.Wait() //nolint:mutexio // mutex serializes capture lifecycle; Wait reaps under the owning lock
		}
	}
	// Hand the file off to be closed OUTSIDE the mutex (no I/O under lock);
	// nil it under the lock so a second Close() sees the handoff.
	f := c.file
	c.file = nil
	c.mu.Unlock()
	if cf, ok := f.(io.Closer); ok {
		err := cf.Close()
		c.mu.Lock()
		if c.closeErr == nil {
			c.closeErr = err
		}
		err = c.closeErr
		c.mu.Unlock()
		return err
	}
	return nil
}

// OtoPlayer adapts tts.AudioPlayer onto Player (the live speaker; leaf
// 08 spec: "plays through internal/tts's oto player"). Construction is
// lazy (the oto context opens on first Play), so a headless host can
// still run --in/--out file mode without touching this type.
type OtoPlayer struct {
	inner *tts.AudioPlayer
}

// NewOtoPlayer returns the oto-backed Player seam. The context opens at
// MicSampleRate (16 kHz): the local leg's pair sockets carry raw L16
// s16le@16k and oto v3 plays bytes at the context rate with no WAV
// parsing and no resampling - the old hardcoded 22050 Hz context played
// every burst ~38% fast (chipmunk pitch, unintelligible to ears and to
// whisper-class STT).
func NewOtoPlayer() *OtoPlayer {
	return &OtoPlayer{inner: tts.NewAudioPlayer(tts.PlaybackConfig{SampleRate: MicSampleRate})}
}

// Play hands one burst to the oto player. The previous burst, if still
// playing, is paused by the retained player (leaf 01: one live burst at
// a time - the old tail never plays out under the new burst).
func (p *OtoPlayer) Play(burst []byte) error { return p.inner.Play(burst) }

// Clear silences the current burst: tts.AudioPlayer.Stop pauses the live
// oto player (leaf 01, recallable playback). Idempotent; safe before any
// Play (no retained burst = no-op).
func (p *OtoPlayer) Clear() { _ = p.inner.Stop() }

// Close suspends the audio driver.
func (p *OtoPlayer) Close() { _ = p.inner.Close() }

// Player is the speaker seam (leaf 08: tts.AudioPlayer satisfies it;
// tests inject fakes). Clear drops any pending burst and silences the
// one playing; Close releases the device.
type Player interface {
	Play(burst []byte) error
	Clear()
	Close()
}

// SinkGap separates two playback bursts: the child's paced PCM is
// continuous while speaking; a silence gap this long (a sentence break
// or a barge-in clear) ends the burst.
const SinkGap = 350 * time.Millisecond

// SpeakerSink turns the .out PCM stream into per-burst RAW PCM blocks for
// the player (no per-burst WAV header: see the amended burst contract
// above - oto parses no headers and the --out file carries the one
// canonical header written at open). NOT concurrency-safe beyond
// Close/Clear (the CLI drains the out socket from one goroutine).
type SpeakerSink struct {
	player    Player
	gap       time.Duration
	buf       bytes.Buffer
	lastWrite time.Time
}

// NewSpeakerSink constructs the sink. A nil player is valid: the sink
// discards (headless smoke tests).
func NewSpeakerSink(player Player, gap time.Duration) *SpeakerSink {
	if gap <= 0 {
		gap = SinkGap
	}
	return &SpeakerSink{player: player, gap: gap}
}

// Write buffers PCM. A gap longer than the sink's gap since the last
// write implicitly closes the previous burst (Play it) before the new
// bytes join the next one.
func (s *SpeakerSink) Write(pcm []byte) (int, error) {
	if s.buf.Len() > 0 && !s.lastWrite.IsZero() && time.Since(s.lastWrite) > s.gap {
		if err := s.Flush(); err != nil {
			return 0, err
		}
	}
	s.buf.Write(pcm) //nolint:errcheck // bytes.Buffer writes never fail
	s.lastWrite = time.Now()
	return len(pcm), nil
}

// Flush hands the pending burst to the player as one RAW PCM block (the
// burst contract above: no per-burst header). Empty buffer = no-op.
func (s *SpeakerSink) Flush() error {
	if s.buf.Len() == 0 {
		return nil
	}
	burst := s.buf.Bytes()
	s.buf.Reset()
	if s.player == nil {
		return nil
	}
	return s.player.Play(burst)
}

// Pending reports the bytes buffered since the last burst handoff - the
// not-yet-played backlog. Live-mode readers use it to bound their
// lookahead (leaf 01: cmd/meept/voice.go drainLiveSpeaker never lets the
// sink hold a whole utterance, so a barge-in tail is at most ~150 ms of
// queued audio instead of seconds).
func (s *SpeakerSink) Pending() int { return s.buf.Len() }

// Clear drops the pending burst and silences the burst already handed
// to the player (leaf 01: the oto Player's Clear pauses the live player,
// so neither the queued tail nor the playing head survives a barge-in).
func (s *SpeakerSink) Clear() {
	s.buf.Reset()
	s.lastWrite = time.Time{}
	if s.player != nil {
		s.player.Clear()
	}
}

// Close flushes any last burst then releases the player.
func (s *SpeakerSink) Close() error {
	err := s.Flush()
	if s.player != nil {
		s.player.Close()
	}
	return err
}

// NewWAVHeader builds the canonical 44-byte PCM WAV header for
// s16le at the given rate/channels and data size. The local leg's
// bursts (and the CLI's --in stripping) are the only consumers; the
// child pipeline never sees a header (the pair carries raw PCM).
func NewWAVHeader(sampleRate, channels, dataLen int) []byte {
	const byteRateBase = 2 // s16le = 2 bytes/sample
	h := make([]byte, 44)
	byteRate := sampleRate * channels * byteRateBase
	blockAlign := channels * byteRateBase
	copy(h[0:4], "RIFF")
	binary.LittleEndian.PutUint32(h[4:8], uint32(36+dataLen)) //nolint:gosec // G115: bounded WAV RIFF size field
	copy(h[8:12], "WAVE")
	copy(h[12:16], "fmt ")
	binary.LittleEndian.PutUint32(h[16:20], 16)                 // fmt chunk size
	binary.LittleEndian.PutUint16(h[20:22], 1)                  // PCM
	binary.LittleEndian.PutUint16(h[22:24], uint16(channels))   //nolint:gosec // channel counts are tiny
	binary.LittleEndian.PutUint32(h[24:28], uint32(sampleRate)) //nolint:gosec // rates are tiny
	binary.LittleEndian.PutUint32(h[28:32], uint32(byteRate))   //nolint:gosec // derived from tiny constants
	binary.LittleEndian.PutUint16(h[32:34], uint16(blockAlign)) //nolint:gosec // tiny
	binary.LittleEndian.PutUint16(h[34:36], 16)                 // bits per sample
	copy(h[36:40], "data")
	binary.LittleEndian.PutUint32(h[40:44], uint32(dataLen)) //nolint:gosec // caller-truncated
	return h
}
