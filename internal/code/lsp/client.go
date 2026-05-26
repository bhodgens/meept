package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/internal/code/lsp/transport"
)

// ErrNotFound is returned when the LSP server returns no result for a query.
var ErrNotFound = errors.New("not found")

// Client is a JSON-RPC client for communicating with an LSP server.
type Client struct {
	transport transport.Transport
	mu        sync.Mutex
	nextID    atomic.Int64
	pending   map[int64]chan *JSONRPCResponse
	handlers  map[string]NotificationHandler
	caps      ServerCapabilities
	rootURI   string
	running   atomic.Bool
	done      chan struct{}
}

// NotificationHandler handles server notifications.
type NotificationHandler func(method string, params json.RawMessage)

// NewClient creates a new LSP client.
func NewClient(t transport.Transport) *Client {
	c := &Client{
		transport: t,
		pending:   make(map[int64]chan *JSONRPCResponse),
		handlers:  make(map[string]NotificationHandler),
		done:      make(chan struct{}),
	}
	c.running.Store(true)
	return c
}

// Start begins processing messages from the server.
func (c *Client) Start(ctx context.Context) {
	c.running.Store(true)
	go c.readLoop(ctx)
}

// readLoop reads messages from the transport.
func (c *Client) readLoop(ctx context.Context) {
	defer close(c.done)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, err := c.transport.Read(ctx)
		if err != nil {
			// Check if context was cancelled
			select {
			case <-ctx.Done():
				return
			default:
			}
			// Transport error, exit loop
			return
		}

		// Try to parse as response first
		var response JSONRPCResponse
		if err := json.Unmarshal(data, &response); err == nil && response.ID != nil {
			c.handleResponse(&response)
			continue
		}

		// Try to parse as notification
		var request JSONRPCRequest
		if err := json.Unmarshal(data, &request); err == nil && request.ID == nil {
			c.handleNotification(request.Method, request.Params)
		}
	}
}

func (c *Client) handleResponse(resp *JSONRPCResponse) {
	// Convert ID to int64
	var id int64
	switch v := resp.ID.(type) {
	case float64:
		id = int64(v)
	case int64:
		id = v
	case int:
		id = int64(v)
	default:
		return
	}

	c.mu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()

	if ok {
		// Non-blocking send in case receiver has timed out or been cancelled
		select {
		case ch <- resp:
		default:
			// Response channel abandoned, discard response
		}
	}
}

func (c *Client) handleNotification(method string, params json.RawMessage) {
	c.mu.Lock()
	handler, ok := c.handlers[method]
	c.mu.Unlock()

	if ok {
		handler(method, params)
	}
}

// OnNotification registers a handler for server notifications.
func (c *Client) OnNotification(method string, handler NotificationHandler) {
	c.mu.Lock()
	c.handlers[method] = handler
	c.mu.Unlock()
}

// Call makes a JSON-RPC request and waits for the response.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	var paramsRaw json.RawMessage
	if params != nil {
		var err error
		paramsRaw, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsRaw,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respCh := make(chan *JSONRPCResponse, 1)
	c.mu.Lock()
	c.pending[id] = respCh
	c.mu.Unlock()

	if err := c.transport.Write(ctx, data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// Notify sends a notification (no response expected).
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	var paramsRaw json.RawMessage
	if params != nil {
		var err error
		paramsRaw, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsRaw,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	return c.transport.Write(ctx, data)
}

// Initialize initializes the LSP connection.
func (c *Client) Initialize(ctx context.Context, rootURI string) error {
	c.rootURI = rootURI

	params := InitializeParams{
		ProcessID: os.Getpid(),
		RootURI:   rootURI,
		Capabilities: ClientCapabilities{
			TextDocument: TextDocumentClientCapabilities{
				Synchronization: TextDocumentSyncClientCapabilities{
					DynamicRegistration: false,
					DidSave:             true,
				},
			},
		},
	}

	result, err := c.Call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	var initResult InitializeResult
	if err := json.Unmarshal(result, &initResult); err != nil {
		return fmt.Errorf("failed to parse initialize result: %w", err)
	}

	c.caps = initResult.Capabilities

	// Send initialized notification
	if err := c.Notify(ctx, "initialized", struct{}{}); err != nil {
		return fmt.Errorf("initialized notification failed: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the server.
func (c *Client) Shutdown(ctx context.Context) error {
	_, err := c.Call(ctx, "shutdown", nil)
	if err != nil {
		return err
	}

	return c.Notify(ctx, "exit", nil)
}

// Close closes the client and transport.
func (c *Client) Close() error {
	if !c.running.CompareAndSwap(true, false) {
		return nil // already closed
	}
	return c.transport.Close()
}

// Capabilities returns the server's capabilities.
func (c *Client) Capabilities() ServerCapabilities {
	return c.caps
}

// RootURI returns the workspace root URI.
func (c *Client) RootURI() string {
	return c.rootURI
}

// GotoDefinition finds the definition of a symbol at a position.
func (c *Client) GotoDefinition(ctx context.Context, uri string, line, char int) ([]Location, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: char},
	}

	result, err := c.Call(ctx, "textDocument/definition", params)
	if err != nil {
		return nil, err
	}

	// Result can be Location, []Location, or []LocationLink
	var locations []Location
	if err := json.Unmarshal(result, &locations); err != nil {
		// Try single location
		var loc Location
		if err := json.Unmarshal(result, &loc); err != nil {
			return nil, fmt.Errorf("failed to parse definition result: %w", err)
		}
		locations = []Location{loc}
	}

	return locations, nil
}

// FindReferences finds all references to a symbol.
func (c *Client) FindReferences(ctx context.Context, uri string, line, char int, includeDecl bool) ([]Location, error) {
	params := ReferenceParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: uri},
			Position:     Position{Line: line, Character: char},
		},
		Context: ReferenceContext{IncludeDeclaration: includeDecl},
	}

	result, err := c.Call(ctx, "textDocument/references", params)
	if err != nil {
		return nil, err
	}

	var locations []Location
	if err := json.Unmarshal(result, &locations); err != nil {
		return nil, fmt.Errorf("failed to parse references result: %w", err)
	}

	return locations, nil
}

// Hover gets hover information at a position.
func (c *Client) Hover(ctx context.Context, uri string, line, char int) (*Hover, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: char},
	}

	result, err := c.Call(ctx, "textDocument/hover", params)
	if err != nil {
		return nil, err
	}

	if len(result) == 0 || string(result) == "null" {
		return nil, ErrNotFound
	}

	var hover Hover
	if err := json.Unmarshal(result, &hover); err != nil {
		return nil, fmt.Errorf("failed to parse hover result: %w", err)
	}

	return &hover, nil
}

// WorkspaceSymbols searches for symbols in the workspace.
func (c *Client) WorkspaceSymbols(ctx context.Context, query string) ([]SymbolInformation, error) {
	params := WorkspaceSymbolParams{Query: query}

	result, err := c.Call(ctx, "workspace/symbol", params)
	if err != nil {
		return nil, err
	}

	var symbols []SymbolInformation
	if err := json.Unmarshal(result, &symbols); err != nil {
		return nil, fmt.Errorf("failed to parse workspace symbols result: %w", err)
	}

	return symbols, nil
}

// DocumentSymbols gets all symbols in a document.
func (c *Client) DocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	params := struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}

	result, err := c.Call(ctx, "textDocument/documentSymbol", params)
	if err != nil {
		return nil, err
	}

	// Try DocumentSymbol first
	var docSymbols []DocumentSymbol
	if err := json.Unmarshal(result, &docSymbols); err == nil && len(docSymbols) > 0 {
		return docSymbols, nil
	}

	// Try SymbolInformation
	var symInfo []SymbolInformation
	if err := json.Unmarshal(result, &symInfo); err == nil {
		// Convert to DocumentSymbol
		for _, si := range symInfo {
			docSymbols = append(docSymbols, DocumentSymbol{
				Name:           si.Name,
				Kind:           si.Kind,
				Range:          si.Location.Range,
				SelectionRange: si.Location.Range,
			})
		}
		return docSymbols, nil
	}

	return nil, fmt.Errorf("failed to parse document symbols result")
}

// Rename renames a symbol at a given position.
func (c *Client) Rename(ctx context.Context, uri string, line, char int, newName string) (*WorkspaceEdit, error) {
	params := RenameParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: char},
		NewName:      newName,
	}

	result, err := c.Call(ctx, "textDocument/rename", params)
	if err != nil {
		return nil, err
	}

	if len(result) == 0 || string(result) == "null" {
		return nil, nil
	}

	var edit WorkspaceEdit
	if err := json.Unmarshal(result, &edit); err != nil {
		return nil, fmt.Errorf("failed to parse rename result: %w", err)
	}

	return &edit, nil
}

// TypeDefinition finds the type definition of a symbol at a position.
func (c *Client) TypeDefinition(ctx context.Context, uri string, line, char int) ([]Location, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: char},
	}

	result, err := c.Call(ctx, "textDocument/typeDefinition", params)
	if err != nil {
		return nil, err
	}

	var locations []Location
	if err := json.Unmarshal(result, &locations); err != nil {
		// Try single location
		var loc Location
		if err := json.Unmarshal(result, &loc); err != nil {
			return nil, fmt.Errorf("failed to parse type definition result: %w", err)
		}
		locations = []Location{loc}
	}

	return locations, nil
}

// Implementation finds implementations of a symbol at a position.
func (c *Client) Implementation(ctx context.Context, uri string, line, char int) ([]Location, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: char},
	}

	result, err := c.Call(ctx, "textDocument/implementation", params)
	if err != nil {
		return nil, err
	}

	var locations []Location
	if err := json.Unmarshal(result, &locations); err != nil {
		// Try single location
		var loc Location
		if err := json.Unmarshal(result, &loc); err != nil {
			return nil, fmt.Errorf("failed to parse implementation result: %w", err)
		}
		locations = []Location{loc}
	}

	return locations, nil
}

// CodeActions retrieves available code actions for a range.
func (c *Client) CodeActions(ctx context.Context, uri string, line, char int) ([]CodeAction, error) {
	params := CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range: Range{
			Start: Position{Line: line, Character: char},
			End:   Position{Line: line, Character: char},
		},
		Context: CodeActionContext{},
	}

	result, err := c.Call(ctx, "textDocument/codeAction", params)
	if err != nil {
		return nil, err
	}

	var actions []CodeAction
	if err := json.Unmarshal(result, &actions); err != nil {
		return nil, fmt.Errorf("failed to parse code actions result: %w", err)
	}

	return actions, nil
}

// Formatting formats a document.
func (c *Client) Formatting(ctx context.Context, uri string) ([]TextEdit, error) {
	params := struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Options      FormattingOptions      `json:"options"`
	}{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Options: FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	result, err := c.Call(ctx, "textDocument/formatting", params)
	if err != nil {
		return nil, err
	}

	if len(result) == 0 || string(result) == "null" {
		return nil, nil
	}

	var edits []TextEdit
	if err := json.Unmarshal(result, &edits); err != nil {
		return nil, fmt.Errorf("failed to parse formatting result: %w", err)
	}

	return edits, nil
}

// WaitForExit waits for the client to stop.
func (c *Client) WaitForExit(timeout time.Duration) error {
	select {
	case <-c.done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for client exit")
	}
}

// Ensure Client implements io.Closer
var _ io.Closer = (*Client)(nil)
