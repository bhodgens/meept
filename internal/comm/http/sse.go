package http

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEWriter wraps an http.ResponseWriter for Server-Sent Events streaming.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter creates an SSE writer. It sets the required SSE headers.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	return &SSEWriter{w: w, flusher: flusher}, nil
}

// SendEvent writes an SSE event with the given event type and data.
// The data is JSON-encoded.
func (s *SSEWriter) SendEvent(event string, data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal SSE data: %w", err)
	}

	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, jsonData); err != nil {
		return fmt.Errorf("write SSE event: %w", err)
	}
	s.flusher.Flush()
	return nil
}

// SendComment writes an SSE comment (used as keep-alive heartbeat).
func (s *SSEWriter) SendComment() error {
	if _, err := fmt.Fprint(s.w, ": heartbeat\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// SendError writes an SSE event with error information.
func (s *SSEWriter) SendError(errMsg string) error {
	return s.SendEvent("error", map[string]string{"error": errMsg})
}

// Write implements io.Writer for raw writes.
func (s *SSEWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if err != nil {
		return n, err
	}
	s.flusher.Flush()
	return n, nil
}

// CloseWithFinalEvent sends a final done event.
func (s *SSEWriter) CloseWithFinalEvent() error {
	return s.SendEvent("done", map[string]string{KeyStatus: "complete"})
}
