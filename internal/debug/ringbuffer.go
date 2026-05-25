package debug

// RingBuffer is a fixed-size ring buffer for debug output.
// It overwrites old data when full and provides thread-safe read/write.
type RingBuffer struct {
	data []byte
	size int
	pos  int
	full bool
}

// NewRingBuffer creates a new ring buffer with the given capacity in bytes.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 4096
	}
	return &RingBuffer{
		data: make([]byte, size),
		size: size,
	}
}

// Write appends bytes to the ring buffer, overwriting old data when full.
// It always returns len(p), nil and never errors.
func (r *RingBuffer) Write(p []byte) (int, error) {
	n := len(p)
	for _, b := range p {
		r.data[r.pos] = b
		r.pos = (r.pos + 1) % r.size
		if r.pos == 0 {
			r.full = true
		}
	}
	return n, nil
}

// Read returns the contents of the ring buffer in chronological order.
// The returned slice is a copy; callers may modify it freely.
func (r *RingBuffer) Read() []byte {
	if r.full {
		buf := make([]byte, r.size)
		copy(buf, r.data[r.pos:])
		copy(buf[r.size-r.pos:], r.data[:r.pos])
		return buf
	}
	if r.pos == 0 {
		return nil
	}
	buf := make([]byte, r.pos)
	copy(buf, r.data[:r.pos])
	return buf
}

// Len returns the number of bytes currently stored in the buffer.
func (r *RingBuffer) Len() int {
	if r.full {
		return r.size
	}
	return r.pos
}

// Reset clears the ring buffer.
func (r *RingBuffer) Reset() {
	r.pos = 0
	r.full = false
}
