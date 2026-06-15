package web

import (
	"fmt"
	"net/http"
	"strings"
)

// writeSSEData writes a properly-framed SSE data event. Per the SSE spec,
// multi-line data must use separate "data:" lines.
func writeSSEData(w http.ResponseWriter, data string) {
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprintf(w, "\n")
}

// handleChatStream handles POST /api/v1/chat/stream with Server-Sent Events.
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	var req struct {
		Message string `json:"message"`
	}
	if err := readJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		s.writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	if s.chatStreamer == nil {
		// Fallback: use non-streaming handler and send as a single chunk.
		if s.handler == nil {
			s.writeError(w, http.StatusServiceUnavailable, "handler not available")
			return
		}
		response, err := s.handler.Chat(r.Context(), req.Message)
		if err != nil {
			writeSSEData(w, "event: error")
			writeSSEData(w, err.Error())
			return
		}
		writeSSEData(w, response)
		fmt.Fprintf(w, "event: done\ndata: \n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	chunks := make(chan string, 64)
	done := make(chan error, 1)

	go func() {
		done <- s.chatStreamer.ChatStream(r.Context(), req.Message, chunks)
	}()

	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				// Channel closed; wait for done signal.
				chunks = nil
				continue
			}
			writeSSEData(w, chunk)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case err := <-done:
			if err != nil {
				fmt.Fprintf(w, "event: error\n")
				writeSSEData(w, err.Error())
			}
			fmt.Fprintf(w, "event: done\ndata: \n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return
		case <-r.Context().Done():
			return
		}
	}
}
