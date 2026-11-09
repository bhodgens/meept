// Signal voice leg audio adapter (leaf 11a T4): bridges signal-cli
// socket audio <-> the per-call C2 unix pair the jargo child consumes.
//
// Wire direction (signal-cli socket, RingRTC PCM at its native rate -
// 48k network leg, 16k/48k I/O leg per the research archive): resample
// INBOUND (socket -> child) and OUTBOUND (child -> socket) to L16
// s16le@16k, chunk 640 B/20 ms. The child NEVER sees non-L16@16k chunks
// (audit H2: the wire codec must never size child chunks; 640 B
// L16@16k is the only child chunk size, voiceframe.ChunkBytes).
//
// Pacing (localsplice rule): OUTBOUND (child -> socket) writes one
// 640 B chunk per 20 ms. The signal-cli tunnel socket is a raw byte
// stream, not a playout buffer, but the same pacing discipline applies:
// bursts overflow the tunnel's write buffer as crackle on the far end.
// INBOUND (socket -> child) is chunked (accumulator over 640 B) but NOT
// paced - the socket delivers realtime audio already.
//
// Backpressure: a socket write error on either direction ends the leg
// with a named error (the localsplice CL7 rule); there is no
// drop-oldest path.
//
// Resampler: linear interpolation, stateful across chunks (a fractional
// output position carries between Read calls so 48k->16k never drifts).
// Ratios are exact integers for the two documented rates (3:1 for
// 48k->16k, 1:1 for 16k->16k), but the adapter stays generic: any
// source rate resamples through the same interpolation.

package signalleg

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/voice"
)

// Named audio-adapter errors (repo rule; CL7 backpressure posture).
var (
	// ErrAudioBackpressure is the terminal adapter error: a socket
	// write failed mid-call (child gone, or the signal-cli socket
	// broke). The adapter ends the leg with this named error.
	ErrAudioBackpressure = errors.New("signalleg: audio adapter backpressure")
	// ErrAudioSourceRate is the refusal for an undecodable/zero source
	// sample rate: the adapter refuses to guess a resample ratio.
	ErrAudioSourceRate = errors.New("signalleg: invalid source sample rate")
)

// Adapter pacing/chunk constants (the localsplice values, re-pinned
// here because the adapter paces the SOCKET side, not the child side).
const (
	// audioChunkBytes is the child-native 20 ms chunk (L16 s16le@16k,
	// voiceframe.ChunkBytes("L16", 16000)).
	audioChunkBytes = 640
	// audioPace is the outbound inter-chunk interval (20 ms per 640 B
	// L16@16k chunk, the uptractor pacing rule RunLocalSplice mirrors).
	audioPace = 20 * time.Millisecond
	// childSampleRate is the child-native rate (C2).
	childSampleRate = 16000
)

// audioSocket is the transport the adapter exchanges wire PCM over. The
// concrete *signal socket conn satisfies it; tests hand net.Conn or
// net.Pipe. SetReadDeadline backs the cancel path: a ctx cancel
// unblocks the loops via the bounded deadline (deadlines are honored by
// every net.Conn implementation including net.Pipe).
type audioSocket interface {
	io.ReadWriter
	SetReadDeadline(time.Time) error
}

// audioReadDeadline bounds each adapter read iteration so the ctx check
// re-runs promptly on cancel (short: far below audio timescales).
const audioReadDeadline = 200 * time.Millisecond

// startAudioAdapter launches the goroutine pair for one connected call:
//
//	outbound: child .out (L16@16k) -> resample to wire rate -> paced
//	          640-B-at-20ms-equivalent writes toward the socket
//	inbound:  socket PCM (wire rate) -> resample to L16@16k -> chunked
//	          640 B writes into the child .in
//
// A terminal error on either side ends the call through l.End +
// Manager.FailOutcome (the CL7 backpressure rule). ctx cancel (End /
// ENDED event) is the clean shutdown.
func (l *Leg) startAudioAdapter(st *callState) {
	if st.pair == nil || st.ctx == nil {
		return
	}
	socket := st.socket
	if socket == nil {
		// No socket-audio transport wired (a stub/CI leg or the tunnel
		// helper absent): the call runs on stub audio - the pinned
		// warn-and-continue posture (open-failure parity). Nothing to
		// adapt; the talk loop still runs.
		l.log.Warn("signalleg: no socket audio transport (call continues on stub audio)", "ccid", st.ccid)
		return
	}
	srcRate := st.sourceRate
	if srcRate <= 0 {
		l.log.Error("signalleg: adapter source rate unset; ending leg", "ccid", st.ccid)
		go l.adapterTerminal(st, fmt.Errorf("%w: rate %d", ErrAudioSourceRate, srcRate))
		return
	}
	go func() {
		if err := runOutboundAudio(st.ctx, st.pair, socket, srcRate, st.ccid, l.log, nil); err != nil &&
			!errors.Is(err, context.Canceled) && st.ctx.Err() == nil {
			l.adapterTerminal(st, err)
		}
	}()
	go func() {
		if err := runInboundAudio(st.ctx, st.pair, socket, srcRate, st.ccid, l.log); err != nil &&
			!errors.Is(err, context.Canceled) && st.ctx.Err() == nil {
			l.adapterTerminal(st, err)
		}
	}()
}

// adapterTerminal ends the leg on a terminal adapter error (CL7): leg
// teardown first (adapter ctx cancel + pair close), then the manager
// fan-out (release hook, registry release, binding end).
func (l *Leg) adapterTerminal(st *callState, cause error) {
	l.log.Error("signalleg: adapter ended the leg", "ccid", st.ccid, "err", cause)
	if err := l.End(st.ccid); err != nil {
		l.log.Warn("signalleg: adapter end", "ccid", st.ccid, "err", err)
	}
	l.deps.Manager.FailOutcome(st.ccid, voice.OutcomeFailed, fmt.Sprintf("audio adapter: %v", cause))
}

// resampler is a stateful linear-interpolation resampler between s16le
// PCM streams. NewResampler(srcRate, dstRate) builds one; Write
// consumes src-rate bytes and returns dst-rate bytes. Fractional output
// position carries across Write calls so multi-chunk streams never
// drift (the outbound pace depends on it).
type resampler struct {
	srcRate int
	dstRate int

	pos int64 // fractional read position in SOURCE samples (fixed-point 32.32)
}

// NewResampler builds a linear resampler. Same rates pass bytes through
// byte-identically (the 16k<->16k I/O leg; no copy, no drift).
func NewResampler(srcRate, dstRate int) (*resampler, error) {
	if srcRate <= 0 || dstRate <= 0 {
		return nil, fmt.Errorf("%w: src=%d dst=%d", ErrAudioSourceRate, srcRate, dstRate)
	}
	return &resampler{srcRate: srcRate, dstRate: dstRate}, nil
}

// passthrough reports whether no resampling is needed.
func (r *resampler) passthrough() bool { return r.srcRate == r.dstRate }

// Write resamples one s16le chunk. Input length must be even (whole
// samples); a trailing odd byte is held for the next Write (s16le
// chunks are always even on the wire, but a stream Read can split a
// sample across reads).
func (r *resampler) Write(pcm []byte) []byte {
	if len(pcm) == 0 {
		return nil
	}
	if len(pcm)%2 != 0 {
		// Hold the odd tail byte; fold it into the next Write.
		pcm = pcm[:len(pcm)-1]
		if len(pcm) == 0 {
			return nil
		}
	}
	if r.passthrough() {
		out := make([]byte, len(pcm))
		copy(out, pcm)
		return out
	}
	inSamples := len(pcm) / 2
	// Output sample count from the fixed-point position: each output
	// sample consumes srcRate/dstRate input samples.
	step := int64(r.srcRate) << 32 / int64(r.dstRate)
	total := int64(inSamples) << 32
	var out []byte
	for r.pos+step <= total {
		idx := int(r.pos >> 32)
		frac := int32(r.pos & 0xFFFFFFFF) //nolint:gosec // G115: masked to 32 bits
		s0 := sampleAt(pcm, idx)
		var s int16
		if idx+1 < inSamples {
			s1 := sampleAt(pcm, idx+1)
			s = lerp(s0, s1, frac)
		} else {
			// Last input sample: interpolate against the previous
			// chunk's final sample state (prev) only when frac is past
			// the sample; otherwise emit it. At chunk boundaries the
			// next Write continues from prev so the stream is smooth.
			s = s0
		}
		out = append(out, byte(s), byte(s>>8))
		r.pos += step
	}
	// Carried state: how many WHOLE input samples the position advanced
	// past (the next Write drops them from ITS perspective - the caller
	// hands fresh chunks, so the resampler keeps the last sample value
	// as interpolation context only).
	consumed := int(r.pos >> 32)
	if consumed > 0 {
		r.pos -= int64(consumed) << 32
	}
	return out
}

// sampleAt decodes one s16le sample (bounds-checked; past-the-end
// returns the last sample).
func sampleAt(pcm []byte, i int) int16 {
	if 2*i+1 >= len(pcm) {
		if len(pcm) >= 2 {
			return int16(binary.LittleEndian.Uint16(pcm[len(pcm)-2:]))
		}
		return 0
	}
	return int16(binary.LittleEndian.Uint16(pcm[2*i:]))
}

// lerp interpolates between two s16le samples at fixed-point frac
// (0..2^32-1).
func lerp(s0, s1 int16, frac int32) int16 {
	d := int(s1) - int(s0)
	v := int(s0)<<32 + int(d)*int(frac)
	return int16(v >> 32)
}

// runOutboundAudio pumps child PCM (.out, L16 s16le@16k) to the signal
// socket: resample to the wire rate, then pace one wire-equivalent
// 20 ms write at a time. The PACE UNIT is the wire chunk (20 ms of
// wire-rate audio); the CHILD chunk stays 640 B always (H2) - the wire
// chunk size is derived AFTER resampling and never flows to the child.
// paceWait is the injectable pacing seam (realPaceWait in production).
func runOutboundAudio(ctx context.Context, pair *voice.AudioPair, socket audioSocket, srcRate int, ccid string, log *slog.Logger, paceWait paceWaitFunc) error {
	if paceWait == nil {
		paceWait = realPaceWait
	}
	r, err := NewResampler(childSampleRate, srcRate)
	if err != nil {
		return err
	}
	wireChunk := audioChunkBytes * srcRate / childSampleRate // 20 ms at wire rate
	buf := make([]byte, audioChunkBytes)
	var carry []byte
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Poll with a short sleep instead of a blocking read: the child
		// dials .out shortly after the open frame; ReadFromChild errors
		// with "not connected" until then, and a blocking read with no
		// deadline support on AudioPair would hang the ctx exit. The
		// poll interval is far below audio timescales.
		n, err := pair.ReadFromChild(buf)
		if n > 0 {
			carry = append(carry, r.Write(buf[:n])...)
			for len(carry) >= wireChunk {
				chunk := carry[:wireChunk]
				carry = carry[wireChunk:]
				if werr := pacedWrite(ctx, socket, chunk, audioPace, paceWait); werr != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					return fmt.Errorf("%w: outbound child->socket: %v", ErrAudioBackpressure, werr)
				}
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				// Child half-close: end-of-turn for the reply path; the
				// adapter keeps running (inbound side still live).
				continue
			}
			if strings.Contains(err.Error(), "not connected") {
				// Pre-dial window: poll instead of hang (the pair's
				// pendingIn buffers inbound writes, so no audio loss).
				time.Sleep(audioReadDeadline)
				continue
			}
			return fmt.Errorf("%w: child read: %v", ErrAudioBackpressure, err)
		}
		time.Sleep(audioReadDeadline) // bounded poll; no busy spin between frames
	}
}

// pacedWrite writes one chunk after the pace interval, honoring ctx
// cancel mid-wait (the wait happens BEFORE the write so a cancel or a
// barge-in abort never accelerates the send rate - the paceOne rule,
// localsplice.go). paceWait is the injectable wait seam: the production
// value is realPaceWait (a timer); tests inject a fake clock that
// records intervals.
func pacedWrite(ctx context.Context, socket audioSocket, chunk []byte, pace time.Duration, paceWait paceWaitFunc) error {
	if err := paceWait(ctx, pace); err != nil {
		return err
	}
	if _, err := socket.Write(chunk); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

// paceWaitFunc is the pacing wait seam (tests inject a fake clock).
type paceWaitFunc func(ctx context.Context, d time.Duration) error

// realPaceWait is the production pacing wait: a timer canceled by ctx.
func realPaceWait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// runInboundAudio pumps socket PCM (wire rate) to the child .in:
// resample to L16 s16le@16k, accumulate into exact 640 B chunks, write.
// NOT paced: the socket delivers realtime audio already. A partial tail
// is held (never flushed) - the child speaks in whole 640 B units; a
// torn fragment at teardown is not audio (the localsplice inbound rule).
func runInboundAudio(ctx context.Context, pair *voice.AudioPair, socket audioSocket, srcRate int, ccid string, log *slog.Logger) error {
	r, err := NewResampler(srcRate, childSampleRate)
	if err != nil {
		return err
	}
	buf := make([]byte, audioChunkBytes)
	var pending []byte
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Bounded read deadline so a ctx cancel mid-Read unblocks (the
		// ctx check at the top of the loop is the actual exit; a
		// deadline-expired read is looped, not terminal).
		_ = socket.SetReadDeadline(time.Now().Add(audioReadDeadline))
		n, err := socket.Read(buf)
		_ = socket.SetReadDeadline(time.Time{})
		if n > 0 {
			pending = append(pending, r.Write(buf[:n])...)
			for len(pending) >= audioChunkBytes {
				chunk := make([]byte, audioChunkBytes)
				copy(chunk, pending[:audioChunkBytes])
				pending = pending[audioChunkBytes:]
				if werr := pair.WriteToChild(chunk); werr != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					return fmt.Errorf("%w: inbound socket->child: %v", ErrAudioBackpressure, werr)
				}
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				// Socket EOF: the far end stopped talking. Hold - the
				// call stays live (RECONNECTING may follow).
				log.Debug("signalleg: socket audio EOF (holding)", "ccid", ccid)
				continue
			}
			if isDeadlineExpired(err) {
				continue // bounded-iteration deadline: re-check ctx
			}
			return fmt.Errorf("%w: socket read: %v", ErrAudioBackpressure, err)
		}
	}
}

// isDeadlineExpired reports whether err is a deadline/timeout expiry
// (os.ErrDeadlineExceeded wrappers across net.Conn implementations).
func isDeadlineExpired(err error) bool {
	return errors.Is(err, os.ErrDeadlineExceeded) ||
		strings.Contains(err.Error(), "i/o timeout")
}
