// Package tui provides the terminal user interface for meept.
package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
	"github.com/caimlas/meept/pkg/models"
)

// RPCClient is a JSON-RPC client for communicating with the meept daemon.
type RPCClient struct {
	socketPath string
	conn       net.Conn
	reader     *bufio.Reader
	writer     io.Writer
	mu         sync.Mutex
	connected  bool
	timeout    time.Duration
	nextID     int64
}

// NewRPCClient creates a new RPC client.
func NewRPCClient(socketPath string) *RPCClient {
	return &RPCClient{
		socketPath: socketPath,
		timeout:    30 * time.Second,
	}
}

// Connect establishes a connection to the daemon.
func (c *RPCClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.writer = conn
	c.connected = true
	return nil
}

// Close closes the connection.
func (c *RPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	c.connected = false
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns whether the client is connected.
func (c *RPCClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Call makes an RPC call and returns the result.
func (c *RPCClient) Call(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected to daemon")
	}

	// Build request
	c.nextID++
	req := models.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID,
		Method:  method,
	}

	if params != nil {
		paramsData, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		req.Params = paramsData
	}

	// Marshal request
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Set deadline
	if err := c.conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, fmt.Errorf("failed to set deadline: %w", err)
	}

	// Write length-prefixed frame
	_, err = fmt.Fprintf(c.writer, "%d\n", len(reqData))
	if err != nil {
		c.connected = false
		return nil, fmt.Errorf("failed to write length: %w", err)
	}

	_, err = c.writer.Write(reqData)
	if err != nil {
		c.connected = false
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Read response
	lengthLine, err := c.reader.ReadString('\n')
	if err != nil {
		c.connected = false
		return nil, fmt.Errorf("failed to read response length: %w", err)
	}

	length, err := strconv.Atoi(strings.TrimSpace(lengthLine))
	if err != nil {
		return nil, fmt.Errorf("invalid response length: %w", err)
	}

	if length <= 0 || length > 10*1024*1024 {
		return nil, fmt.Errorf("invalid response length: %d", length)
	}

	respData := make([]byte, length)
	_, err = io.ReadFull(c.reader, respData)
	if err != nil {
		c.connected = false
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var resp models.JSONRPCResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("[%d] %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

// SetTimeout sets the RPC call timeout.
func (c *RPCClient) SetTimeout(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeout = d
}

// Chat sends a chat message and returns the response.
func (c *RPCClient) Chat(message, conversationID string) (string, error) {
	params := map[string]string{
		"message":         message,
		"conversation_id": conversationID,
	}

	result, err := c.Call("chat", params)
	if err != nil {
		return "", err
	}

	var resp struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("failed to parse chat response: %w", err)
	}

	return resp.Reply, nil
}

// Status gets the daemon status.
func (c *RPCClient) Status() (*types.DaemonStatusResponse, error) {
	result, err := c.Call("status", nil)
	if err != nil {
		return nil, err
	}

	var resp types.DaemonStatusResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse status response: %w", err)
	}

	return &resp, nil
}

// ListJobs gets the list of scheduled jobs.
func (c *RPCClient) ListJobs() (*types.JobListResponse, error) {
	result, err := c.Call("scheduler.list_jobs", nil)
	if err != nil {
		return nil, err
	}

	var resp types.JobListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse jobs response: %w", err)
	}

	return &resp, nil
}

// QueryMemory queries the memory store.
func (c *RPCClient) QueryMemory(query string, limit int) (*types.MemoryQueryResponse, error) {
	params := map[string]any{
		"query": query,
		"limit": limit,
	}

	result, err := c.Call("memory.query", params)
	if err != nil {
		return nil, err
	}

	var resp types.MemoryQueryResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse memory response: %w", err)
	}

	return &resp, nil
}
