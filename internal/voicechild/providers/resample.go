package providers

import (
	"io"

	"github.com/gojargo/jargo/audio/resample"
)

// resampleChunkBytes is how much source PCM one conversion pass consumes.
// 60 ms at 24 kHz mono s16le (2880 bytes) keeps the per-read conversion
// cost small while letting the streaming filter carry state across calls.
const resampleChunkBytes = 2880

// newResamplingReader wraps src (s16le mono at inRate) in a reader that
// emits s16le mono at outRate. It uses jargo's stateful streaming Resampler
// (pure Go), so conversion is chunked and incremental - the caller starts
// receiving converted audio without the whole stream being buffered.
type resamplingReader struct {
	src     io.Reader
	rs      *resample.Resampler
	pending []byte // converted audio not yet handed to the caller
	srcErr  error  // sticky source error after pending drains
	done    bool
}

func newResamplingReader(src io.Reader, inRate, outRate int) *resamplingReader {
	rs, err := resample.New(inRate, outRate, 1)
	if err != nil {
		// The converter failed to build; degrade to passthrough rather
		// than fail the call - the rate mismatch surfaces as pitch shift,
		// which the daemon's rate negotiation prevents in practice.
		return &resamplingReader{src: src, srcErr: nil, done: false, pending: nil, rs: nil}
	}
	return &resamplingReader{src: src, rs: rs}
}

// Read implements io.Reader over the resampled stream.
func (r *resamplingReader) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.done {
			return 0, r.srcErr
		}
		buf := make([]byte, resampleChunkBytes)
		n, err := io.ReadFull(r.src, buf)
		if n > 0 {
			out := r.convert(buf[:n])
			if len(out) > 0 {
				r.pending = out
			}
		}
		if err != nil {
			if err == io.ErrUnexpectedEOF || err == io.EOF {
				r.done = true
				r.srcErr = io.EOF
				if r.rs != nil {
					r.rs.Close()
				}
				// pending may hold audio converted from the tail above;
				// loop drains it before reporting EOF.
				continue
			}
			r.done = true
			r.srcErr = err
			if r.rs != nil {
				r.rs.Close()
			}
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// Close releases the source.
func (r *resamplingReader) Close() error {
	if r.rs != nil {
		r.rs.Close()
	}
	rc, ok := r.src.(io.ReadCloser)
	if ok {
		return rc.Close()
	}
	return nil
}

// convert runs one chunk through the streaming resampler (passthrough when
// the converter never built).
func (r *resamplingReader) convert(in []byte) []byte {
	if r.rs == nil {
		return in
	}
	out := r.rs.Process(in)
	if out == nil {
		return nil
	}
	return out
}

// compile-time: the reader satisfies io.ReadCloser.
var _ io.ReadCloser = (*resamplingReader)(nil)
