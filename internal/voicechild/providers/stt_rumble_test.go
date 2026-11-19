package providers

// F1 gate: the Rumble adapter buffers one voiced PCM window, posts it to a
// warm vLLM transcription server as base64 audio, and emits exactly one
// Final transcript. The fake server proves the keyless request shape.
// RED until leaf 01 lands newRumbleSTT.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
)

func TestRumbleSTTStream(t *testing.T) {
	var sawFile, sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/audio/transcriptions") {
			t.Errorf("request path = %q, want /v1/audio/transcriptions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			sawAuth = true
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if _, ok := body["file"]; ok {
			sawFile = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"text":"hello there"}`)
	}))
	defer srv.Close()

	stt, err := newRumbleSTT(ProviderConfig{
		Vendor:     "rumble",
		Model:      "Reson8/Rumble-2-Flash",
		BaseURL:    srv.URL,
		SampleRate: 16000,
	})
	if err != nil {
		t.Fatalf("newRumbleSTT: %v", err)
	}
	var _ voicechild.STTProvider = stt

	pcm := make(chan []byte)
	go func() {
		for range 40 {
			pcm <- make([]byte, 640)
		}
		close(pcm)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	select {
	case tr, ok := <-out:
		if !ok {
			t.Fatal("stream closed with no transcript")
		}
		if tr.Text != "hello there" || !tr.Final {
			t.Fatalf("transcript = %+v, want {hello there true}", tr)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for transcript")
	}
	if !sawFile {
		t.Error("request omitted base64 audio field 'file'")
	}
	if sawAuth {
		t.Error("keyless Rumble adapter sent an Authorization header")
	}
}
