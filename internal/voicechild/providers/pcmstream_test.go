package providers

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// frame builds one pcm-stream frame (4-byte BE length + payload).
func frame(payload []byte) []byte {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	return append(hdr[:], payload...)
}

// splitReader serves its byte slice at most one byte per Read, forcing the
// deframer to carry header state across calls.
type splitReader struct {
	b []byte
}

func (s *splitReader) Read(p []byte) (int, error) {
	if len(s.b) == 0 {
		return 0, io.EOF
	}
	if len(p) > 1 {
		p = p[:1]
	}
	p[0] = s.b[0]
	s.b = s.b[1:]
	return 1, nil
}

func TestPCMStreamFrameReaderDeframes(t *testing.T) {
	payload1 := bytes.Repeat([]byte{0x01, 0x02}, 5) // 10 bytes
	payload2 := bytes.Repeat([]byte{0x03, 0x04}, 3) // 6 bytes
	want := append(append([]byte{}, payload1...), payload2...)
	wire := append(frame(payload1), frame(payload2)...)
	var zero [4]byte
	wire = append(wire, zero[:]...) // explicit end-of-stream frame

	// One-byte-at-a-time: headers arrive split across Reads.
	r := newPCMStreamFrameReader(&splitReader{b: wire})
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("deframed bytes %x, want %x", got, want)
	}
}

func TestPCMStreamFrameReaderCleanEOFWithoutTerminator(t *testing.T) {
	// Server may close at a frame boundary without a zero-length frame.
	wire := frame([]byte{1, 2, 3, 4})
	r := newPCMStreamFrameReader(bytes.NewReader(wire))
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Fatalf("got %x, want 01020304", got)
	}
}

func TestPCMStreamFrameReaderRejectsTruncatedHeader(t *testing.T) {
	// A body ending mid-header is a transport error, not silent EOF.
	r := newPCMStreamFrameReader(bytes.NewReader([]byte{0, 0}))
	_, err := io.ReadAll(r)
	if err == nil {
		t.Fatal("truncated frame header must error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestPCMStreamFrameReaderClosePropagates(t *testing.T) {
	src := &trackingReadCloser{Reader: bytes.NewReader(frame([]byte{1, 2}))}
	r := newPCMStreamFrameReader(src)
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !src.closed {
		t.Fatal("Close did not reach the underlying response body")
	}
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (t *trackingReadCloser) Close() error {
	t.closed = true
	return nil
}
