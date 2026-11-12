package main

// voice.go - the `meept voice` CLI (local-leg leaf 08, T4-T6):
//
//   - voice local --employee X [--label L | --in f | --out g]: opens a
//     local call over RPC, dials BOTH pair sockets, and shuffles audio
//     (file mode: pure io.Copy both directions, exits cleanly when the
//     call ends; live mode: ffmpeg mic + oto speaker).
//   - voice local --list: lists employees (convenience).
//   - voice status: one table, both legs (phone rows + local rows).
//
// Graceful exit: SIGINT ends the call via voice.local.end before the
// process leaves, so the daemon's release fan-out runs exactly once and
// the leg never waits out its reaper timers.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/caimlas/meept/internal/voice"
	"github.com/spf13/cobra"
)

func newVoiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "voice",
		Short: "voice calls: local leg status and live local calls",
	}
	cmd.AddCommand(newVoiceLocalCmd())
	cmd.AddCommand(newVoiceLinkCmd())
	cmd.AddCommand(newVoiceStatusCmd())
	return cmd
}

// ---------------------------------------------------------------------------
// meept voice local
// ---------------------------------------------------------------------------

func newVoiceLocalCmd() *cobra.Command {
	f := localFlags{}

	cmd := &cobra.Command{
		Use:   "local",
		Short: "open a local voice call (mic/speaker, or --in/--out files)",
		Long: `Hold a voice conversation with an employee on this machine -
no carrier, no cloud. With --in/--out the call reads mic input from a
WAV file and writes the reply to a WAV file; without them the mic is
captured via ffmpeg and the reply plays through the speaker.

--record DIR (live mode only) tees EXACTLY the bytes handed to your
own speaker playback into DIR/<call-id>.raw (raw s16le mono 16 kHz)
plus DIR/<call-id>-meta.json, so head truncation and jitter on the
human leg are measurable with scripts/voice-gap-analyze.py. The
recorder taps the oto playback path, which file mode never opens, so
--record with --in/--out is refused.

Examples:
  meept voice local --list                        # employees you can call
  meept voice local --employee eve --in q.wav --out a.wav
  meept voice local --employee eve --label desk   # live mic + speaker
  meept voice local --employee eve --record /tmp/local-rec   # + human-leg evidence`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := f.validate(); err != nil {
				return err
			}
			if f.list {
				return runVoiceLocalList()
			}
			return runVoiceLocal(f.employee, f.label, f.inPath, f.outPath, f.record)
		},
	}

	cmd.Flags().StringVar(&f.employee, "employee", "", "employee id to call (required unless --list)")
	cmd.Flags().StringVar(&f.label, "label", "", "session label (default: hostname)")
	cmd.Flags().StringVar(&f.inPath, "in", "", "read mic input from this WAV file (file mode)")
	cmd.Flags().StringVar(&f.outPath, "out", "", "write the reply to this WAV file (file mode)")
	cmd.Flags().BoolVar(&f.list, "list", false, "list employees available for local calls")
	cmd.Flags().StringVar(&f.record, "record", "", "live mode only: record the exact bytes handed to speaker playback into DIR (raw s16le mono 16 kHz + meta JSON)")
	return cmd
}

// localFlags is the validated `voice local` flag set (pure validation +
// the --record directory side effect, unit-tested without a daemon —
// same shape as linkFlags).
type localFlags struct {
	employee string
	label    string
	inPath   string
	outPath  string
	list     bool
	record   string
}

// validate reports the flag errors the leaf pins: --employee required
// (unless --list), and --record refused in file mode (--in/--out): the
// recorder taps the oto playback seam (recorderPlayer wraps the live
// speaker's player) — file mode is pure io.Copy into a WAV and never
// constructs a player, so there are no playback bytes to tee. When
// --record is set the DIR is created if it does not exist (MkdirAll is
// idempotent, mirroring linkFlags.validate).
func (f localFlags) validate() error {
	if !f.list && f.employee == "" {
		return fmt.Errorf("--employee is required (or use --list)")
	}
	if f.record != "" {
		if f.inPath != "" || f.outPath != "" {
			return fmt.Errorf("--record requires live mode (no --in/--out): the recorder taps the oto playback path, which file mode never opens")
		}
		if err := os.MkdirAll(f.record, 0o755); err != nil {
			return fmt.Errorf("--record: cannot create directory %q: %w", f.record, err)
		}
	}
	return nil
}

// runVoiceLocalList lists employees via agents.list (the same surface
// the agents CLI uses) so `meept voice local --list` needs no new RPC.
func runVoiceLocalList() error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	raw, err := client.Call("agents.list", map[string]any{})
	if err != nil {
		return fmt.Errorf("failed to list employees: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	if errMsg := rpcError(m); errMsg != "" {
		return fmt.Errorf("%s", errMsg)
	}
	agents, _ := m["agents"].([]any)
	if len(agents) == 0 {
		fmt.Println("no employees found.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tENABLED")
	for _, a := range agents {
		agent, ok := a.(map[string]any)
		if !ok {
			continue
		}
		enabled := "false"
		if b, ok := agent["enabled"].(bool); ok && b {
			enabled = "true"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			getStringOr(agent, "id", ""),
			getStringOr(agent, "name", ""),
			enabled)
	}
	return w.Flush()
}

// voiceLocalOpenResult mirrors the voice.local.open wire shape (CL8).
type voiceLocalOpenResult struct {
	CallID    string `json:"call_id"`
	InPath    string `json:"in_path"`
	OutPath   string `json:"out_path"`
	SessionID string `json:"session_id"`
}

// runVoiceLocal opens the call, wires the audio paths, and blocks until
// the call ends (daemon hangup, SIGINT, or file exhaustion). recordDir
// ("" = off) is live-mode only — localFlags.validate refuses --record
// with --in/--out — and tees the operator's own speaker playback into
// DIR/<call-id>.raw for gap analysis (leaf 02, C2).
func runVoiceLocal(employee, label, inPath, outPath, recordDir string) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	req := map[string]any{"employee_id": employee}
	if label != "" {
		req["label"] = label
	}
	raw, err := client.Call("voice.local.open", req)
	if err != nil {
		return fmt.Errorf("voice.local.open: %w", err)
	}
	var open voiceLocalOpenResult
	if err := json.Unmarshal(raw, &open); err != nil {
		return fmt.Errorf("failed to parse open response: %w", err)
	}
	if open.CallID == "" {
		return fmt.Errorf("daemon refused the call (no call id returned)")
	}
	fmt.Printf("call %s open: mic %s / speaker %s\n", open.CallID, open.InPath, open.OutPath)

	// Operator telemetry: live event lines (heard / thinking / replied /
	// barge-in / ended) from the daemon's per-call ring. Best-effort: the
	// poller never fails the call.
	teleCtx, teleCancel := context.WithCancel(context.Background())
	defer teleCancel()
	events := newVoiceEventTracker(client, map[string][2]string{
		open.CallID: {"local", employee},
	})
	events.start(teleCtx)

	// SIGINT -> voice.local.end (graceful: the daemon's release fan-out
	// runs before we exit; no double teardown because End is idempotent).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			fmt.Println("\nsignal received; ending call...")
			_, _ = client.Call("voice.local.end", map[string]any{"call_id": open.CallID})
			cancel()
		case <-ctx.Done():
		}
	}()

	if inPath != "" || outPath != "" {
		err = runVoiceLocalFileMode(ctx, client, open, inPath, outPath)
	} else {
		err = runVoiceLocalLive(ctx, open, employee, recordDir)
	}

	// The call ends here no matter how the loop exited (the daemon may
	// have reaped it already; End is idempotent so this is safe).
	if _, endErr := client.Call("voice.local.end", map[string]any{"call_id": open.CallID}); endErr != nil && err == nil {
		err = fmt.Errorf("voice.local.end: %w", endErr)
	}

	// Flush the telemetry tail after the end call (the ring's call_ended
	// lands during teardown): stop the live poller, print whatever is new.
	teleCancel()
	events.stop()
	events.drainOnce()

	// The user explicitly asked for the transcript back (event-feed
	// regression follow-up, call local-53b347f028f0a43f): the live feed
	// shows WHEN things happened; this shows WHAT was said. Best-effort
	// like the poller - a failed/absent read never changes the run's
	// exit (the call already succeeded or failed on its own terms).
	// Source is voice.local.transcript (the call store, call-scoped —
	// NOT session.messages.get: without fresh_session_per_call the
	// session is shared across calls and would print prior calls too).
	printLocalCallTranscript(client, open.CallID, employee)
	if err == nil {
		fmt.Println("call ended.")
	}
	return err
}

// runVoiceLocalFileMode is the deterministic CI-safe path: pure io.Copy
// both directions over the two pair sockets. The mic file streams until
// exhausted (EOF -> half-close the in socket so the child sees the end
// of speech), and the speaker file collects whatever the child sends
// back. Exit: when the daemon ends the call both socket reads fail and
// the loop returns cleanly.
func runVoiceLocalFileMode(ctx context.Context, client interface {
	Call(method string, params any) (json.RawMessage, error)
}, open voiceLocalOpenResult, inPath, outPath string) error {
	// Dial BOTH pair sockets up front (live mode's ordering): the
	// daemon's splice ends with a named error the moment the child's
	// first outbound audio lands while the speaker accept is pending,
	// so a mic-first sequential dial deadlocks file mode.
	if inPath != "" {
		// Keep the mic conn open across the whole call below.
	} else {
		// Without --in the child gets no mic audio; dial and close to
		// mark the pair joined so the splice's accept completes.
		if conn, err := net.Dial("unix", open.InPath); err == nil {
			_ = conn.Close()
		}
	}

	micDone := make(chan error, 1)
	spkDone := make(chan error, 1)
	if inPath != "" {
		go func() { micDone <- copyMicFile(ctx, open.InPath, inPath) }()
	}
	go func() { spkDone <- drainSpeakerFile(ctx, open.OutPath, outPath) }()

	// Phase 1: the mic file drains (utterance delivered) - then hold the
	// call open so whisper STT + the employee turn + piper TTS can run
	// and the reply reaches the speaker file. Phase 2: the speaker
	// stream closes (the daemon ended the call - silence timeout or a
	// hangup), which is file mode's real end. A ctx cancel cuts through
	// both.
	if inPath != "" {
		select {
		case <-micDone:
		case <-spkDone:
		case <-ctx.Done():
		}
	}
	select {
	case <-spkDone:
	case <-ctx.Done():
	}

	// File mode has no user "hangup" gesture: once the mic file is
	// drained and the speaker stream went quiet, end the call.
	if _, err := client.Call("voice.local.end", map[string]any{"call_id": open.CallID}); err != nil {
		// The daemon may have ended it first; a not-found / unknown
		// refusal is the idempotent shape, keep it quiet.
		return nil //nolint:nilerr // intentional: end-of-call is idempotent, a refusal is not a failure
	}
	return nil
}

// copyMicFile streams one WAV file's raw PCM into the pair's .in socket.
func copyMicFile(ctx context.Context, sockPath, wavPath string) error {
	cap, err := voice.NewRawMicCapture(wavPath, "")
	if err != nil {
		return err
	}
	defer cap.Close() //nolint:errcheck // file source; Close errors never mask the copy
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return fmt.Errorf("dial mic socket %s: %w", sockPath, err)
	}
	defer conn.Close() //nolint:errcheck // teardown; the daemon ends the call regardless
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(conn, cap)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("stream mic: %w", err)
		}
		// End-of-speech signal: half-close the mic direction so the
		// child's audio pump sees EOF, closes the PCM channel, and the
		// whisper adapter flushes the buffered utterance. Without this
		// the pump blocks on Read forever (file mode has no more mic
		// chunks), no silence ever accumulates, and the utterance is
		// never recognized. The conn itself stays open (defer Close).
		if uc, ok := conn.(*net.UnixConn); ok {
			if hwErr := uc.CloseWrite(); hwErr != nil {
				return fmt.Errorf("half-close mic: %w", hwErr)
			}
		}
		return nil
	case <-ctx.Done():
		return nil // signal end: caller sends voice.local.end
	}
}

// drainSpeakerFile reads the .out socket until the daemon closes it
// (call end), writing PCM to outPath as a canonical WAV (or discarding
// when outPath is empty).
func drainSpeakerFile(ctx context.Context, sockPath, outPath string) error {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return fmt.Errorf("dial speaker socket %s: %w", sockPath, err)
	}
	defer conn.Close() //nolint:errcheck // teardown

	var sink *voice.SpeakerSink
	var wavFile io.WriteCloser
	if outPath != "" {
		f, ferr := os.Create(outPath)
		if ferr != nil {
			return fmt.Errorf("create out file: %w", ferr)
		}
		wavFile = f
		// Reserve the 44-byte header; patch the data length at the end.
		if _, ferr := f.Write(voice.NewWAVHeader(voice.MicSampleRate, voice.MicChannels, 0)); ferr != nil {
			f.Close()
			return fmt.Errorf("write out header: %w", ferr)
		}
		player := &wavFilePlayer{w: f}
		// gap 0 = default SinkGap: bursts close on real gaps (sentence
		// breaks / barge-in) and at Flush/Close; the raw PCM appends into
		// the single-header file (no per-burst header since 2026-11-09).
		sink = voice.NewSpeakerSink(player, 0)
	}
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 8*1024)
		for {
			n, rerr := conn.Read(buf)
			if n > 0 {
				// With a nil sink and nil wavFile there is nowhere for the bytes to
				// go (connectivity probe with --out unset): the discard path drops
				// them here by design.
				if sink != nil {
					if _, werr := sink.Write(buf[:n]); werr != nil {
						done <- werr
						return
					}
				}
			}
			if rerr != nil {
				// EOF / connection closed = daemon ended the call: normal.
				// Flush the pending burst BEFORE reporting the end: the
				// per-read flush is gone (it wrapped every paced chunk in
				// its own WAV header - live defect 2026-11-09), so the
				// final burst closes only here, on the gap heuristic at
				// the next Write, or at sink Close below.
				done <- nil
				return
			}
		}
	}()
	select {
	case err := <-done:
		if wavFile != nil && sink != nil {
			// Close flushes the final pending burst into the file BEFORE
			// the data-length patch: without it the tail burst after the
			// last gap heuristic close is dropped (and the header lies).
			if cerr := sink.Close(); cerr != nil && err == nil {
				err = cerr
			}
			patchWAVDataLen(wavFile)
			wavFile.Close() //nolint:errcheck // flushed above
		}
		return err
	case <-ctx.Done():
		if wavFile != nil && sink != nil {
			_ = sink.Close() //nolint:errcheck // signal path: keep what arrived
			patchWAVDataLen(wavFile)
			wavFile.Close() //nolint:errcheck // signal path
		}
		return nil
	}
}

// wavFilePlayer adapts an io.Writer onto voice.Player for file mode:
// Play appends the burst payload (RAW PCM since the 2026-11-09 contract
// amendment; the file's one canonical header is written at open and
// patched at close).
type wavFilePlayer struct {
	w io.Writer
}

func (p *wavFilePlayer) Play(burst []byte) error {
	_, err := p.w.Write(burst)
	return err
}
func (p *wavFilePlayer) Clear() {}
func (p *wavFilePlayer) Close() {}

// patchWAVDataLen seeks back and writes the true data length into the
// reserved header (io.Seeker), then rewinds so Close flushes cleanly.
func patchWAVDataLen(f io.WriteCloser) {
	sk, ok := f.(io.Seeker)
	if !ok {
		return
	}
	cur, err := sk.Seek(0, io.SeekCurrent)
	if err != nil {
		return
	}
	dataLen := cur - 44
	if dataLen < 0 || dataLen > int64(^uint32(0)) {
		// The WAV size field is 32-bit; a stream longer than that can't be
		// header-addressed, so leave the reserved trailer bytes untouched.
		return
	}
	if _, err := sk.Seek(40, io.SeekStart); err != nil {
		return
	}
	sz := uint32(dataLen)
	var size [4]byte
	size[0] = byte(sz)                          //nolint:gosec // G115: dataLen bounds-checked to fit uint32 immediately above
	size[1] = byte(sz >> 8)                     //nolint:gosec // G115: dataLen bounds-checked to fit uint32 immediately above
	size[2] = byte(sz >> 16)                    //nolint:gosec // G115: dataLen bounds-checked to fit uint32 immediately above
	size[3] = byte(sz >> 24)                    //nolint:gosec // G115: dataLen bounds-checked to fit uint32 immediately above
	if _, err := f.Write(size[:]); err != nil { //nolint:errcheck // best-effort trailer patch
		return
	}
	_, _ = sk.Seek(cur, io.SeekStart)
}

// runVoiceLocalLive wires the ffmpeg mic + speaker sink over the pair
// sockets (T5). darwin-only live capture: other GOOS refuse with the
// named clientaudio error pointing at --in.

// ErrMicNoAudio names the live-capture death where the mic pipeline died
// before producing a single byte (broken ffmpeg install, denied mic
// permission, wrong device). The old behavior SILENTLY ended the call -
// the operator saw only "call ended." - so this failure condition is
// pinned by TestClassifyMicDeath: every zero-byte mic death MUST surface
// as this error, never as a clean return.
var ErrMicNoAudio = errors.New("live mic capture produced no audio (is ffmpeg working? try 'brew reinstall ffmpeg'; check mic permission for the terminal)")

// classifyMicDeath maps one finished mic stream onto the live-mode exit:
// ErrMicNoAudio when the capture died before producing audio (wrap it
// around the underlying read error so the cause is never lost), the read
// error itself when real audio flowed then failed, and nil for a clean
// EOF after audio (normal hangup path). micErr nil + noAudio means the
// capture ended cleanly without ever producing audio - still a failure:
// a live mic that produces nothing IS the failure condition.
func classifyMicDeath(micErr error, micBytes int64) error {
	if micBytes > 0 {
		return micErr
	}
	if micErr == nil {
		return fmt.Errorf("%w: capture ended cleanly with zero bytes", ErrMicNoAudio)
	}
	return fmt.Errorf("%w: %v", ErrMicNoAudio, micErr)
}

// liveMaxPendingAudioBytes bounds the not-yet-played backlog the CLI
// live loop keeps between the .out socket and the speakers (leaf 01,
// recallable playback). The old loop let the sink's gap heuristic buffer
// a whole utterance before the first handoff, so seconds of audio stayed
// queued behind a barge-in and talked over the new answer (live defect,
// transcript local-2d84c384fd93f447).
//
// The bound MUST stay comfortably above one oto burst's play time. The
// daemon paces 640 B per 20 ms — exactly real time — so a cap equal to
// the drain rate makes burst k finish at the same instant burst k+1 is
// flushed: zero mixer overlap, and every handoff pays the fresh-player
// start latency as an audible micro-gap every ~150 ms of speech
// ("words cut into segments", 2026-11-10; the MEEPT_VOICE_TRACE
// underrun counters proved it with early_overlaps=0). 375 ms = 3x the
// 150 ms burst drain keeps ~225 ms of predecessor audio live at every
// handoff, which is what makes successive bursts seamless in oto's
// mixer. Recallability is unchanged by the bound: Clear() drops the
// buffered burst and Stop() pauses every live player, so a barge-in
// still silences the speakers within ~20 ms; the bound only sizes the
// queued-but-unplayed tail (375 ms, versus the whole-utterance backlog
// that was the original defect).
//
// The recorded lanes (--out, link --record) are structurally blind to
// this class: they never open oto, so a clean recording does NOT certify
// live smoothness. Verify playout-contract changes on the audible layer.
const liveMaxPendingAudioBytes = 16000 * 2 * 375 / 1000 // 12000 B = 375 ms @ L16@16k mono

// liveSpeakerReadChunk is one paced daemon chunk (L16@16k, 20 ms):
// sizing the drain read this way keeps the backlog overshoot past the
// bound at most 20 ms of audio.
const liveSpeakerReadChunk = 640

// drainLiveSpeaker pumps the .out socket into the sink with the bounded
// lookahead above: writes accumulate in the sink, and once the backlog
// reaches liveMaxPendingAudioBytes the burst is handed off (bursts
// OVERLAP in oto - a handoff never pauses the predecessor; the bound only
// caps how far ahead of the speakers audio runs). On socket end the final
// pending burst is flushed before reporting. Runs until read error / EOF /
// a sink write error; the error goes to done (buffered: the reader selects
// on it, so this never blocks on exit).
//
// Jitter instrumentation (2026-11-09 chop hunt): with MEEPT_VOICE_TRACE=1
// each loop iteration's socket-read gap and each burst handoff size are
// printed to stderr at call end as a compact cadence line. Read gaps >
// ~50 ms while a call is speaking mean the daemon pacer under-supplied
// the client (upstream stall); burst sizes pinned at the 4800 B bound
// mean handoffs are bound-driven (expected), sizes < ~1000 B mean the
// 350 ms sink-gap heuristic is splitting sentences (audible seams).
func drainLiveSpeaker(r io.Reader, s *voice.SpeakerSink, done chan<- error) {
	trace := os.Getenv("MEEPT_VOICE_TRACE") == "1"
	var (
		lastRead   time.Time
		maxReadGap time.Duration
		sumReadGap time.Duration
		gapSamples int
		bytesTotal int64
	)
	buf := make([]byte, liveSpeakerReadChunk)
	var (
		// Audible-layer instrumentation (2026-11-10 seam delta hunt):
		// the earlier acceptance lane measured RECORDED byte streams,
		// which never pass through oto. These counters measure the
		// DEVICE-CLOCK playout: handoff interval vs the previous
		// burst's audio duration. A positive residual = a silent gap
		// at the speakers (oto had finished burst k-1 and waited for
		// burst k) that a file recording structurally cannot show.
		lastFlushAt    time.Time
		lastFlushBytes int
		underrunCount  int
		underrunSumMs  int64
		underrunMaxMs  int64
		burstCount     int
		earlyCount     int // interval < duration (overlap, normal oto mixing)
	)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			now := time.Now()
			if trace {
				bytesTotal += int64(n)
				if !lastRead.IsZero() {
					if g := now.Sub(lastRead); g > 20*time.Millisecond {
						sumReadGap += g
						gapSamples++
						if g > maxReadGap {
							maxReadGap = g
						}
					}
				}
				lastRead = now
			}
			if _, werr := s.Write(buf[:n]); werr != nil {
				done <- werr
				return
			}
			if s.Pending() >= liveMaxPendingAudioBytes {
				if trace {
					burstCount++
					now := time.Now()
					durMs := int64(float64(lastFlushBytes) / 2 / 16000 * 1000)
					if !lastFlushAt.IsZero() {
						residual := now.Sub(lastFlushAt).Milliseconds() - durMs
						if residual > 20 { // >20 ms silent gap at the speakers
							underrunCount++
							underrunSumMs += residual
							if residual > underrunMaxMs {
								underrunMaxMs = residual
							}
						} else if residual < -20 {
							earlyCount++ // overlap: next burst started before previous ended (normal)
						}
					}
					lastFlushAt = now
					lastFlushBytes = s.Pending()
				}
				if ferr := s.Flush(); ferr != nil {
					done <- ferr
					return
				}
			}
		}
		if rerr != nil {
			// EOF / connection closed = daemon ended the call: normal.
			// Flush the tail burst BEFORE reporting the end (the
			// per-read flush is gone - live defect 2026-11-09 jitter;
			// the bound flush above is the only mid-stream handoff).
			// Flush on an empty buffer is a no-op, so this is safe on
			// a zero-byte EOF.
			if ferr := s.Flush(); ferr != nil {
				done <- ferr
				return
			}
			if trace {
				avg := time.Duration(0)
				if gapSamples > 0 {
					avg = sumReadGap / time.Duration(gapSamples)
				}
				fmt.Fprintf(os.Stderr, "voice trace: bytes=%d gap_reads=%d read_gap_avg=%v read_gap_max=%v bursts=%d underruns=%d underrun_sum_ms=%d underrun_max_ms=%d early_overlaps=%d\n",
					bytesTotal, gapSamples, avg, maxReadGap, burstCount, underrunCount, underrunSumMs, underrunMaxMs, earlyCount)
			}
			done <- nil
			return
		}
	}
}

// liveWarmBurstBytes is one silent 20 ms burst (L16@16k mono) used to
// open the oto audio context before any real audio can land.
const liveWarmBurstBytes = 640

// liveSpeaker is the warmed + (optionally) recorded speaker chain for a
// live local call: the player seam, the sink the drain pumps into, the
// dialed .out conn, and the recorder whose file closes at teardown.
type liveSpeaker struct {
	conn   net.Conn
	player voice.Player
	sink   *voice.SpeakerSink
	rec    *recorderPlayer // nil when --record is off
}

// setupLiveSpeaker constructs the CLI's oto player, WARMS the audio
// context with a silent burst, and only then dials the .out socket —
// the same fix shape the link monitor received (startVoiceLinkMonitor).
// Without the pre-dial warm-up, oto's lazy context init (~0.3 s device
// open + ready wait + CoreAudio first-start latency) hits the first
// ~150 ms burst of the greeting and drops/distorts it: the audible
// "beginning gets truncated" the operator reported on live call
// local-496a3fd0d2ab3fc6 (the monitor's twin defect was
// link-d52f032b6e010882).
//
// recordDir ("" = off) wraps the player in the monitor's skip-first
// recorderPlayer: the tee records EXACTLY the bytes handed to playback
// (so scripts/voice-gap-analyze.py measures what the operator HEARS),
// and the warm-up goes THROUGH the recorder so the silent burst opens
// the context but provably never enters the .raw (monitor convention).
// A meta-write or recorder-create failure degrades recording off (with
// a stderr warning) exactly like the monitor, never the call.
//
// newPlayer and dial are seams (mirroring how drainLiveSpeaker takes an
// io.Reader): production passes voice.NewOtoPlayer + net.Dial; the unit
// test passes a fake player and a fake dial to pin the warm-before-dial
// ORDER, which a real dial cannot exercise headlessly.
func setupLiveSpeaker(open voiceLocalOpenResult, employee, recordDir string,
	newPlayer func() voice.Player, dial func(network, addr string) (net.Conn, error),
) (*liveSpeaker, error) {
	player := newPlayer()
	var (
		rec  *recorderPlayer
		p    voice.Player = player
		warm              = func() error { return player.Play(make([]byte, liveWarmBurstBytes)) }
	)
	if recordDir != "" {
		// Belt-and-braces: validate() already MkdirAlls; this keeps the
		// seam safe for any caller. Then meta, then the recorder file.
		if rerr := os.MkdirAll(recordDir, 0o755); rerr != nil {
			fmt.Fprintf(os.Stderr, "record: dir: %v (recording off)\n", rerr)
			recordDir = ""
		} else if merr := writeLocalRecordingMeta(recordDir, open, employee); merr != nil { //nolint:nestif // degrade recording, never the call
			fmt.Fprintf(os.Stderr, "record: meta: %v (recording off)\n", merr)
			recordDir = ""
		} else {
			rc, cerr := newSideRecorder(player, recordDir, open.CallID, "")
			if cerr != nil {
				fmt.Fprintf(os.Stderr, "record: %v (recording off)\n", cerr)
				recordDir = ""
			} else {
				rec = rc
				p = rc
				// Warm THROUGH the recorder: skipFirst consumes the
				// silent burst from the tee but forwards it to oto.
				warm = func() error { return rc.Play(make([]byte, liveWarmBurstBytes)) }
			}
		}
	}
	// Warm BEFORE dialing: the order is the whole point of the fix —
	// the daemon starts teeing audio out of the splice the instant the
	// dial lands, so a cold player here = a clipped greeting.
	if werr := warm(); werr != nil {
		if rec != nil {
			_ = rec.closeFile()
		}
		return nil, fmt.Errorf("speaker warm-up failed: %w", werr)
	}
	conn, err := dial("unix", open.OutPath)
	if err != nil {
		if rec != nil {
			_ = rec.closeFile()
		}
		return nil, fmt.Errorf("dial speaker socket %s: %w", open.OutPath, err)
	}
	return &liveSpeaker{
		conn:   conn,
		player: player,
		sink:   voice.NewSpeakerSink(p, 0),
		rec:    rec,
	}, nil
}

// teardownLiveSpeaker is the live-local speaker teardown, mirroring the
// monitor's per-side goroutine (dialSide): close the .out conn (ends the
// drain loop at socket EOF), wait for the drain to RETURN, then flush
// the sink tail (hands the final burst through the recorder) and close
// the recorder file — so the .raw is the complete, gap-free, in-order
// playback byte stream. The Player seam's Close (the oto device) runs
// last, once nothing can Play. The drain wait is bounded: a closed unix
// conn makes the drain's Read return at once; the timeout only guards a
// wedged oto call, and a timed-out drain is left to die on the closed
// conn rather than hanging the CLI's exit (a late recorder Play after
// closeFile is a no-op tee by recorderPlayer contract, never a panic).
func teardownLiveSpeaker(lspk *liveSpeaker, spkReturned chan struct{}) {
	_ = lspk.conn.Close() //nolint:errcheck // teardown
	select {
	case <-spkReturned:
	case <-time.After(2 * time.Second):
	}
	_ = lspk.sink.Flush() //nolint:errcheck // best-effort tail; drain is done
	if lspk.rec != nil {
		if cerr := lspk.rec.closeFile(); cerr != nil {
			fmt.Fprintf(os.Stderr, "record: close: %v\n", cerr)
		}
	}
	lspk.player.Close()
}

// writeLocalRecordingMeta writes DIR/<call-id>-meta.json describing the
// human-leg recording (the local sibling of writeLinkRecordingMeta —
// one call, one side, so its own slim shape).
func writeLocalRecordingMeta(dir string, open voiceLocalOpenResult, employee string) error {
	meta := struct {
		CallID     string `json:"call_id"`
		Employee   string `json:"employee"`
		StartedAt  string `json:"started_at"`
		SampleRate int    `json:"sample_rate"`
	}{
		CallID:     open.CallID,
		Employee:   employee,
		StartedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		SampleRate: voice.MicSampleRate,
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(filepath.Join(dir, open.CallID+"-meta.json"), b, 0o644)
}

func runVoiceLocalLive(ctx context.Context, open voiceLocalOpenResult, employee, recordDir string) error {
	cap, err := voice.NewRawMicCapture("", "")
	if err != nil {
		return err
	}
	defer cap.Close() //nolint:errcheck // teardown; ffmpeg reaped via ctx cancel

	// speaker chain: construct + warm the oto context BEFORE dialing
	// (leaf 02, C2 — see setupLiveSpeaker), with the optional
	// human-leg recorder tee.
	lspk, err := setupLiveSpeaker(open, employee, recordDir,
		func() voice.Player { return voice.NewOtoPlayer() }, net.Dial)
	if err != nil {
		return err
	}

	inConn, err := net.Dial("unix", open.InPath)
	if err != nil {
		return fmt.Errorf("dial mic socket %s: %w", open.InPath, err)
	}
	defer inConn.Close() //nolint:errcheck // teardown

	micDone := make(chan error, 1)
	spkDone := make(chan error, 1)
	// spkReturned closes when the drain goroutine RETURNS (not when it
	// sends on spkDone — the main select consumes that value on the
	// call-ended path, and the teardown must still know the goroutine
	// is gone before it touches the non-thread-safe sink).
	spkReturned := make(chan struct{})
	// mic: capture -> in socket. Byte-counted atomically so the count is
	// live even while io.Copy is running, and a capture death can be
	// DISTINGUISHED from a user hangup: only a zero-byte death while the
	// call is still supposed to be live is an error (a ^C or daemon-side
	// call end closes the sockets and the mic dies at zero bytes as a
	// SYMPTOM, not the cause - reporting that as a mic failure was the
	// user-visible false alarm).
	var micBytes int64
	go func() {
		n, err := io.Copy(inConn, cap)
		atomic.StoreInt64(&micBytes, n)
		micDone <- err
	}()
	// speaker: out socket -> sink (raw PCM bursts -> oto), bounded
	// lookahead (leaf 01). No unbounded per-utterance buffering: the old
	// gap-only flush let seconds of audio queue behind a barge-in and
	// talk over the new answer.
	go func() {
		drainLiveSpeaker(lspk.conn, lspk.sink, spkDone)
		close(spkReturned)
	}()
	// Registered AFTER the drain starts (the only code between dialing
	// and here has no error return, so the teardown never waits on a
	// drain that was never spawned): close the .out conn to end the
	// drain loop, WAIT for it to return, then flush the sink tail and
	// close the recorder file — the monitor's dialSide teardown order —
	// so the .raw is the complete, in-order playback byte stream and the
	// non-thread-safe SpeakerSink is never touched while the drain is
	// still writing it. The wait is bounded (2 s): a closed unix socket
	// makes the drain's Read return immediately; the timeout only guards
	// a wedged oto call.
	defer teardownLiveSpeaker(lspk, spkReturned)
	select {
	case err := <-micDone:
		// The mic pipe ended: an ffmpeg death. While the call is still
		// live that is a failure (classify + name the ffmpeg cause);
		// after a cancel it is teardown noise.
		if ctx.Err() != nil {
			return nil
		}
		if cErr := classifyMicDeath(err, atomic.LoadInt64(&micBytes)); cErr != nil {
			if tail := cap.StderrTail(); tail != "" {
				return fmt.Errorf("%w (ffmpeg: %s)", cErr, tail)
			}
			return cErr
		}
		return nil
	case err := <-spkDone:
		// The .out socket closed: the call ended (daemon hangup, idle
		// reaper, or the SIGINT path's voice.local.end) or errored.
		// Never re-interpret this as a mic failure.
		_ = cap.Close()
		<-micDone
		return err
	case <-ctx.Done():
		return nil
	}
}

// ---------------------------------------------------------------------------
// operator telemetry poller (voice-events)
// ---------------------------------------------------------------------------

// voiceEventPollInterval is the CLI's voice.local.events cadence. 500 ms
// is well under any human-perceptible turn latency and cheap on the
// loopback RPC socket (the client's callMu serialises calls with the
// status poll / transcript reads, so the two pollers interleave safely).
const voiceEventPollInterval = 500 * time.Millisecond

// voiceEventRow mirrors one voice.local.events event on the wire.
type voiceEventRow struct {
	Seq    uint64 `json:"seq"`
	Kind   string `json:"kind"`
	At     string `json:"at"`
	Text   string `json:"text"`
	Words  int    `json:"words"`
	Ms     int64  `json:"ms"`
	Reason string `json:"reason"`
}

// voiceEventRender turns one telemetry event into the operator line body
// (pure, unit-tested): what the CLI prints after the "[side employee] "
// prefix. Unknown kinds render the raw kind so a future daemon-side kind
// never disappears silently.
func voiceEventRender(ev voiceEventRow) string {
	at := ev.At
	if len(at) >= 19 {
		at = at[11:19]
	}
	switch ev.Kind {
	case "heard":
		return at + " heard: " + ev.Text
	case "prompt_dispatch":
		return at + " thinking (" + itoa(ev.Words) + " words): " + ev.Text
	case "turn_final":
		return at + " replied (" + itoa(ev.Words) + " words, " + itoa64(ev.Ms) + "ms)"
	case "barge_in":
		return at + " >> barge-in"
	case "call_ended":
		return at + " call ended: " + ev.Reason
	case "silence_deferred":
		return at + " silence deferred (agent audio playing, x" + ev.Reason + ")"
	case "prompt_held":
		return at + " held (peer speaking): " + ev.Text
	case "prompt_released":
		return at + " released: " + ev.Text
	case "prompt_dropped":
		return at + " dropped (" + ev.Reason + "): " + ev.Text
	default:
		return at + " " + ev.Kind
	}
}

func itoa(n int) string     { return strconv.Itoa(n) }
func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// voiceEventTracker owns the per-call cursors so the live poller loop and
// the end-of-run synchronous drain never re-print the same event. It polls
// voice.local.events for each known call id and prints one line per event,
// prefixed "[<label> <employee>] ". Best-effort by contract (like the
// monitor tap): a failed poll is silently retried on the next tick - the
// link/local run must NEVER die because telemetry could not be read.
// start() spawns the ticker goroutines (they exit when ctx ends, when the
// call's feed reports found=false, or on the bounded-call timeout);
// stop() (after the cancel) joins them so a drain that follows never
// races a live poller's prints. Each round-trip is bounded (C-11): a
// client.Call that would block for minutes (retry ladder) is abandoned
// at voiceEventCallTimeout and its result discarded - stop() can never
// hang on a wedged socket.
type voiceEventTracker struct {
	client interface {
		Call(method string, params any) (json.RawMessage, error)
	}
	// calls: call_id -> {label, employee}.
	calls   map[string][2]string
	mu      sync.Mutex
	cursors map[string]uint64
	// stopped guards printing once stop() has begun joining: an
	// abandoned-but-still-running poll must never print after stop
	// returns (C-11).
	stopped bool
	// ended records the first found=false observation per ccid (C-20):
	// the poller prints one "feed ended" line and stops polling it.
	ended map[string]bool
	wg    sync.WaitGroup
}

// voiceEventCallTimeout bounds one voice.local.events round-trip (C-11):
// the RPC client's own retry ladder can hold a call for minutes, so the
// poller abandons at this window and retries on the next tick instead.
const voiceEventCallTimeout = 5 * time.Second

func newVoiceEventTracker(client interface {
	Call(method string, params any) (json.RawMessage, error)
}, calls map[string][2]string) *voiceEventTracker {
	return &voiceEventTracker{
		client:  client,
		calls:   calls,
		cursors: make(map[string]uint64, len(calls)),
		ended:   make(map[string]bool, len(calls)),
	}
}

// start spawns one ticker poller goroutine per known call id. The
// goroutines exit when ctx is cancelled; stop() (after the cancel) joins
// them so a drain that follows never races a live poller's prints.
func (t *voiceEventTracker) start(ctx context.Context) {
	for ccid, side := range t.calls {
		if ccid == "" {
			continue // an old daemon returned no id for that side
		}
		t.wg.Add(1)
		go func(ccid, label, emp string) {
			defer t.wg.Done()
			t.poll(ctx, ccid, label, emp)
		}(ccid, side[0], side[1])
	}
}

// stop joins the live poller goroutines. Call after cancelling their ctx.
// Every in-flight round-trip is bounded by voiceEventCallTimeout, so this
// join waits at most ~one timeout per poller, never minutes (C-11).
func (t *voiceEventTracker) stop() {
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()
	t.wg.Wait()
}

// pollVoiceEvents drives one call's telemetry line loop (ticker cadence).
func (t *voiceEventTracker) poll(ctx context.Context, ccid, label, emp string) {
	tick := time.NewTicker(voiceEventPollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		// One tick: print + advance. Errors are best-effort retried.
		// found=false ends THIS ccid's poller (C-20: the call is no
		// longer tracked - an evicted/unknown id would otherwise tick
		// forever); a link's other side keeps its own poller.
		if !t.pollOnce(ccid, label, emp) {
			return
		}
	}
}

// pollOnce issues one voice.local.events round-trip and prints its rows.
// Returns false when this ccid's feed has ended (found=false observed, or
// the tracker stopped): the caller stops polling it. An errored/timed-out
// round-trip returns true (best-effort: retry on the next tick).
func (t *voiceEventTracker) pollOnce(ccid, label, emp string) bool {
	t.mu.Lock()
	if t.stopped || t.ended[ccid] {
		t.mu.Unlock()
		return false
	}
	cursor := t.cursors[ccid]
	t.mu.Unlock()
	res, ok, dead := t.callBounded(ccid, cursor)
	if dead {
		// found=false at cursor 0 is NOT the C-20 feed end: the ring is
		// created LAZILY by the first Append (heard/dispatch events land
		// only when the first final prompt arrives), so the very first
		// tick of a fresh call always reads found=false. Treating that
		// as "feed ended" permanently silenced the live operator feed -
		// the user saw nothing but the "call open" line for the whole
		// conversation (regression from fcaa2bab's C-20, live call
		// local-53b347f028f0a43f). Keep ticking: the ctx cancel at call
		// end bounds the live loop and the drain terminates the ccid
		// explicitly. A found=false at cursor > 0 is the real
		// eviction/teardown signal and still ends THIS ccid's poller.
		if cursor == 0 {
			return true
		}
		// The daemon answered found=false: the feed is over (C-20).
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.stopped {
			return false
		}
		if !t.ended[ccid] {
			fmt.Printf("[%s %s] telemetry feed ended (call no longer tracked)\n", label, emp)
		}
		t.ended[ccid] = true
		return false
	}
	if !ok {
		return true // best-effort: error/timeout retries on the next tick
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return false // abandoned window: never print after stop began
	}
	for _, ev := range res.Events {
		fmt.Printf("[%s %s] %s\n", label, emp, voiceEventRender(ev))
	}
	if res.Truncated {
		fmt.Printf("[%s %s] note: telemetry backlog truncated (poller fell behind; rows were lost)\n", label, emp)
	}
	if res.Cursor > cursor && res.Cursor > t.cursors[ccid] {
		t.cursors[ccid] = res.Cursor
	}
	return true
}

// voiceEventRes is the voice.local.events wire result (the fields the CLI
// consumes; found=false is the daemon's ring-less/evicted answer).
type voiceEventRes struct {
	Events    []voiceEventRow `json:"events"`
	Cursor    uint64          `json:"cursor"`
	Truncated bool            `json:"truncated"`
	HasMore   bool            `json:"has_more"`
	Found     bool            `json:"found"`
}

// callBounded runs one voice.local.events round-trip on a child goroutine
// bounded by voiceEventCallTimeout (C-11): a wedged socket cannot hold the
// poller (and stop's join) for the client's multi-minute retry ladder. An
// abandoned child's result is discarded; it prints nothing (printing is
// guarded by pollOnce's stopped check under mu). Returns
// (res, ok=true) on success, (_, false, false) on error/timeout, and
// (_, false, true) when the envelope carries found=false.
func (t *voiceEventTracker) callBounded(ccid string, cursor uint64) (res voiceEventRes, ok bool, feedEnded bool) {
	type result struct {
		res voiceEventRes
		ok  bool
	}
	ch := make(chan result, 1)
	go func() {
		raw, err := t.client.Call("voice.local.events", map[string]any{
			"call_id": ccid, "cursor": cursor,
		})
		if err != nil {
			ch <- result{}
			return
		}
		var r voiceEventRes
		if json.Unmarshal(raw, &r) != nil {
			ch <- result{}
			return
		}
		ch <- result{res: r, ok: true}
	}()
	select {
	case r := <-ch:
		if r.ok && !r.res.Found {
			return voiceEventRes{}, false, true
		}
		return r.res, r.ok, false
	case <-time.After(voiceEventCallTimeout):
		return voiceEventRes{}, false, false
	}
}

// drainOnce flushes each call's unread telemetry synchronously (after the
// live poller's ctx was cancelled and stop() joined them): poll until a
// call reports no new events or its feed has ended, bounded to a few
// rounds so a wedged daemon cannot hang the CLI. The drain intentionally
// runs while the tracker is marked stopped - its polls must still print
// (stop only silences the LIVE pollers) - so it uses drainPollOnce, which
// skips the stopped gate but keeps the bounded round-trip (C-11) and the
// per-call ended state.
func (t *voiceEventTracker) drainOnce() {
	for ccid, side := range t.calls {
		if ccid == "" || t.ended[ccid] {
			continue
		}
		for round := 0; round < 8; round++ {
			if !t.drainPollOnce(ccid, side[0], side[1]) {
				break
			}
		}
	}
}

// drainPollOnce is pollOnce's post-stop twin: identical result handling
// (including the C-20 feed-end transition) but it ignores the stopped
// flag - the drain IS the stopping phase's printer. Bounded like pollOnce
// (C-11): one voiceEventCallTimeout per round, 8 rounds per call worst
// case, never minutes.
func (t *voiceEventTracker) drainPollOnce(ccid, label, emp string) bool {
	t.mu.Lock()
	if t.ended[ccid] {
		t.mu.Unlock()
		return false
	}
	cursor := t.cursors[ccid]
	t.mu.Unlock()
	res, ok, dead := t.callBounded(ccid, cursor)
	if dead {
		t.mu.Lock()
		if cursor > 0 && !t.ended[ccid] {
			fmt.Printf("[%s %s] telemetry feed ended (call no longer tracked)\n", label, emp)
		}
		t.ended[ccid] = true
		t.mu.Unlock()
		return false
	}
	if !ok {
		return true // timeout: leave the tail for a possible later drain
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if res.Found {
		for _, ev := range res.Events {
			fmt.Printf("[%s %s] %s\n", label, emp, voiceEventRender(ev))
		}
		if res.Truncated {
			fmt.Printf("[%s %s] note: telemetry backlog truncated (poller fell behind; rows were lost)\n", label, emp)
		}
		if res.Cursor > cursor && res.Cursor > t.cursors[ccid] {
			t.cursors[ccid] = res.Cursor
		}
		return true
	}
	// found=false during drain: the feed ended before the tail flushed.
	if cursor > 0 && !t.ended[ccid] {
		fmt.Printf("[%s %s] telemetry feed ended (call no longer tracked)\n", label, emp)
	}
	t.ended[ccid] = true
	return false
}

// ---------------------------------------------------------------------------
// meept voice status
// ---------------------------------------------------------------------------

func newVoiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   cmdStatus,
		Short: "show live voice calls (both legs)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			raw, err := client.Call("voice.local.status", map[string]any{})
			if err != nil {
				return fmt.Errorf("voice.local.status: %w", err)
			}
			var res struct {
				Calls []voiceCallStatusRow `json:"calls"`
			}
			if err := json.Unmarshal(raw, &res); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			fmt.Println(voiceStatusTable(res.Calls))
			return nil
		},
	}
}

// voiceCallStatusRow mirrors the voice.local.status row shape (CL8,
// contact-display leaf 02 swap).
type voiceCallStatusRow struct {
	CallID        string `json:"call_id"`
	EmployeeID    string `json:"employee_id"`
	TargetName    string `json:"target_name"`
	TargetDisplay string `json:"target_display"`
	SinceRFC3339  string `json:"since_rfc3339"`
	Leg           string `json:"leg"`
	// Deprecated: the raw target. The contact-display daemon never fills
	// it for phone legs (O4); local-leg rows carry "local:<label>"
	// verbatim. Read ONLY as a render fallback — never for routing.
	Target string `json:"target"`
}

// targetDisplay picks the row's render string (contact-display leaf 03,
// master O1): display form first, then name, then the legacy raw target
// (non-empty = an old server or a local-leg label), else "unknown".
// Pure field preference — no client-side resolution or number formatting.
func (r voiceCallStatusRow) targetDisplay() string {
	if r.TargetDisplay != "" {
		return r.TargetDisplay
	}
	if r.TargetName != "" {
		return r.TargetName
	}
	if r.Target != "" {
		return r.Target
	}
	return "unknown"
}

// voiceStatusTable renders the both-legs status table (T6: unit-tested
// formatter - no daemon, no RPC in the test).
func voiceStatusTable(rows []voiceCallStatusRow) string {
	if len(rows) == 0 {
		return "no live voice calls."
	}
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "LEG\tCALL\tEMPLOYEE\tTARGET\tSINCE")
	for _, r := range rows {
		leg := r.Leg
		if leg == "" {
			leg = "phone"
		}
		since := r.SinceRFC3339
		if since == "" {
			since = "-"
		}
		fmt.Fprintf(w, "%s	%s	%s	%s	%s\n", leg, r.CallID, r.EmployeeID, r.targetDisplay(), since)
	}
	//nolint:errcheck // strings.Builder writes never fail
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

// ---------------------------------------------------------------------------
// meept voice link
// ---------------------------------------------------------------------------

// voice link (deadair-link plan leaf 03): cross-connect TWO employees'
// local legs inside the daemon via the voice.link.* RPC and let them
// talk. The CLI NEVER touches audio - it opens the link, polls
// voice.link.status every linkPollInterval printing per-side turn
// counts, closes at the deadline, prints both transcripts, and exits 0
// iff BOTH sides logged >= linkRequiredAssistantTurns assistant turns
// (this doubles as the automated acceptance check).

// linkPollInterval is the status poll cadence while the link runs.
const linkPollInterval = 5 * time.Second

// linkRequiredAssistantTurns is the acceptance bar: both sides need at
// least this many assistant turns for the command to exit 0.
const linkRequiredAssistantTurns = 2

// linkFlags is the validated `voice link` flag set (pure validation +
// the --record directory side effect, unit-tested without a daemon).
type linkFlags struct {
	a       string
	b       string
	seconds int
	monitor bool
	record  string
}

// validate reports the flag errors the leaf pins: --a / --b required,
// a==b refused EARLY (before any RPC), seconds <= 0 refused, and
// --record without --monitor refused (recording rides the monitor's
// playback seam - no tap, no bytes). When --record is set the DIR is
// created if it does not exist (MkdirAll is idempotent).
func (f linkFlags) validate() error {
	if f.a == "" {
		return fmt.Errorf("--a is required (first employee id)")
	}
	if f.b == "" {
		return fmt.Errorf("--b is required (second employee id)")
	}
	if f.a == f.b {
		return fmt.Errorf("--a and --b must be different employees (got %q twice)", f.a)
	}
	if f.seconds <= 0 {
		return fmt.Errorf("--seconds must be > 0 (got %d)", f.seconds)
	}
	if f.record != "" && !f.monitor {
		return fmt.Errorf("--record requires --monitor (the recorder taps the monitor's playback seam)")
	}
	if f.record != "" {
		if err := os.MkdirAll(f.record, 0o755); err != nil {
			return fmt.Errorf("--record: cannot create directory %q: %w", f.record, err)
		}
	}
	return nil
}

func newVoiceLinkCmd() *cobra.Command {
	var (
		a       string
		b       string
		seconds int
		monitor bool
		record  string
	)

	cmd := &cobra.Command{
		Use:   "link",
		Short: "cross-connect two employees' local voice legs and let them talk",
		Long: `Open a two-agent voice link in the daemon: employee A's spoken
audio becomes employee B's microphone and vice versa (real TTS -> real
socket PCM -> real STT -> real turns on both sides). No human, no mic.

Polls voice.link.status every 5 s printing both sides' turn counts, ends
the link at --seconds, prints both transcripts, and exits 0 only when
BOTH sides logged at least 2 assistant turns - so the command doubles as
the automated acceptance check for the dead-air / jitter harness.

--monitor additionally plays BOTH employees' spoken audio through the
operator's speakers (listen-only tap; the mic stays off - the operator
hears the conversation without joining it).

--record DIR (requires --monitor) writes EXACTLY the bytes handed to
playback, per side, to DIR/<linkID>-<a|b>.raw (raw s16le mono 16 kHz)
plus DIR/<linkID>-meta.json. Gap analysis over those files reflects what
the operator HEARS. Feed the .raw files to scripts/voice-gap-analyze.py.

Examples:
  meept voice link --a companion --b secretary --seconds 90
  meept voice link --a companion --b secretary --monitor --seconds 60
  meept voice link --a companion --b secretary --monitor --record /tmp/link-rec --seconds 60`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f := linkFlags{a: a, b: b, seconds: seconds, monitor: monitor, record: record}
			if err := f.validate(); err != nil {
				return err
			}
			return runVoiceLink(f)
		},
	}

	cmd.Flags().StringVar(&a, "a", "", "employee id for side A (required)")
	cmd.Flags().StringVar(&b, "b", "", "employee id for side B (required, must differ from --a)")
	cmd.Flags().IntVar(&seconds, "seconds", 60, "link duration in seconds (must be > 0)")
	cmd.Flags().BoolVar(&monitor, "monitor", false, "play both sides' audio through the operator's speakers (listen-only tap)")
	cmd.Flags().StringVar(&record, "record", "", "record the exact bytes handed to playback per side into DIR (requires --monitor)")
	return cmd
}

// voiceLinkOpenResult mirrors the voice.link.open wire shape.
type voiceLinkOpenResult struct {
	LinkID       string `json:"link_id"`
	ACCID        string `json:"a_ccid"`
	BCCID        string `json:"b_ccid"`
	MonitorAPath string `json:"a_mon_path"`
	MonitorBPath string `json:"b_mon_path"`
}

// voiceLinkSideStatus mirrors one voice.link.status side row.
type voiceLinkSideStatus struct {
	CallID         string `json:"call_id"`
	EmployeeID     string `json:"employee_id"`
	AssistantTurns int    `json:"assistant_turns"`
	UserTurns      int    `json:"user_turns"`
	Transcript     []struct {
		Role string `json:"role"`
		Text string `json:"text"`
		At   string `json:"at"`
	} `json:"transcript"`
}

// voiceLinkStatusResult mirrors the voice.link.status wire shape.
type voiceLinkStatusResult struct {
	LinkID string              `json:"link_id"`
	Ended  bool                `json:"ended"`
	A      voiceLinkSideStatus `json:"a"`
	B      voiceLinkSideStatus `json:"b"`
}

// runVoiceLink drives the whole link lifecycle. Exit contract (leaf
// doc): nil (exit 0) iff both sides reach linkRequiredAssistantTurns
// assistant turns; a descriptive error otherwise (exit 1). An RPC-level
// failure (daemon down, link refused) is returned as-is with context.
func runVoiceLink(f linkFlags) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	raw, err := client.Call("voice.link.open", map[string]any{
		"employee_a": f.a,
		"employee_b": f.b,
		"monitor":    f.monitor,
	})
	if err != nil {
		return fmt.Errorf("voice.link.open: %w", err)
	}
	var open voiceLinkOpenResult
	if err := json.Unmarshal(raw, &open); err != nil {
		return fmt.Errorf("failed to parse open response: %w", err)
	}
	if open.LinkID == "" {
		return fmt.Errorf("daemon refused the link (no link id returned)")
	}
	fmt.Printf("link %s open: a=%s(%s) b=%s(%s), %ds\n",
		open.LinkID, f.a, open.ACCID, f.b, open.BCCID, f.seconds)

	// Operator telemetry: live per-side event lines (heard / thinking /
	// replied / barge-in / ended) polled from the daemon's per-call rings
	// over voice.local.events. Best-effort: the poller never fails the run.
	teleCtx, teleCancel := context.WithCancel(context.Background())
	defer teleCancel()
	events := newVoiceEventTracker(client, map[string][2]string{
		open.ACCID: {"a", f.a},
		open.BCCID: {"b", f.b},
	})
	events.start(teleCtx)

	// Audible monitor: dial the daemon's tap sockets and play BOTH
	// directions through the operator's speakers (listen-only - nothing
	// is ever written back, the .in mic side stays untouched). Best-
	// effort by contract: a dial failure only costs the speaker tap,
	// never the link run.
	if f.monitor {
		startVoiceLinkMonitor(open, f.a, f.b, f.record)
	} else if open.MonitorAPath != "" {
		fmt.Println("note: daemon returned monitor tap paths but --monitor was not set; ignoring them")
	}

	// SIGINT ends the poll early; the close below still runs, so the
	// daemon's release fan-out fires exactly once per leg.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	deadline := time.Now().Add(time.Duration(f.seconds) * time.Second)
	var last voiceLinkStatusResult
	pollErr := error(nil)
	pollTick := time.NewTicker(linkPollInterval)
	defer pollTick.Stop()
	deadlineTimer := time.NewTimer(time.Until(deadline))
	defer deadlineTimer.Stop()

linkLoop:
	for {
		select {
		case <-sigCh:
			fmt.Println("\nsignal received; ending link...")
			break linkLoop
		case <-deadlineTimer.C:
			break linkLoop
		case <-pollTick.C:
			st, err := callVoiceLinkStatus(client, open.LinkID)
			if err != nil {
				// A transient status failure never kills the harness
				// run: keep the last good snapshot, remember the error,
				// and report it only if no later poll succeeds.
				if pollErr == nil {
					pollErr = err
				}
				continue
			}
			pollErr = nil
			last = st
			fmt.Printf("[%s] a: %d user / %d assistant | b: %d user / %d assistant\n",
				time.Now().Format("15:04:05"),
				st.A.UserTurns, st.A.AssistantTurns,
				st.B.UserTurns, st.B.AssistantTurns)
			if st.Ended {
				// The daemon closed the link already (an operator
				// voice.link.close, or the harness reaped a leg): stop
				// early, the final read follows.
				break linkLoop
			}
		}
	}

	// Close the link (idempotent; the daemon may have reaped a leg via
	// the silence reaper first).
	if _, closeErr := client.Call("voice.link.close", map[string]any{"link_id": open.LinkID}); closeErr != nil {
		fmt.Fprintf(os.Stderr, "voice.link.close: %v\n", closeErr)
	}

	// Stop the live telemetry poller and flush each side's tail
	// synchronously: the rings outlive the call (the daemon evicts only
	// after 32 newer calls), so the end/barge-in events of the final
	// seconds still land on the console before the transcripts.
	teleCancel()
	events.stop()
	events.drainOnce()

	// Final status AFTER close: the daemon retains the entry (and the
	// calls.db rows live on), so this is the authoritative transcript
	// snapshot.
	if st, err := callVoiceLinkStatus(client, open.LinkID); err == nil {
		last = st
		pollErr = nil
	} else if pollErr == nil {
		pollErr = err
	}

	printLinkTranscript("a", last.A)
	printLinkTranscript("b", last.B)

	if pollErr != nil {
		return fmt.Errorf("voice.link.status: %w", pollErr)
	}
	if last.A.AssistantTurns < linkRequiredAssistantTurns || last.B.AssistantTurns < linkRequiredAssistantTurns {
		return fmt.Errorf("link did not converge: side a logged %d assistant turns, side b %d (need >=%d each)",
			last.A.AssistantTurns, last.B.AssistantTurns, linkRequiredAssistantTurns)
	}
	fmt.Printf("link verified: both employees exchanged >=%d assistant turns.\n", linkRequiredAssistantTurns)
	return nil
}

// startVoiceLinkMonitor dials the daemon's monitor tap sockets and plays
// BOTH sides' audio through the operator's speakers (listen-only: the
// tap sockets carry tee PCM out of the bridge; nothing is ever written
// back - the operator does not join the conversation). Each side gets
// its OWN player seam + SpeakerSink + drainLiveSpeaker loop, reusing the
// live-local machinery: two oto instances overlap safely in oto's mixer
// (the player.go overlap contract - Play never pauses predecessors), so
// A and B audible streams mix together.
//
// recordDir ("" = off) additionally tees EXACTLY the bytes handed to
// playback into DIR/<linkID>-<a|b>.raw (raw s16le mono 16 kHz) via
// recorderPlayer, plus one DIR/<linkID>-meta.json, so gap analysis
// reflects what the operator HEARS. Default off = zero behaviour change.
//
// Best-effort by contract: a dial failure disables that side with a
// warning, never aborts the link run; the loops die on socket EOF when
// the link closes (tap conns close with the bridge's teardown).
func startVoiceLinkMonitor(open voiceLinkOpenResult, aEmp, bEmp, recordDir string) {
	// ONE oto context per process is a hard driver limit (a second
	// oto.NewContext errors "context is already created" - live
	// link-d2d7e713a8d914f5 killed side b's player). Both sides share a
	// single AudioPlayer; bursts from the two sinks OVERLAP in oto's
	// mixer (the internal/tts playback contract), which is exactly what
	// a two-voice monitor needs. The monitor never Clears, so sharing
	// cannot silence one side via the other.
	player := voice.NewOtoPlayer()
	// Warm the audio context BEFORE dialing the taps: the tee starts
	// flowing the instant a dial lands, and lazy context init (~0.3 s
	// device open + ready wait) stalled the first writes and dropped
	// the greeting burst behind the 8 KB socket buffer (live
	// link-d52f032b6e010882). One silent 20 ms burst = pure warm-up.
	// When recording, this top-level warm-up is SKIPPED: each side
	// instead warms through its own skip-first recorderPlayer in
	// dialSide, so the silent 640-byte burst still opens the context
	// but provably cannot enter a .raw file (the skip consumes it).
	if recordDir == "" {
		if werr := player.Play(make([]byte, 640)); werr != nil {
			fmt.Fprintf(os.Stderr, "monitor: speaker warm-up failed: %v (speakers off)\n", werr)
			return
		}
	} else if merr := writeLinkRecordingMeta(recordDir, open, aEmp, bEmp); merr != nil {
		fmt.Fprintf(os.Stderr, "monitor: record meta: %v (recording off)\n", merr)
		recordDir = ""
		if werr := player.Play(make([]byte, 640)); werr != nil {
			fmt.Fprintf(os.Stderr, "monitor: speaker warm-up failed: %v (speakers off)\n", werr)
			return
		}
	}
	dialSide := func(label, emp, path string) {
		if path == "" {
			fmt.Fprintf(os.Stderr, "monitor %s: daemon returned no tap path (speakers off for that side)\n", label)
			return
		}
		conn, err := net.Dial("unix", path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "monitor %s: dial %s: %v (speakers off for that side)\n", label, path, err)
			return
		}
		fmt.Printf("monitor %s: tapping %s via speakers\n", label, emp)
		var p voice.Player = player
		var rec *recorderPlayer
		if recordDir != "" {
			rec, err = newSideRecorder(player, recordDir, open.LinkID, label)
			if err != nil {
				fmt.Fprintf(os.Stderr, "monitor %s: record %v (recording off for that side)\n", label, err)
				// Recording off for this side: fall back to the plain
				// shared player and warm the context directly (the
				// per-recorder skip-first warm-up below never runs).
				if werr := player.Play(make([]byte, 640)); werr != nil {
					fmt.Fprintf(os.Stderr, "monitor %s: speaker warm-up failed: %v (speakers off)\n", label, werr)
				}
			} else {
				p = rec
				// Warm-up THROUGH the recorder (skip-first): silent
				// 640 B burst reaches the shared oto context but is
				// consumed by the skip, never written to the file.
				if werr := rec.Play(make([]byte, 640)); werr != nil {
					fmt.Fprintf(os.Stderr, "monitor %s: speaker warm-up failed: %v (speakers off)\n", label, werr)
				}
			}
		}
		sink := voice.NewSpeakerSink(p, 0)
		done := make(chan error, 1)
		// First-byte announcement: the operator sees the side start to
		// speak exactly when tee audio lands (not at dial time).
		go drainLiveSpeaker(&monitorFirstByteReader{r: conn, label: label, emp: emp}, sink, done)
		go func() {
			// The drain loop exits on socket EOF/error once the daemon
			// closes the tap. Flush this side's tail; the shared player
			// is closed once, by the LAST side out (the taps close
			// together at link end, so either drain finishing first
			// must not yank the device under the other).
			<-done
			_ = conn.Close() //nolint:errcheck // teardown
			_ = sink.Flush()
			// Recorder file closes AFTER this side's drain loop has
			// returned and its tail burst has been handed off (the
			// Flush above went through the recorder), so the .raw is
			// the complete, in-order playback byte stream.
			if rec != nil {
				if cerr := rec.closeFile(); cerr != nil {
					fmt.Fprintf(os.Stderr, "monitor %s: record close: %v\n", label, cerr)
				}
			}
			monitorWG.Done()
		}()
	}
	monitorWG.Add(2)
	dialSide("a", aEmp, open.MonitorAPath)
	dialSide("b", bEmp, open.MonitorBPath)
	go func() {
		monitorWG.Wait()
		player.Close()
	}()
}

// linkRecordingMeta is the DIR/<linkID>-meta.json shape consumed by
// scripts/voice-gap-analyze.py readers (and any future evidence tool).
type linkRecordingMeta struct {
	LinkID     string `json:"link_id"`
	ACCID      string `json:"a_ccid"`
	BCCID      string `json:"b_ccid"`
	AEmployee  string `json:"a_employee"`
	BEmployee  string `json:"b_employee"`
	StartedAt  string `json:"started_at"`
	SampleRate int    `json:"sample_rate"`
}

// writeLinkRecordingMeta writes the one-per-link metadata file next to
// the two .raw streams.
func writeLinkRecordingMeta(dir string, open voiceLinkOpenResult, aEmp, bEmp string) error {
	meta := linkRecordingMeta{
		LinkID:     open.LinkID,
		ACCID:      open.ACCID,
		BCCID:      open.BCCID,
		AEmployee:  aEmp,
		BEmployee:  bEmp,
		StartedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		SampleRate: voice.MicSampleRate,
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(filepath.Join(dir, open.LinkID+"-meta.json"), b, 0o644)
}

// newSideRecorder opens DIR/<linkID>-<label>.raw (DIR/<callID>.raw when
// label is "" — the single-sided local live leg) and wraps inner in a
// skip-first recorderPlayer (the silent warm-up burst must not land in
// the recording).
func newSideRecorder(inner voice.Player, dir, linkID, label string) (*recorderPlayer, error) {
	name := linkID
	if label != "" {
		name += "-" + label
	}
	f, err := os.Create(filepath.Join(dir, name+".raw"))
	if err != nil {
		return nil, fmt.Errorf("create %q: %w", filepath.Join(dir, name+".raw"), err)
	}
	return newRecorderPlayer(inner, f, true), nil
}

// recorderPlayer is the recording Player seam: it tees EXACTLY the bytes
// handed to playback into a raw s16le mono 16 kHz file AND forwards every
// call to the inner player, so gap analysis over the .raw reflects what
// the operator HEARS (not what the socket carried).
//
// Concurrency: one recorder per monitor side; Play is called only by
// that side's drainLiveSpeaker goroutine, closeFile only by its teardown
// goroutine after the drain returned - the mutex makes that ordering
// explicit and keeps Close/Clear safe if the seam is ever driven from
// two goroutines. The buffered file Write under the lock is deliberate:
// burst ORDER is the point of the recorder (an async writer goroutine
// could reorder or drop the tail); writes are <= ~5 KB, buffered, and
// never hold the lock across the inner (oto) call.
type recorderPlayer struct {
	inner voice.Player
	file  *os.File
	buf   *bufio.Writer
	mu    sync.Mutex
	// skipFirst drops the FIRST Play from the recording (the monitor's
	// silent 640-byte warm-up burst) while still forwarding it to the
	// inner player - the context must warm before dialing.
	skipFirst bool
	closed    bool
	writeErr  error // first tee error, surfaced by closeFile
}

func newRecorderPlayer(inner voice.Player, file *os.File, skipFirst bool) *recorderPlayer {
	return &recorderPlayer{inner: inner, file: file, buf: bufio.NewWriter(file), skipFirst: skipFirst}
}

// Play records the burst (unless it is the consumed warm-up) then hands
// it to the inner player unchanged - the tee never blocks or reorders
// playback.
func (r *recorderPlayer) Play(burst []byte) error {
	r.mu.Lock()
	switch {
	case r.closed:
		// Teardown raced a late flush: the drain loop is done; drop the
		// tee rather than writing to a closed fd (playback continues).
	case r.skipFirst:
		r.skipFirst = false
	default:
		//nolint:mutexio // in-order tee is the recorder's contract; see type comment
		if _, err := r.buf.Write(burst); err != nil && r.writeErr == nil {
			r.writeErr = err
		}
	}
	r.mu.Unlock()
	return r.inner.Play(burst)
}

// Clear forwards to the inner player (the monitor never Clears; a shared
// player's Clear must reach the device, and the recorder's already-
// handed bytes are history - recordings are what was played).
func (r *recorderPlayer) Clear() { r.inner.Clear() }

// Close satisfies voice.Player: best-effort file close. The monitor's
// teardown path uses closeFile to surface errors. inner is the SHARED
// monitor player, closed once by the last side out (monitorWG) - the
// recorder never closes the audio context.
func (r *recorderPlayer) Close() { _ = r.closeFile() }

// closeFile flushes the buffered tee and closes the raw file. Idempotent;
// returns the first write/flush/close error (nil = clean stream). The fd
// close runs OUTSIDE the lock (the drain loop has already returned and the
// closed flag makes late Play calls no-op tees), so no I/O is ever held
// under the mutex beyond the deliberate in-order buffered tee write.
func (r *recorderPlayer) closeFile() error {
	r.mu.Lock()
	if r.closed {
		err := r.writeErr
		r.mu.Unlock()
		return err
	}
	r.closed = true
	//nolint:mutexio // in-order tee is the recorder's contract; see type comment
	if err := r.buf.Flush(); err != nil && r.writeErr == nil {
		r.writeErr = err
	}
	writeErr := r.writeErr
	r.mu.Unlock()
	if err := r.file.Close(); err != nil && writeErr == nil {
		writeErr = err
	}
	r.mu.Lock()
	if r.writeErr == nil {
		r.writeErr = writeErr
	}
	err := r.writeErr
	r.mu.Unlock()
	return err
}

// monitorWG coordinates the shared monitor player's teardown: both side
// drains must flush before the one audio context is closed.
var monitorWG sync.WaitGroup

// monitorFirstByteReader announces a side's speech once when tee audio
// first lands on the monitor socket ("[a companion] speaking"), then
// passes every read through untouched. One instance per side; the drain
// loop is its only reader, so no lock is needed.
type monitorFirstByteReader struct {
	r         io.Reader
	label     string
	emp       string
	announced bool
}

func (m *monitorFirstByteReader) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	if n > 0 && !m.announced {
		m.announced = true
		fmt.Printf("[%s %s] speaking\n", m.label, m.emp)
	}
	return n, err
}

// callVoiceLinkStatus issues one voice.link.status round-trip.
func callVoiceLinkStatus(client interface {
	Call(method string, params any) (json.RawMessage, error)
}, linkID string) (voiceLinkStatusResult, error) {
	var res voiceLinkStatusResult
	raw, err := client.Call("voice.link.status", map[string]any{"link_id": linkID})
	if err != nil {
		return res, fmt.Errorf("voice.link.status: %w", err)
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return res, fmt.Errorf("failed to parse status response: %w", err)
	}
	return res, nil
}

// printLinkTranscript prints one side's transcript compactly (role-tagged
// lines). An empty transcript prints an explicit "no lines" - the
// honest degrade when the daemon's store seam is unwired.
func printLinkTranscript(name string, side voiceLinkSideStatus) {
	emp := side.EmployeeID
	if emp == "" {
		emp = "?"
	}
	fmt.Printf("\n--- transcript %s (%s / %s): %d user / %d assistant ---\n",
		name, emp, side.CallID, side.UserTurns, side.AssistantTurns)
	if len(side.Transcript) == 0 {
		fmt.Println("(no transcript lines)")
		return
	}
	for _, l := range side.Transcript {
		printTranscriptLine(l.Role, l.At, l.Text)
	}
}

// voiceLocalTranscriptResult mirrors the voice.local.transcript wire shape.
type voiceLocalTranscriptResult struct {
	CallID     string `json:"call_id"`
	EmployeeID string `json:"employee_id"`
	SessionID  string `json:"session_id"`
	Lines      []struct {
		Role string `json:"role"`
		Text string `json:"text"`
		At   string `json:"at"`
	} `json:"lines"`
	Found bool `json:"found"`
}

// printLocalCallTranscript prints the call's stored transcript at call
// end (the user-facing "what was said" the live event feed also lost in
// the local-53b347f028f0a43f regression). Reads voice.local.transcript -
// the call store, call-scoped, so a shared session never leaks prior
// calls' history. Best-effort by contract: an RPC error, an old daemon
// without the method, or an unwired store prints the honest "no
// transcript lines" degrade and NEVER changes the run's outcome.
func printLocalCallTranscript(client interface {
	Call(method string, params any) (json.RawMessage, error)
}, callID, employee string) {
	var res voiceLocalTranscriptResult
	raw, err := client.Call("voice.local.transcript", map[string]any{"call_id": callID})
	if err == nil {
		err = json.Unmarshal(raw, &res)
	}
	if err != nil || !res.Found {
		emp := employee
		if res.EmployeeID != "" {
			emp = res.EmployeeID
		}
		fmt.Printf("\n--- transcript (%s / %s) ---\n", emp, callID)
		fmt.Println("(no transcript lines)")
		return
	}
	var userN, asstN int
	for _, l := range res.Lines {
		switch l.Role {
		case "user":
			userN++
		case "assistant":
			asstN++
		}
	}
	emp := res.EmployeeID
	if emp == "" {
		emp = employee
	}
	if emp == "" {
		emp = "?"
	}
	fmt.Printf("\n--- transcript (%s / %s): %d user / %d assistant ---\n",
		emp, res.CallID, userN, asstN)
	if len(res.Lines) == 0 {
		fmt.Println("(no transcript lines)")
		return
	}
	for _, l := range res.Lines {
		printTranscriptLine(l.Role, l.At, l.Text)
	}
}

// printTranscriptLine renders one role-tagged transcript line (shared
// shape of the link/local end-of-call prints: HH:MM:SS <role>: <text>).
func printTranscriptLine(role, at, text string) {
	if len(at) >= 19 {
		at = at[11:19]
	}
	fmt.Printf("%s %s: %s\n", at, role, strings.TrimSpace(text))
}
