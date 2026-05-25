package debug

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// Client is a DAP (Debug Adapter Protocol) JSON-RPC client.
// It communicates with a debug adapter subprocess over stdio using the
// same Content-Length framing as LSP.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu      sync.Mutex
	seq     atomic.Int64
	pending map[int64]chan *DAPResponse

	events chan *DAPEvent
	done   chan struct{}
	logger *slog.Logger
}

// DAPRequest is a DAP protocol request message.
type DAPRequest struct {
	Seq       int    `json:"seq"`
	Type      string `json:"type"`
	Command   string `json:"command"`
	Arguments any    `json:"arguments,omitempty"`
}

// DAPResponse is a DAP protocol response message.
type DAPResponse struct {
	Seq        int64           `json:"seq"`
	Type       string          `json:"type"`
	RequestSeq int64           `json:"request_seq"`
	Success    bool            `json:"success"`
	Command    string          `json:"command"`
	Body       json.RawMessage `json:"body"`
	Message    string          `json:"message,omitempty"`
}

// DAPEvent is a DAP protocol event message.
type DAPEvent struct {
	Seq   int64           `json:"seq"`
	Type  string          `json:"type"`
	Event string          `json:"event"`
	Body  json.RawMessage `json:"body"`
}

// StoppedEventBody represents the body of a DAP "stopped" event.
type StoppedEventBody struct {
	Reason            string `json:"reason"`
	ThreadID          int    `json:"threadId,omitempty"`
	AllThreadsStopped bool   `json:"allThreadsStopped,omitempty"`
	HitBreakpointIDs  []int  `json:"hitBreakpointIds,omitempty"`
}

// OutputEventBody represents the body of a DAP "output" event.
type OutputEventBody struct {
	Category string `json:"category,omitempty"`
	Output   string `json:"output"`
}

// ExitedEventBody represents the body of a DAP "exited" event.
type ExitedEventBody struct {
	ExitCode int `json:"exitCode"`
}

// TerminatedEventBody represents the body of a DAP "terminated" event.
type TerminatedEventBody struct {
	Restart any `json:"restart,omitempty"`
}

// InitializeRequestArguments is the argument for the "initialize" request.
type InitializeRequestArguments struct {
	AdapterID    string `json:"adapterID"`
	PathFormat   string `json:"pathFormat,omitempty"`   // "path" or "uri"
	LinesStartAt1 *bool `json:"linesStartAt1,omitempty"`
	ColumnsStartAt1 *bool `json:"columnsStartAt1,omitempty"`
}

// LaunchRequestArguments is the argument for the "launch" request.
type LaunchRequestArguments struct {
	Program       string            `json:"program"`
	Args          []string          `json:"args,omitempty"`
	Cwd           string            `json:"cwd,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	StopOnEntry   bool              `json:"stopOnEntry,omitempty"`
	NoDebug       bool              `json:"noDebug,omitempty"`
}

// AttachRequestArguments is the argument for the "attach" request.
type AttachRequestArguments struct {
	ProcessID *int   `json:"processId,omitempty"`
	Port      *int   `json:"port,omitempty"`
	Host      string `json:"host,omitempty"`
	Program   string `json:"program,omitempty"`
}

// Source is a DAP Source object identifying a source file.
type Source struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// SourceBreakpoint is a breakpoint specification in a source file.
type SourceBreakpoint struct {
	Line      int    `json:"line"`
	Column    *int   `json:"column,omitempty"`
	Condition string `json:"condition,omitempty"`
	HitCount  *int   `json:"hitCount,omitempty"`
}

// SetBreakpointsArguments is the argument for "setBreakpoints".
type SetBreakpointsArguments struct {
	Source      Source             `json:"source"`
	Breakpoints []SourceBreakpoint `json:"breakpoints,omitempty"`
	SourceModified bool            `json:"sourceModified,omitempty"`
}

// StackTraceArguments is the argument for "stackTrace".
type StackTraceArguments struct {
	ThreadID int `json:"threadId"`
	StartFrame *int `json:"startFrame,omitempty"`
	Levels     *int `json:"levels,omitempty"`
}

// ScopesArguments is the argument for "scopes".
type ScopesArguments struct {
	FrameID int `json:"frameId"`
}

// VariablesArguments is the argument for "variables".
type VariablesArguments struct {
	VariablesReference int    `json:"variablesReference"`
	Filter             string `json:"filter,omitempty"` // "named", "indexed"
	Start              *int   `json:"start,omitempty"`
	Count              *int   `json:"count,omitempty"`
}

// EvaluateArguments is the argument for "evaluate".
type EvaluateArguments struct {
	Expression string `json:"expression"`
	FrameID    *int   `json:"frameId,omitempty"`
	Context    string `json:"context,omitempty"` // "watch", "repl", "hover"
}

// NewClient creates a new DAP client by spawning the given command as a
// subprocess and communicating over its stdin/stdout.
func NewClient(cmd *exec.Cmd) (*Client, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to start adapter: %w", err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReaderSize(stdout, 65536),
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
		logger:  slog.Default().With("component", "dap-client"),
	}

	return c, nil
}

// Start begins the read loop that processes incoming DAP messages.
func (c *Client) Start(ctx context.Context) {
	go c.readLoop(ctx)
}

// readLoop reads DAP messages from the adapter subprocess.
func (c *Client) readLoop(ctx context.Context) {
	defer close(c.events)
	defer close(c.done)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := c.readMessage()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			// EOF means the adapter exited.
			if err == io.EOF {
				if c.logger != nil {
					c.logger.Debug("adapter stdout closed")
				}
				return
			}
			if c.logger != nil {
				c.logger.Error("failed to read DAP message", "error", err)
			}
			return
		}

		c.dispatchMessage(msg)
	}
}

// readMessage reads a single DAP message using Content-Length framing.
func (c *Client) readMessage() (json.RawMessage, error) {
	// Read headers until empty line.
	var contentLength int
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		// Trim both \r\n (Windows/DAP) and \n (bare newline).
		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			break
		}

		if len(line) > 16 && line[:16] == "Content-Length: " {
			_, err := fmt.Sscanf(line[16:], "%d", &contentLength)
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length header: %w", err)
			}
		}
	}

	if contentLength <= 0 {
		return nil, fmt.Errorf("missing or invalid Content-Length header")
	}

	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, buf); err != nil {
		return nil, fmt.Errorf("failed to read message body (%d bytes): %w", contentLength, err)
	}

	return json.RawMessage(buf), nil
}

// dispatchMessage routes a message to the appropriate handler.
func (c *Client) dispatchMessage(msg json.RawMessage) {
	// Peek at the type field to determine message kind.
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg, &header); err != nil {
		if c.logger != nil {
			c.logger.Error("failed to parse DAP message type", "error", err)
		}
		return
	}

	switch header.Type {
	case "response":
		var resp DAPResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			if c.logger != nil {
				c.logger.Error("failed to parse DAP response", "error", err)
			}
			return
		}
		c.handleResponse(&resp)

	case "event":
		var evt DAPEvent
		if err := json.Unmarshal(msg, &evt); err != nil {
			if c.logger != nil {
				c.logger.Error("failed to parse DAP event", "error", err)
			}
			return
		}
		c.handleEvent(&evt)

	default:
		if c.logger != nil {
			c.logger.Warn("unknown DAP message type", "type", header.Type)
		}
	}
}

// handleResponse delivers a response to the waiting caller.
func (c *Client) handleResponse(resp *DAPResponse) {
	c.mu.Lock()
	ch, ok := c.pending[resp.RequestSeq]
	if ok {
		delete(c.pending, resp.RequestSeq)
	}
	c.mu.Unlock()

	if ok {
		select {
		case ch <- resp:
		default:
			// Caller already timed out; discard.
		}
	}
}

// handleEvent sends an event to the events channel.
func (c *Client) handleEvent(evt *DAPEvent) {
	select {
	case c.events <- evt:
	default:
		// Drop oldest event if channel is full.
		select {
		case <-c.events:
		default:
		}
		c.events <- evt
	}
}

// Events returns the channel on which DAP events are delivered.
func (c *Client) Events() <-chan *DAPEvent {
	return c.events
}

// SendRequest sends a DAP request and waits for the response.
func (c *Client) SendRequest(ctx context.Context, command string, args any) (*DAPResponse, error) {
	seq := c.seq.Add(1)

	req := DAPRequest{
		Seq:       int(seq),
		Type:      "request",
		Command:   command,
		Arguments: args,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respCh := make(chan *DAPResponse, 1)
	c.mu.Lock()
	c.pending[seq] = respCh
	c.mu.Unlock()

	if err := c.writeMessage(data); err != nil {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-respCh:
		return resp, nil
	}
}

// writeMessage writes a DAP message with Content-Length framing.
func (c *Client) writeMessage(data []byte) error {
	_, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n", len(data))
	if err != nil {
		return err
	}
	_, err = c.stdin.Write(data)
	return err
}

// Initialize sends the DAP initialize request.
func (c *Client) Initialize(ctx context.Context, adapterID string) error {
	args := InitializeRequestArguments{
		AdapterID:      adapterID,
		PathFormat:     "path",
		LinesStartAt1:  boolPtr(true),
		ColumnsStartAt1: boolPtr(true),
	}

	resp, err := c.SendRequest(ctx, "initialize", args)
	if err != nil {
		return fmt.Errorf("initialize request failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("initialize failed: %s", resp.Message)
	}
	return nil
}

// Launch sends the DAP launch request.
func (c *Client) Launch(ctx context.Context, args LaunchRequestArguments) error {
	resp, err := c.SendRequest(ctx, "launch", args)
	if err != nil {
		return fmt.Errorf("launch request failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("launch failed: %s", resp.Message)
	}
	return nil
}

// Attach sends the DAP attach request.
func (c *Client) Attach(ctx context.Context, args AttachRequestArguments) error {
	resp, err := c.SendRequest(ctx, "attach", args)
	if err != nil {
		return fmt.Errorf("attach request failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("attach failed: %s", resp.Message)
	}
	return nil
}

// SetBreakpoints sends the DAP setBreakpoints request.
func (c *Client) SetBreakpoints(ctx context.Context, args SetBreakpointsArguments) (json.RawMessage, error) {
	resp, err := c.SendRequest(ctx, "setBreakpoints", args)
	if err != nil {
		return nil, fmt.Errorf("setBreakpoints request failed: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("setBreakpoints failed: %s", resp.Message)
	}
	return resp.Body, nil
}

// Continue sends the DAP continue request for the given thread.
func (c *Client) Continue(ctx context.Context, threadID int) error {
	resp, err := c.SendRequest(ctx, "continue", map[string]any{"threadId": threadID})
	if err != nil {
		return fmt.Errorf("continue request failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("continue failed: %s", resp.Message)
	}
	return nil
}

// StepOver sends the DAP next request.
func (c *Client) StepOver(ctx context.Context, threadID int) error {
	resp, err := c.SendRequest(ctx, "next", map[string]any{"threadId": threadID})
	if err != nil {
		return fmt.Errorf("stepOver request failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("stepOver failed: %s", resp.Message)
	}
	return nil
}

// StepIn sends the DAP stepIn request.
func (c *Client) StepIn(ctx context.Context, threadID int) error {
	resp, err := c.SendRequest(ctx, "stepIn", map[string]any{"threadId": threadID})
	if err != nil {
		return fmt.Errorf("stepIn request failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("stepIn failed: %s", resp.Message)
	}
	return nil
}

// StepOut sends the DAP stepOut request.
func (c *Client) StepOut(ctx context.Context, threadID int) error {
	resp, err := c.SendRequest(ctx, "stepOut", map[string]any{"threadId": threadID})
	if err != nil {
		return fmt.Errorf("stepOut request failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("stepOut failed: %s", resp.Message)
	}
	return nil
}

// Evaluate sends the DAP evaluate request.
func (c *Client) Evaluate(ctx context.Context, args EvaluateArguments) (json.RawMessage, error) {
	resp, err := c.SendRequest(ctx, "evaluate", args)
	if err != nil {
		return nil, fmt.Errorf("evaluate request failed: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("evaluate failed: %s", resp.Message)
	}
	return resp.Body, nil
}

// StackTrace sends the DAP stackTrace request.
func (c *Client) StackTrace(ctx context.Context, args StackTraceArguments) (json.RawMessage, error) {
	resp, err := c.SendRequest(ctx, "stackTrace", args)
	if err != nil {
		return nil, fmt.Errorf("stackTrace request failed: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("stackTrace failed: %s", resp.Message)
	}
	return resp.Body, nil
}

// Threads sends the DAP threads request.
func (c *Client) Threads(ctx context.Context) (json.RawMessage, error) {
	resp, err := c.SendRequest(ctx, "threads", nil)
	if err != nil {
		return nil, fmt.Errorf("threads request failed: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("threads failed: %s", resp.Message)
	}
	return resp.Body, nil
}

// Scopes sends the DAP scopes request.
func (c *Client) Scopes(ctx context.Context, frameID int) (json.RawMessage, error) {
	resp, err := c.SendRequest(ctx, "scopes", ScopesArguments{FrameID: frameID})
	if err != nil {
		return nil, fmt.Errorf("scopes request failed: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("scopes failed: %s", resp.Message)
	}
	return resp.Body, nil
}

// Variables sends the DAP variables request.
func (c *Client) Variables(ctx context.Context, args VariablesArguments) (json.RawMessage, error) {
	resp, err := c.SendRequest(ctx, "variables", args)
	if err != nil {
		return nil, fmt.Errorf("variables request failed: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("variables failed: %s", resp.Message)
	}
	return resp.Body, nil
}

// Terminate sends the DAP terminate request.
func (c *Client) Terminate(ctx context.Context) error {
	resp, err := c.SendRequest(ctx, "terminate", nil)
	if err != nil {
		return fmt.Errorf("terminate request failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("terminate failed: %s", resp.Message)
	}
	return nil
}

// Disconnect sends the DAP disconnect request.
func (c *Client) Disconnect(ctx context.Context) error {
	resp, err := c.SendRequest(ctx, "disconnect", nil)
	if err != nil {
		return fmt.Errorf("disconnect request failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("disconnect failed: %s", resp.Message)
	}
	return nil
}

// Close terminates the adapter subprocess and releases resources.
func (c *Client) Close() error {
	var lastErr error

	// Try to close stdin to signal the adapter.
	if err := c.stdin.Close(); err != nil {
		lastErr = err
	}

	// Kill the subprocess.
	if c.cmd.Process != nil {
		if err := c.cmd.Process.Kill(); err != nil {
			if lastErr == nil {
				lastErr = err
			}
		}
		// Reap the process to avoid zombies.
		c.cmd.Wait()
	}

	return lastErr
}

// Done returns a channel that is closed when the read loop exits.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// trimCR removes a trailing carriage return from a line.
func trimCR(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\r' {
		return s[:len(s)-1]
	}
	return s
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(v bool) *bool {
	return &v
}
