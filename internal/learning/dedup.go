package learning

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
)

// IsDuplicate checks whether newExample's Instruction already exists in the
// given JSONL file by comparing SHA-256 hashes. A missing file is treated as
// "no duplicates" (not an error).
func IsDuplicate(newExample TrainingExample, existingFile string) (bool, error) {
	newHash := sha256.Sum256([]byte(newExample.Instruction))

	f, err := os.Open(existingFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("learning: open existing file %s: %w", existingFile, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Allow long lines (up to 1 MB) for large training examples.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var existing TrainingExample
		if err := json.Unmarshal(line, &existing); err != nil {
			// Skip malformed lines rather than failing.
			continue
		}
		existingHash := sha256.Sum256([]byte(existing.Instruction))
		if bytes.Equal(newHash[:], existingHash[:]) {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("learning: scan %s: %w", existingFile, err)
	}
	return false, nil
}
