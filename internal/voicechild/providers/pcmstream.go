package providers

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// pcmStreamFrameReader strips the pcm-stream route's codec-frame envelope
// from a response body, yielding the concatenated raw s16le mono PCM that
// the shared resampler expects.
//
// Wire contract (verified against socrates:8001 POST
// /v1/audio/speech/pcm-stream and the server's /openapi.json, 2026-11-18):
//
//	[4-byte big-endian uint32 length][length bytes int16 PCM] ... [0-length frame]
//
// A zero-length frame marks the end of the stream. The length is in BYTES,
// not samples. The route is the only one of the qwen3 server's speech
// routes that delivers token-level incremental audio; the OpenAI-shaped
// /v1/audio/speech route buffers the whole utterance before its body starts,
// which is the ~1-2 s first-byte latency this reader exists to remove.
//
// Frames may arrive split across arbitrary network reads (a header can land
// in one Read and its payload in the next), so the reader carries partial
// header state across calls. It never buffers a whole frame: the first
// payload bytes are handed to the caller as soon as they arrive, preserving
// the token-level first-audio win.
type pcmStreamFrameReader struct {
	src       io.Reader
	header    [4]byte
	headerN   int // bytes of the current length header already read
	remaining int // payload bytes left in the current frame
	err       error
}

func newPCMStreamFrameReader(src io.Reader) *pcmStreamFrameReader {
	return &pcmStreamFrameReader{src: src}
}

// Read implements io.Reader. It delivers at most one frame's worth of
// payload per call and never crosses a frame boundary, so the resampler's
// fixed 640-byte reads stay inside a single frame until that frame drains.
func (r *pcmStreamFrameReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.err != nil {
		return 0, r.err
	}
	for {
		if r.remaining == 0 {
			n, err := readHeader(r.src, &r.header, &r.headerN)
			if err != nil {
				if errors.Is(err, io.EOF) && r.headerN == 0 {
					// Clean end at a frame boundary (the server may
					// close without an explicit zero-length frame).
					r.err = io.EOF
					return 0, io.EOF
				}
				r.err = err
				return 0, err
			}
			if n == 0 {
				// Explicit zero-length end-of-stream frame.
				r.err = io.EOF
				return 0, io.EOF
			}
			r.remaining = int(n)
		}

		want := len(p)
		if want > r.remaining {
			want = r.remaining
		}
		n, err := r.src.Read(p[:want])
		r.remaining -= n
		if n > 0 {
			return n, nil
		}
		if err != nil {
			r.err = err
			return 0, err
		}
		// A 0-byte, nil-error Read is legal but makes no progress; loop
		// rather than return (0, nil), which io.Reader forbids.
	}
}

// readHeader accumulates exactly four bytes across Reads and returns the
// big-endian length. It returns (0, nil) for a zero-length terminator and
// io.ErrUnexpectedEOF when the body ends mid-header.
func readHeader(src io.Reader, dst *[4]byte, have *int) (uint32, error) {
	for *have < len(dst) {
		n, err := src.Read(dst[*have:])
		*have += n
		if err != nil {
			if errors.Is(err, io.EOF) && *have == 0 {
				return 0, io.EOF
			}
			if errors.Is(err, io.EOF) {
				return 0, fmt.Errorf("pcm-stream: truncated frame header: %w", io.ErrUnexpectedEOF)
			}
			return 0, fmt.Errorf("pcm-stream: read frame header: %w", err)
		}
	}
	*have = 0
	return binary.BigEndian.Uint32(dst[:]), nil
}

// Close releases the underlying response body. It is what lets the shared
// resampler's Close (which type-asserts its source to io.ReadCloser) tear
// down the HTTP stream on barge-in instead of leaking the connection.
func (r *pcmStreamFrameReader) Close() error {
	if rc, ok := r.src.(io.Closer); ok {
		return rc.Close()
	}
	return nil
}

var _ io.ReadCloser = (*pcmStreamFrameReader)(nil)
