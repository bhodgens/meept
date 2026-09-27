package stt

import (
	"strings"
	"testing"
	"time"
)

// transcribeWithETXTBSYRetry wraps an engine transcribe call for tests
// that exec a mock binary written via os.WriteFile: on Linux overlayfs the
// first exec can race the writeback (ETXTBSY) even after fsync. CI has
// hit this on both whisper and parakeet (2026-09-26/27); every call site
// in the stt tests goes through this retry so the class is fixed once.
func transcribeWithETXTBSYRetry(t *testing.T, transcribe func() (string, error)) string {
	t.Helper()
	var text string
	var err error
	for i := 0; ; i++ {
		text, err = transcribe()
		if err == nil || !strings.Contains(err.Error(), "text file busy") || i >= 5 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	return text
}
