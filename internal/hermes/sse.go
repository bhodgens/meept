// SSE event streaming for the Hermes gateway (hermes-interop phase 2,
// leaf p2-02 / p2-master.md Contract P2-B).
//
// Two upstream streams are read here:
//
//   - GET  /v1/runs/{id}/events          — run lifecycle events
//   - POST /api/sessions/{id}/chat/stream — assistant.delta,
//     assistant.commentary, tool.started/completed/failed, then a
//     terminal run.completed / run.failed / run.cancelled
//
// Parse rules (standard SSE framing, verified against the upstream
// gateway):
//
//   - ':'-prefixed lines are COMMENTS and are skipped. The upstream
//     emits ': keepalive' every 10s; they must never surface as events.
//   - 'event:' lines set the event name (default "message" per the SSE
//     spec when a frame carries only data).
//   - 'data:' lines accumulate; multiple data lines in one frame are
//     joined with '\n' before dispatch.
//   - a blank line dispatches the accumulated frame.
//
// Lifecycle contract: StreamRunEvents/StreamChat classify non-2xx
// responses into the typed sentinels (401 -> ErrHermesAuth,
// 429 -> ErrHermesBusy, 409 -> ErrHermesSessionForeign, else *APIError)
// BEFORE returning a channel — on error the channel is nil. On success
// the returned channel closes after the terminal run.* event, on ctx
// cancel, or when the upstream closes the stream.
//
// Streams are NOT bounded by the Client's sync-turn http.Client timeout
// (a long-running stream would be killed mid-flight); the caller's ctx is
// the only bound. A dedicated timeout-less client performs the request.
package hermes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Event is one dispatched SSE frame. Type is the event name
// ("assistant.delta", "tool.started", "run.completed", ...); Data is the
// raw payload (multi-line data joined with '\n' per the SSE spec) and is
// valid JSON for all upstream event kinds.
type Event struct {
	Type string
	Data json.RawMessage
}

// sseTerminalEvents are the run lifecycle events that end a stream: the
// event is delivered, then the channel closes.
var sseTerminalEvents = map[string]bool{
	"run.completed": true,
	"run.failed":    true,
	"run.cancelled": true,
}

// sseChanBuffer bounds the event channel. Small buffers make a slow
// consumer apply backpressure to the reader goroutine (which stops
// reading the socket), which is the desired behavior for tool callers.
const sseChanBuffer = 32

// sseMaxLine bounds a single SSE line (one data: line of a large frame).
// 4 MiB is far above any upstream event payload and prevents bufio's
// 64 KiB default from killing a stream on a big tool.completed frame.
const sseMaxLine = 4 << 20

// sseHTTPClient performs stream requests. It deliberately has NO timeout:
// the caller's ctx bounds the stream, not the sync-turn timeout on
// Client.httpClient (which would kill long runs mid-flight).
var sseHTTPClient = &http.Client{}

// StreamRunEvents subscribes to GET /v1/runs/{id}/events and returns the
// run's lifecycle event stream. The channel closes after the terminal
// run.* event, on ctx cancel, or when the upstream ends the stream. A
// non-2xx response returns a typed error and a nil channel.
func (c *Client) StreamRunEvents(ctx context.Context, runID string) (<-chan Event, error) {
	if runID == "" {
		return nil, fmt.Errorf("hermes stream: run id required")
	}
	path := "/v1/runs/" + url.PathEscape(runID) + "/events"
	return c.sseStream(ctx, "GET /v1/runs/{id}/events", http.MethodGet, path, nil)
}

// StreamChat runs a streaming chat turn on an existing session
// (POST /api/sessions/{id}/chat/stream). The stream carries
// assistant.delta / assistant.commentary / tool.* events and ends with a
// terminal run.* event that closes the channel. opts' model/provider ride
// the documented per-request override fields when non-empty. A non-2xx
// response returns a typed error and a nil channel.
func (c *Client) StreamChat(ctx context.Context, sessionID, input string, opts ChatOptions) (<-chan Event, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("hermes stream chat: session id required")
	}
	if input == "" {
		return nil, fmt.Errorf("hermes stream chat: input required")
	}
	payload := map[string]any{"input": input}
	if opts.Model != "" {
		payload["model"] = opts.Model
	}
	if opts.Provider != "" {
		payload["provider"] = opts.Provider
	}
	path := "/api/sessions/" + url.PathEscape(sessionID) + "/chat/stream"
	return c.sseStream(ctx, "POST /api/sessions/{id}/chat/stream", http.MethodPost, path, payload)
}

// sseStream performs the request, classifies non-2xx synchronously (typed
// error, nil channel), and on success starts the reader goroutine that
// parses frames into the returned channel.
func (c *Client) sseStream(ctx context.Context, endpoint, method, path string, in any) (<-chan Event, error) {
	var body *bytes.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("hermes %s: encode request: %w", endpoint, err)
		}
		body = bytes.NewReader(buf)
	}

	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("hermes %s: build request: %w", endpoint, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := sseHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hermes %s: %w", endpoint, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Classify BEFORE any channel exists (contract): typed sentinel
		// or *APIError; the key never appears in the error text.
		cerr := c.classifyError(endpoint, resp)
		respClosing(resp.Body)
		return nil, cerr
	}

	ch := make(chan Event, sseChanBuffer)
	go ssePump(ctx, ch, resp, endpoint)
	return ch, nil
}

// ssePump reads the response body, parses SSE frames, and delivers them
// until a terminal run.* event, ctx cancel, EOF, or a read error. It owns
// resp.Body and always closes both the body and the channel.
func ssePump(ctx context.Context, ch chan<- Event, resp *http.Response, endpoint string) {
	defer close(ch)
	defer respClosing(resp.Body)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), sseMaxLine)

	var (
		eventType string
		dataLines []string
		haveData  bool
	)

	// dispatch sends the accumulated frame (if any) and reports whether
	// the stream is over (terminal event delivered or ctx cancelled).
	dispatch := func() (done bool) {
		if !haveData && eventType == "" {
			return false // blank line with nothing accumulated: no-op
		}
		typ := eventType
		if typ == "" {
			typ = "message" // SSE spec default
		}
		ev := Event{Type: typ, Data: json.RawMessage(strings.Join(dataLines, "\n"))}
		eventType, dataLines, haveData = "", nil, false

		select {
		case ch <- ev:
		case <-ctx.Done():
			return true
		}
		return sseTerminalEvents[ev.Type]
	}

	for scanner.Scan() {
		line := scanner.Text() // ScanLines strips a trailing \r

		if line == "" {
			if dispatch() {
				return
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			// Comment line — upstream keepalive (every 10s). Skipped.
			continue
		}

		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ") // one optional space per SSE spec
		switch field {
		case "event":
			eventType = value
		case "data":
			dataLines = append(dataLines, value)
			haveData = true
		case "id", "retry":
			// Known SSE fields we have no use for; ignore.
		default:
			// Unknown field name: ignored per the SSE spec.
		}

		// A long-running stream must also honor ctx cancel while
		// blocked between frames: check after every line (cheap).
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
	if err := scanner.Err(); err != nil {
		// EOF is a nil error; anything else is an upstream/transport
		// failure mid-stream. The channel closes either way — consumers
		// see the stream end, matching the ctx-cancel path.
		if ctx.Err() == nil {
			hermesDebugLog(fmt.Sprintf("hermes %s: stream read", endpoint), err)
		}
	}
	// Stream ended without a terminal event: flush a trailing frame that
	// arrived without its blank-line terminator (tolerant close).
	dispatch()
}
