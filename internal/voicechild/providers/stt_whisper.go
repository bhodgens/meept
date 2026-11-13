package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
)

// defaultWhisperBin is the whisper.cpp CLI binary name. whisper.cpp is MIT
// and runs as a subprocess only - never vendored or linked (leaf 10 rule).
const defaultWhisperBin = "whisper-cli"

// Segmentation constants (leaf 10 contract): trailing-silence energy
// gating closes an utterance; the buffered window is hard-capped so a
// never-silent source cannot grow memory unbounded.
const (
	// whisperSilenceRMS is the RMS cutoff (normalized to int16 full
	// scale) below which a chunk counts as silence.
	whisperSilenceRMS = 0.001
	// whisperSilenceMs closes an utterance after this much continuous
	// trailing silence (~500 ms of no speech).
	whisperSilenceMs = 500
	// whisperWallIdleMs is the WALL-CLOCK twin of whisperSilenceMs: an
	// open voiced window that receives NO audio at all for this long is
	// also closed. The pump-fed paths (voice link bridge, live mic)
	// simply stop forwarding bytes when the speaker goes quiet — no
	// silence chunks are injected — so an audio-ms-only rule can never
	// accumulate 500 ms of trailing silence and the utterance hangs
	// until teardown cancels the (never-started) recognition (link
	// regression 2026-11-10: 0/0 turns, side-A greeting never
	// dispatched). Fast producers (file-mode copyMicFile) always have
	// >=1 chunk in flight per 20 ms of wall clock, so this timer only
	// fires after a REAL pause, adding no extra latency there.
	whisperWallIdleMs = 500
	// whisperMaxWindowS caps one buffered utterance at N seconds of
	// audio regardless of silence; on overflow the window flushes.
	whisperMaxWindowS = 10
	// whisperStderrTailBytes caps the stderr tail carried into errors.
	whisperStderrTailBytes = 200
)

// whisperRecognizeTimeout bounds ONE whisper-cli segment run.
//
// ctx at the call site is the Stream context = the pipeline context = the
// whole CALL lifetime. Handing that to exec.CommandContext means a wedged
// whisper-cli blocks the transcript consumer forever while forwardPCM
// silently drops the caller's speech (defect 5). This per-segment bound
// turns a wedge into a segment error: recognize() logs it and ends the
// stream, so the child stays responsive.
//
// The segment WAV is at most whisperMaxWindowS (10 s) of audio; 15 s leaves
// ~1.5x headroom over real time for a cold subprocess, and a slow model is
// still bounded instead of unbounded.
const whisperRecognizeTimeout = 15 * time.Second

// whisperWallIdle is the duration form of whisperWallIdleMs. A package
// var so unit tests can shrink the wall-idle wait; production uses the
// constant.
var whisperWallIdle = time.Duration(whisperWallIdleMs) * time.Millisecond

// ErrWhisperBinMissing is a constructor error: the whisper-cli binary is
// not in PATH. It fails ONLY that call; the child and other calls survive.
var ErrWhisperBinMissing = errors.New("voicechild/providers: whisper-cli binary not found (install whisper-cpp or set PATH)")

// WhisperSTT turns the C2 PCM chunk stream into final Transcripts by
// segmenting on trailing silence (energy gating), writing each utterance
// as a WAV, and running the whisper.cpp CLI as a subprocess (voicechild
// .STTProvider). It emits exactly one FINAL Transcript per utterance and
// NEVER a partial: the local leg's barge-in rides RMS on the playback
// path (master.md CL3), so no mid-sentence text is consumed.
//
// Transcript source: whisper-cli stdout with -nt (no timestamps). The fake
// binary in tests defines the same contract: plain text on stdout.
type WhisperSTT struct {
	cfg ProviderConfig
	bin string // resolved at construction (test seam: PATH)

	// kickMu guards the per-session flush-kick channels registered by
	// Stream and retired by run. BARGE-IN CONTRACT (2026-11-11): FlushNow
	// force-recognizes every session's OPEN voiced window immediately so
	// the whisper C5 final lands ~350 ms (one cold subprocess) sooner on
	// the pure-whisper barge-in path. The signal is a non-blocking send
	// on a buffered(1) channel: the run loop never takes kickMu while
	// handling a kick, and a second pending kick coalesces into the
	// first (one flush already covers everything buffered so far).
	kickMu sync.Mutex
	kicks  map[chan struct{}]struct{}
}

// FlushNow force-closes the open voiced window of every live Stream
// session of this adapter (early-kick for barge-in latency).
//
// BARGE-IN CONTRACT (2026-11-11): FlushNow must NEVER fire recognition
// on an empty (or un-voiced) window — the run loop's kick case checks
// voicedSamples > 0 and no-ops otherwise. It never injects silence and
// never splits an utterance that has no speech in it: with nothing
// buffered the kick is a no-op, so behavior is byte-identical when the
// pipeline calls it speculatively on every sustained-energy window.
func (s *WhisperSTT) FlushNow() {
	s.kickMu.Lock()
	for ch := range s.kicks {
		select {
		case ch <- struct{}{}:
		default: // a kick already pending: coalesce
		}
	}
	s.kickMu.Unlock()
}

// registerKick adds one session's flush-kick channel and returns its
// retire closure (deferred by run).
func (s *WhisperSTT) registerKick() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.kickMu.Lock()
	if s.kicks == nil {
		s.kicks = make(map[chan struct{}]struct{})
	}
	s.kicks[ch] = struct{}{}
	s.kickMu.Unlock()
	return ch, func() {
		s.kickMu.Lock()
		delete(s.kicks, ch)
		s.kickMu.Unlock()
	}
}

// newWhisperSTT builds the local STT adapter. No key is needed - this
// vendor never touches the network. Empty Model is ErrLocalModelRequired;
// a missing binary is ErrWhisperBinMissing and fails only that call.
// SampleRate defaults to 16 kHz (the C2 local-leg rate) when unset.
func newWhisperSTT(cfg ProviderConfig) (voicechild.STTProvider, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("%w: stt_model", ErrLocalModelRequired)
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 16000
	}
	bin, err := exec.LookPath(defaultWhisperBin)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrWhisperBinMissing, defaultWhisperBin, err)
	}
	return &WhisperSTT{cfg: cfg, bin: bin}, nil
}

// Stream implements voicechild.STTProvider. The returned channel carries
// one Final Transcript per recognized utterance and closes when pcm
// closes (with a last flush), the context ends, or a whisper failure ends
// the stream (a subprocess death that leaves no recoverable segment is a
// stream-ending error, mirroring the Deepgram adapter's policy).
func (s *WhisperSTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan voicechild.Transcript, error) {
	dir, err := os.MkdirTemp("", "whisper-stt-")
	if err != nil {
		return nil, fmt.Errorf("whisper temp dir: %w", err)
	}
	out := make(chan voicechild.Transcript, 8)
	kick, retire := s.registerKick()
	go s.run(ctx, pcm, out, dir, kick, retire)
	return out, nil
}

// run is the segmentation loop. Trailing silence is measured in AUDIO
// ms (chunk sample counts) so a fast producer flushes on the same audio
// boundary a slow one does; a producer that STOPS sending bytes at all
// (the pump bridge's silence is an absence of chunks) is caught by the
// whisperWallIdle wall-clock timer instead — an utterance flushes when
// either kind of pause closes it. A third close trigger is the barge-in
// flush kick (kick channel, see FlushNow): the pipeline asks for the
// open voiced window NOW so the C5 final arrives ~350 ms sooner.
func (s *WhisperSTT) run(ctx context.Context, pcm <-chan []byte, out chan<- voicechild.Transcript, dir string, kick <-chan struct{}, retire func()) {
	defer close(out)
	defer retire()
	defer func() {
		if rerr := os.RemoveAll(dir); rerr != nil {
			slog.Error("whisper temp cleanup failed", "dir", dir, "error", rerr)
		}
	}()

	var (
		buf            []byte // raw accumulated s16le bytes of the open utterance
		silenceSamples int    // trailing silent samples in the open window
		voicedSamples  int    // samples above threshold in the open window
		voicedEnd      int    // byte offset just past the last voiced chunk
		maxWindow      int
		silenceCutoff  int
		uttSeq         int
		gate           rmsGate // per-stream odd-boundary carry for RMS math
	)
	maxWindow = whisperMaxWindowS * s.cfg.SampleRate * 2
	silenceCutoff = s.cfg.SampleRate * whisperSilenceMs / 1000

	// recognize flushes the open window to the CLI and delivers the
	// transcript. Only the voiced span ships: the trailing silence that
	// triggered the flush stays out of the WAV. It reports false only
	// when the stream must end.
	recognize := func() bool {
		data := buf
		if voicedEnd < len(data) {
			data = data[:voicedEnd]
		}
		if voicedSamples == 0 || len(data) < 2 {
			buf, silenceSamples, voicedSamples, voicedEnd = nil, 0, 0, 0
			return true
		}
		text, rerr := s.recognizeOne(ctx, dir, uttSeq, data)
		uttSeq++
		buf, silenceSamples, voicedSamples, voicedEnd = nil, 0, 0, 0
		if rerr != nil {
			// A dead CLI leaves no recoverable segment: end the stream
			// with the error logged (vendor+model only, never content).
			slog.Error("whisper segment recognition failed", "model", s.cfg.Model, "error", rerr)
			return false
		}
		if text == "" {
			return true
		}
		select {
		case out <- voicechild.Transcript{Text: text, Final: true}:
			return true
		case <-ctx.Done():
			return false
		}
	}

	// Wall-idle timer (see whisperWallIdleMs): closes an OPEN voiced
	// window when the producer stops sending bytes altogether (the pump
	// bridge's silence is an ABSENCE of chunks, not zero-amplitude
	// chunks, so the audio-ms rule above can never see it). Armed on
	// every chunk while a voiced window is open, disarmed whenever the
	// window closes. One reusable timer; the stop/drain dance keeps a
	// stale fire out of the channel so idleC only ever carries a REAL
	// expiry.
	idleTimer := time.NewTimer(whisperWallIdle)
	if !idleTimer.Stop() {
		<-idleTimer.C
	}
	var idleC <-chan time.Time
	armIdle := func() {
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(whisperWallIdle)
		idleC = idleTimer.C
	}
	disarmIdle := func() {
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleC = nil
	}
	defer idleTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-kick:
			// BARGE-IN CONTRACT (2026-11-11): early flush kick from the
			// pipeline (a sustained-energy window is attempting a trip).
			// Flush ONLY the OPEN voiced window: with nothing voiced
			// buffered this is a strict no-op — never inject silence,
			// never launch a subprocess, no behavior change when the
			// buffer is empty (recognize's voicedSamples==0 guard
			// already handles the empty case, and we skip it entirely
			// so even the reset is a no-op).
			if voicedSamples > 0 {
				disarmIdle()
				if !recognize() {
					return
				}
			}
		case <-idleC:
			// Audio stopped arriving with a voiced window open: the
			// speaker finished (or paused); close the utterance on the
			// wall clock exactly as trailing silence would.
			disarmIdle()
			if !recognize() {
				return
			}
		case chunk, ok := <-pcm:
			if !ok {
				recognize() // flush the tail if anything voiced is open
				return
			}
			if len(chunk) == 0 {
				continue
			}
			buf = append(buf, chunk...)
			n, voicedNow := gate.energy(chunk)
			if voicedNow {
				voicedSamples += n
				silenceSamples = 0
				voicedEnd = len(buf)
			} else {
				silenceSamples += n
			}
			// Any byte arriving while a window is open proves the
			// producer is still streaming: re-arm the wall-idle timer
			// so it can only fire on a REAL gap in delivery.
			if voicedSamples > 0 {
				armIdle()
			}
			if silenceSamples >= silenceCutoff || len(buf) >= maxWindow {
				disarmIdle()
				if !recognize() {
					return
				}
			}
		}
	}
}

// rmsGate tracks the odd-length-chunk carry byte so the boundary sample
// between two chunks is never dropped from the RMS math. One gate per
// Stream, because a WhisperSTT adapter serves concurrent Streams.
type rmsGate struct {
	carry     byte
	haveCarry bool
}

// energy returns the chunk's completed-sample count and whether its RMS
// exceeds the silence threshold. sampleRate is the stream's rate.
func (g *rmsGate) energy(chunk []byte) (samples int, voiced bool) {
	var sumSq uint64
	n := 0
	start := 0
	if g.haveCarry {
		// Carry (low byte of the dangling sample) + this chunk's first
		// byte (its high byte) complete one sample.
		sample := int16(uint16(chunk[0])<<8 | uint16(g.carry)) //nolint:gosec // G115: PCM s16le byte reassembly
		sumSq += uint64(int32(sample) * int32(sample))         //nolint:gosec // G115: 16-bit sample squared, fits int64
		n++
		g.haveCarry = false
		start = 1 // bytes 1.. now hold whole samples only
	}
	for i := start + 1; i < len(chunk); i += 2 {
		sample := int16(uint16(chunk[i])<<8 | uint16(chunk[i-1])) //nolint:gosec // G115: PCM s16le byte reassembly
		sumSq += uint64(int32(sample) * int32(sample))            //nolint:gosec // G115: 16-bit sample squared, fits int64
		n++
	}
	if (len(chunk)-start)%2 == 1 {
		g.carry = chunk[len(chunk)-1]
		g.haveCarry = true
	}
	if n == 0 {
		return 0, false // chunk was only a dangling byte; nothing to gate on
	}
	rms := math.Sqrt(float64(sumSq)/float64(n)) / math.MaxInt16
	return n, rms > whisperSilenceRMS
}

// recognizeOne writes buf as a WAV under dir, runs the CLI on it, and
// returns the trimmed transcript. The WAV is written with a hand-rolled
// RIFF/WAVE header (stdlib has no WAV encoder and no new dependency is
// allowed for one 44-byte header).
func (s *WhisperSTT) recognizeOne(ctx context.Context, dir string, seq int, buf []byte) (string, error) {
	wavPath := filepath.Join(dir, fmt.Sprintf("utt%d.wav", seq))
	if err := writeWAV16(wavPath, buf, s.cfg.SampleRate); err != nil {
		return "", fmt.Errorf("write wav: %w", err)
	}
	// Per-segment bound (whisperRecognizeTimeout): the caller's ctx is the
	// whole call lifetime, so a wedged CLI would otherwise block this
	// consumer forever and starve the transcript stream.
	segCtx, cancel := context.WithTimeout(ctx, whisperRecognizeTimeout)
	defer cancel()
	// -m model, -f wav, -nt = no timestamps (plain text on stdout).
	cmd := exec.CommandContext(segCtx, s.bin, "-m", s.cfg.Model, "-f", wavPath, "-nt")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		if errors.Is(segCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("whisper-cli on segment: timed out after %s: %w", whisperRecognizeTimeout, segCtx.Err())
		}
		return "", fmt.Errorf("whisper-cli on segment: %w (stderr tail: %s)", err, tailBytes(stderr.Bytes(), whisperStderrTailBytes))
	}
	return strings.TrimSpace(string(stdout)), nil
}

// writeWAV16 writes buf (truncated to an even length) as mono 16-bit PCM
// WAV at the given sample rate.
func writeWAV16(path string, buf []byte, sampleRate int) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create wav: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	data := buf
	if len(data)%2 == 1 {
		data = data[:len(data)-1] // drop the dangling byte for sample alignment
	}
	const headerBytes = 36
	var hdr [44]byte
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(headerBytes+len(data))) //nolint:gosec // G115: WAV size lives well below 2^32
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint16(hdr[16:20], 16)                   // fmt chunk size
	binary.LittleEndian.PutUint16(hdr[20:22], 1)                    // PCM
	binary.LittleEndian.PutUint16(hdr[22:24], 1)                    // mono
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(sampleRate))   //nolint:gosec // G115: sample rate is a small config int
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(sampleRate*2)) //nolint:gosec // G115: byte rate, always within uint32
	binary.LittleEndian.PutUint16(hdr[32:34], 2)                    // block align
	binary.LittleEndian.PutUint16(hdr[34:36], 16)                   // bits
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], uint32(len(data))) //nolint:gosec // G115: WAV data size lives well below 2^32
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("wav header: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("wav data: %w", err)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("wav flush: %w", err)
	}
	return nil
}
