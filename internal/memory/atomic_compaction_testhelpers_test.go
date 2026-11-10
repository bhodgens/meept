package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/queue"
)

// Test helpers for the atomic-compaction wiring pins. All test-only.

func testDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func jsonMarshalForTest(v any) ([]byte, error) {
	return json.Marshal(v)
}

func newL1JobForTest(payload []byte) *queue.Job {
	return &queue.Job{Type: MemoryJobL1Extract, Payload: payload}
}

func countNonEmptyLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func joinL0Conversation(recs []l0Record) string {
	return strings.Join(formatL0Conversation(recs), "\n")
}

// ambientPromptForTest reproduces the exact prompt shape handleL1Extract
// assembles for a raw (uncompacted) window, so the disabled-posture test can
// compare byte-for-byte.
func ambientPromptForTest(window string) string {
	return fmt.Sprintf(ambientExtractionPromptTemplate, window)
}

var _ = context.Background
