package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/queue"
)

// Test helpers for the atomic-compaction wiring pins. All test-only.

func testDiscardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func jsonMarshalForTest(v any) ([]byte, error) {
	return json.Marshal(v)
}

func newL1JobForTest(payload []byte) *queue.Job {
	return &queue.Job{Type: MemoryJobL1Extract, Payload: payload}
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
