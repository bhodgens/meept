package eval

import (
	"encoding/binary"
	"fmt"
	"os"
)

// readWAVMono16 parses a PCM16 mono WAV file and returns the raw s16le
// sample bytes. Only the committed fixture shape is supported (mono,
// 16-bit, RIFF/WAVE fmt chunk PCM=1); anything else is a named error.
// Sample rate rides the WAV header and is NOT assumed here — the fixture
// set mixes 24 kHz (piper/operator) sources.
func readWAVMono16(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("voice eval: read wav: %w", err)
	}
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, fmt.Errorf("voice eval: %s: not a RIFF/WAVE file", path)
	}
	// Walk chunks to fmt and data.
	i := 12
	var (
		audioFormat  uint16
		numChannels  uint16
		bitsPerSampl uint16
		data         []byte
	)
	for i+8 <= len(b) {
		id := string(b[i : i+4])
		size := binary.LittleEndian.Uint32(b[i+4 : i+8])
		body := b[i+8:]
		if int(size) > len(body) {
			return nil, fmt.Errorf("voice eval: %s: truncated %s chunk", path, id)
		}
		switch id {
		case "fmt ":
			audioFormat = binary.LittleEndian.Uint16(body[0:2])
			numChannels = binary.LittleEndian.Uint16(body[2:4])
			bitsPerSampl = binary.LittleEndian.Uint16(body[14:16])
		case "data":
			data = append(data, body[:size]...)
		}
		i += 8 + int(size)
		if size%2 == 1 {
			i++ // RIFF padding
		}
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("voice eval: %s: no data chunk", path)
	}
	if audioFormat != 1 {
		return nil, fmt.Errorf("voice eval: %s: audio format %d, want PCM(1)", path, audioFormat)
	}
	if numChannels != 1 {
		return nil, fmt.Errorf("voice eval: %s: channels %d, want mono", path, numChannels)
	}
	if bitsPerSampl != 16 {
		return nil, fmt.Errorf("voice eval: %s: bits/sample %d, want 16", path, bitsPerSampl)
	}
	return data, nil
}
