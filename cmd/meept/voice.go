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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
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
	var (
		employee string
		label    string
		inPath   string
		outPath  string
		list     bool
	)

	cmd := &cobra.Command{
		Use:   "local",
		Short: "open a local voice call (mic/speaker, or --in/--out files)",
		Long: `Hold a voice conversation with an employee on this machine -
no carrier, no cloud. With --in/--out the call reads mic input from a
WAV file and writes the reply to a WAV file; without them the mic is
captured via ffmpeg and the reply plays through the speaker.

Examples:
  meept voice local --list                        # employees you can call
  meept voice local --employee eve --in q.wav --out a.wav
  meept voice local --employee eve --label desk   # live mic + speaker`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				return runVoiceLocalList()
			}
			if employee == "" {
				return fmt.Errorf("--employee is required (or use --list)")
			}
			return runVoiceLocal(employee, label, inPath, outPath)
		},
	}

	cmd.Flags().StringVar(&employee, "employee", "", "employee id to call (required unless --list)")
	cmd.Flags().StringVar(&label, "label", "", "session label (default: hostname)")
	cmd.Flags().StringVar(&inPath, "in", "", "read mic input from this WAV file (file mode)")
	cmd.Flags().StringVar(&outPath, "out", "", "write the reply to this WAV file (file mode)")
	cmd.Flags().BoolVar(&list, "list", false, "list employees available for local calls")
	return cmd
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
// the call ends (daemon hangup, SIGINT, or file exhaustion).
func runVoiceLocal(employee, label, inPath, outPath string) error {
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
		err = runVoiceLocalLive(ctx, open)
	}

	// The call ends here no matter how the loop exited (the daemon may
	// have reaped it already; End is idempotent so this is safe).
	if _, endErr := client.Call("voice.local.end", map[string]any{"call_id": open.CallID}); endErr != nil && err == nil {
		err = fmt.Errorf("voice.local.end: %w", endErr)
	}
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
// transcript local-2d84c384fd93f447). The bound is the leaf's ~150 ms
// ceiling: 16000 samples/s x 2 bytes x 150 ms = 4800 B (the daemon paces
// 640 B per 20 ms, so 7.5 chunks). drainLiveSpeaker flushes the sink as
// soon as its backlog reaches the bound; the steady-state overshoot is
// one read (liveSpeakerReadChunk) on top. The hard recall of what the
// player ALREADY holds is tts.AudioPlayer's Pause (Play pauses the
// previous burst; the Player seam's Clear/Stop pauses the current one) -
// that is what kills the tail. Socket backpressure then stalls the
// daemon outbound loop, which stops writing at the barge-in latch
// anyway.
const liveMaxPendingAudioBytes = 16000 * 2 * 150 / 1000 // 4800 B = 150 ms @ L16@16k mono

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
		boundHands int
		bytesTotal int64
	)
	buf := make([]byte, liveSpeakerReadChunk)
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
					boundHands++
					if ferr := s.Flush(); ferr != nil {
						done <- ferr
						return
					}
				} else if ferr := s.Flush(); ferr != nil {
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
				fmt.Fprintf(os.Stderr, "voice trace: bytes=%d gap_reads=%d read_gap_avg=%v read_gap_max=%v bound_flushes=%d\n",
					bytesTotal, gapSamples, avg, maxReadGap, boundHands)
			}
			done <- nil
			return
		}
	}
}

func runVoiceLocalLive(ctx context.Context, open voiceLocalOpenResult) error {
	cap, err := voice.NewRawMicCapture("", "")
	if err != nil {
		return err
	}
	defer cap.Close() //nolint:errcheck // teardown; ffmpeg reaped via ctx cancel

	player := voice.NewOtoPlayer()
	sink := voice.NewSpeakerSink(player, 0)

	inConn, err := net.Dial("unix", open.InPath)
	if err != nil {
		return fmt.Errorf("dial mic socket %s: %w", open.InPath, err)
	}
	defer inConn.Close() //nolint:errcheck // teardown

	outConn, err := net.Dial("unix", open.OutPath)
	if err != nil {
		return fmt.Errorf("dial speaker socket %s: %w", open.OutPath, err)
	}
	defer outConn.Close() //nolint:errcheck // teardown

	micDone := make(chan error, 1)
	spkDone := make(chan error, 1)
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
	go drainLiveSpeaker(outConn, sink, spkDone)
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

// voiceCallStatusRow mirrors the voice.local.status row shape (CL8).
type voiceCallStatusRow struct {
	CallID       string `json:"call_id"`
	EmployeeID   string `json:"employee_id"`
	Target       string `json:"target"`
	SinceRFC3339 string `json:"since_rfc3339"`
	Leg          string `json:"leg"`
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
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", leg, r.CallID, r.EmployeeID, r.Target, since)
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

// linkFlags is the validated `voice link` flag set (pure validation,
// unit-tested without a daemon).
type linkFlags struct {
	a       string
	b       string
	seconds int
	monitor bool
}

// validate reports the flag errors the leaf pins: --a / --b required,
// a==b refused EARLY (before any RPC), seconds <= 0 refused.
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
	return nil
}

func newVoiceLinkCmd() *cobra.Command {
	var (
		a       string
		b       string
		seconds int
		monitor bool
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

Examples:
  meept voice link --a companion --b secretary --seconds 90
  meept voice link --a companion --b secretary --monitor --seconds 60`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f := linkFlags{a: a, b: b, seconds: seconds, monitor: monitor}
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

	// Audible monitor: dial the daemon's tap sockets and play BOTH
	// directions through the operator's speakers (listen-only - nothing
	// is ever written back, the .in mic side stays untouched). Best-
	// effort by contract: a dial failure only costs the speaker tap,
	// never the link run.
	if f.monitor {
		startVoiceLinkMonitor(open, f.a, f.b)
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
// its OWN OtoPlayer + SpeakerSink + drainLiveSpeaker loop, reusing the
// live-local machinery: two oto instances overlap safely in oto's mixer
// (the player.go overlap contract - Play never pauses predecessors), so
// A and B audible streams mix together.
//
// Best-effort by contract: a dial failure disables that side with a
// warning, never aborts the link run; the loops die on socket EOF when
// the link closes (tap conns close with the bridge's teardown).
func startVoiceLinkMonitor(open voiceLinkOpenResult, aEmp, bEmp string) {
	dialSide := func(label, emp, path string) {
		if path == "" {
			fmt.Fprintf(os.Stderr, "monitor %s: daemon returned no tap path (speakers off for that side)\n", label)
			return
		}
		// Open the audio context BEFORE dialing the tap: the tee starts
		// flowing the instant the dial lands, and the lazy oto context
		// init (~0.3 s: device open + ready wait, plus CoreAudio's own
		// first-start latency) stalled the first writes and dropped the
		// greeting burst behind an 8 KB socket buffer (live
		// link-d52f032b6e010882). One silent 20 ms burst = pure warm-up,
		// inaudible.
		player := voice.NewOtoPlayer()
		if werr := player.Play(make([]byte, 640)); werr != nil {
			fmt.Fprintf(os.Stderr, "monitor %s: speaker warm-up failed: %v (speakers off for that side)\n", label, werr)
			return
		}
		conn, err := net.Dial("unix", path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "monitor %s: dial %s: %v (speakers off for that side)\n", label, path, err)
			return
		}
		fmt.Printf("monitor %s: tapping %s via speakers\n", label, emp)
		sink := voice.NewSpeakerSink(player, 0)
		done := make(chan error, 1)
		// First-byte announcement: the operator sees the side start to
		// speak exactly when tee audio lands (not at dial time).
		go drainLiveSpeaker(&monitorFirstByteReader{r: conn, label: label, emp: emp}, sink, done)
		go func() {
			// The drain loop exits on socket EOF/error once the daemon
			// closes the tap; release the audio device on the way out.
			<-done
			_ = conn.Close() //nolint:errcheck // teardown
			_ = sink.Close()
		}()
	}
	dialSide("a", aEmp, open.MonitorAPath)
	dialSide("b", bEmp, open.MonitorBPath)
}

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
		at := l.At
		if len(at) >= 8 {
			at = at[11:19]
		}
		fmt.Printf("%s %s: %s\n", at, l.Role, strings.TrimSpace(l.Text))
	}
}
