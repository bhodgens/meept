package debug

import (
	"bytes"
	"testing"
)

func TestRingBufferNew(t *testing.T) {
	rb := NewRingBuffer(1024)
	if rb == nil {
		t.Fatal("expected non-nil ring buffer")
	}
	if rb.Len() != 0 {
		t.Fatalf("expected len 0, got %d", rb.Len())
	}
	if got := rb.Read(); got != nil {
		t.Fatalf("expected nil read from empty buffer, got %q", string(got))
	}
}

func TestRingBufferNewDefaultSize(t *testing.T) {
	rb := NewRingBuffer(0)
	if rb.size != 4096 {
		t.Fatalf("expected default size 4096, got %d", rb.size)
	}
	rb = NewRingBuffer(-1)
	if rb.size != 4096 {
		t.Fatalf("expected default size 4096, got %d", rb.size)
	}
}

func TestRingBufferWriteRead(t *testing.T) {
	rb := NewRingBuffer(64)

	data := []byte("hello world")
	n, err := rb.Write(data)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected %d bytes written, got %d", len(data), n)
	}
	if rb.Len() != len(data) {
		t.Fatalf("expected len %d, got %d", len(data), rb.Len())
	}

	got := rb.Read()
	if !bytes.Equal(got, data) {
		t.Fatalf("expected %q, got %q", string(data), string(got))
	}
}

func TestRingBufferMultipleWrites(t *testing.T) {
	rb := NewRingBuffer(64)

	rb.Write([]byte("hello "))
	rb.Write([]byte("world"))

	got := rb.Read()
	expected := "hello world"
	if string(got) != expected {
		t.Fatalf("expected %q, got %q", expected, string(got))
	}
	if rb.Len() != len(expected) {
		t.Fatalf("expected len %d, got %d", len(expected), rb.Len())
	}
}

func TestRingBufferWrapAround(t *testing.T) {
	rb := NewRingBuffer(8)

	// Write 12 bytes total into a size-8 buffer.
	rb.Write([]byte("12345678"))
	if !rb.full {
		t.Fatal("expected buffer to be full after writing 8 bytes into size-8 buffer")
	}
	rb.Write([]byte("ABCD"))

	got := rb.Read()
	// Should contain the last 8 bytes: "5678ABCD"
	expected := "5678ABCD"
	if string(got) != expected {
		t.Fatalf("expected %q, got %q", expected, string(got))
	}
	if rb.Len() != 8 {
		t.Fatalf("expected len 8, got %d", rb.Len())
	}
}

func TestRingBufferReset(t *testing.T) {
	rb := NewRingBuffer(64)
	rb.Write([]byte("data"))
	rb.Reset()

	if rb.Len() != 0 {
		t.Fatalf("expected len 0 after reset, got %d", rb.Len())
	}
	if rb.full {
		t.Fatal("expected buffer to not be full after reset")
	}
	if got := rb.Read(); got != nil {
		t.Fatalf("expected nil read after reset, got %q", string(got))
	}
}

func TestRingBufferExactFill(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Write([]byte("12345"))

	if !rb.full {
		t.Fatal("expected buffer to be full")
	}
	if rb.Len() != 5 {
		t.Fatalf("expected len 5, got %d", rb.Len())
	}

	got := rb.Read()
	if string(got) != "12345" {
		t.Fatalf("expected %q, got %q", "12345", string(got))
	}
}

func TestRingBufferLargeData(t *testing.T) {
	size := 1024
	rb := NewRingBuffer(size)

	// Write exactly size bytes.
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	rb.Write(data)

	got := rb.Read()
	if !bytes.Equal(got, data) {
		t.Fatal("large data roundtrip mismatch")
	}
}

func TestRingBufferOverwrite(t *testing.T) {
	rb := NewRingBuffer(4)
	// Write 8 bytes into a 4-byte buffer.
	rb.Write([]byte("ABCDEFGH"))

	got := rb.Read()
	// Last 4 bytes: EFGH
	if string(got) != "EFGH" {
		t.Fatalf("expected %q, got %q", "EFGH", string(got))
	}
}
