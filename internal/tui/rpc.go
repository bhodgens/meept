// Package tui provides the terminal user interface for meept.
package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
	"github.com/caimlas/meept/pkg/models"
)

// RPCClient is a JSON-RPC client for communicating with the meept daemon.
// It uses a split-lock design to prevent UI blocking:
// - connMu protects connection state changes (connect/disconnect)
// - callMu serializes RPC calls to prevent interleaving
// - connected is an atomic bool for lock-free status checks
type RPCClient struct {
	socketPath string
	conn       net.Conn
	reader     *bufio.Reader
	writer     io.Writer
	connMu     sync.Mutex    // Protects conn, reader, writer during connect/disconnect
	callMu     sync.Mutex    // Serializes RPC calls (prevents request/response interleaving)
	connected  atomic.Bool   // Lock-free connection status for UI queries
	timeout    time.Duration
	nextID     atomic.Int64 // Thread-safe ID generation

	// Reconnection settings
	maxRetries    int
	retryInterval time.Duration
}

// NewRPCClient creates a new RPC client.
func NewRPCClient(socketPath string) *RPCClient {
	return &RPCClient{
		socketPath:    socketPath,
		timeout:       120 * time.Second, // Match server's chat timeout
		maxRetries:    3,
		retryInterval: 500 * time.Millisecond,
	}
}

// Connect establishes a connection to the daemon.
func (c *RPCClient) Connect() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.connected.Load() {
		return nil
	}

	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.writer = conn
	c.connected.Store(true)
	return nil
}

// Close closes the connection.
func (c *RPCClient) Close() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if !c.connected.Load() {
		return nil
	}

	c.connected.Store(false)
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns whether the client is connected.
// This is lock-free to allow UI rendering without blocking on RPC calls.
func (c *RPCClient) IsConnected() bool {
	return c.connected.Load()
}

// Call makes an RPC call and returns the result.
// It will attempt to reconnect on connection errors.
func (c *RPCClient) Call(method string, params any) (json.RawMessage, error) {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		result, err := c.callOnce(method, params)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Check if this is a connection error that warrants retry
		if !c.isConnectionError(err) {
			return nil, err
		}

		// Try to reconnect
		if attempt < c.maxRetries {
			c.connMu.Lock()
			c.connected.Store(false)
			if c.conn != nil {
				c.conn.Close()
			}
			c.connMu.Unlock()

			time.Sleep(c.retryInterval)

			if err := c.Connect(); err != nil {
				lastErr = fmt.Errorf("reconnect failed: %w", err)
				continue
			}
		}
	}

	return nil, fmt.Errorf("call failed after %d attempts: %w", c.maxRetries+1, lastErr)
}

// isConnectionError returns true if the error suggests connection issues.
func (c *RPCClient) isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "failed to write") ||
		strings.Contains(errStr, "failed to read") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "not connected")
}

// callOnce makes a single RPC call attempt.
// Uses split-lock design: connMu briefly to get conn references, callMu to serialize calls.
func (c *RPCClient) callOnce(method string, params any) (json.RawMessage, error) {
	// Fast path: check connection status without lock
	if !c.connected.Load() {
		return nil, fmt.Errorf("not connected to daemon")
	}

	// Build request outside of any lock
	reqID := c.nextID.Add(1)
	req := models.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
	}

	if params != nil {
		paramsData, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		req.Params = paramsData
	}

	// Marshal request outside of lock
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Serialize RPC calls to prevent request/response interleaving
	c.callMu.Lock()
	defer c.callMu.Unlock()

	// Re-check connection status after acquiring lock
	if !c.connected.Load() {
		return nil, fmt.Errorf("not connected to daemon")
	}

	// Get connection references under connMu (brief hold)
	c.connMu.Lock()
	conn := c.conn
	reader := c.reader
	writer := c.writer
	c.connMu.Unlock()

	if conn == nil {
		c.connected.Store(false)
		return nil, fmt.Errorf("not connected to daemon")
	}

	// Set deadline
	if err := conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, fmt.Errorf("failed to set deadline: %w", err)
	}

	// Write length-prefixed frame
	_, err = fmt.Fprintf(writer, "%d\n", len(reqData))
	if err != nil {
		c.connected.Store(false)
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	_, err = writer.Write(reqData)
	if err != nil {
		c.connected.Store(false)
		return nil, fmt.Errorf("failed to write request body: %w", err)
	}

	// Read response
	lengthLine, err := reader.ReadString('\n')
	if err != nil {
		c.connected.Store(false)
		return nil, fmt.Errorf("failed to read response (daemon may be busy or disconnected): %w", err)
	}

	length, err := strconv.Atoi(strings.TrimSpace(lengthLine))
	if err != nil {
		return nil, fmt.Errorf("invalid response format: %w", err)
	}

	if length <= 0 || length > 10*1024*1024 {
		return nil, fmt.Errorf("invalid response length: %d", length)
	}

	respData := make([]byte, length)
	_, err = io.ReadFull(reader, respData)
	if err != nil {
		c.connected.Store(false)
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse response
	var resp models.JSONRPCResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("[%d] %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

// SetTimeout sets the RPC call timeout.
func (c *RPCClient) SetTimeout(d time.Duration) {
	c.connMu.Lock()
	defer c.connMu.Unlock()
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

// GetRecentMemories retrieves the most recent memories.
func (c *RPCClient) GetRecentMemories(limit int) (*types.MemoryQueryResponse, error) {
	params := map[string]any{
		"limit": limit,
	}

	result, err := c.Call("memory.recent", params)
	if err != nil {
		return nil, err
	}

	var resp types.MemoryQueryResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse memory response: %w", err)
	}

	return &resp, nil
}

// ListWorkers gets the list of active agent workers.
func (c *RPCClient) ListWorkers() (*types.WorkerListResponse, error) {
	result, err := c.Call("agent.workers.list", nil)
	if err != nil {
		return nil, err
	}

	var resp types.WorkerListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse workers response: %w", err)
	}

	return &resp, nil
}

// CreateSession creates a new session.
func (c *RPCClient) CreateSession(name string) (*types.Session, error) {
	params := map[string]string{"name": name}
	result, err := c.Call("session.create", params)
	if err != nil {
		return nil, err
	}

	var resp types.Session
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse session response: %w", err)
	}

	return &resp, nil
}

// ListSessions gets all sessions.
func (c *RPCClient) ListSessions() (*types.SessionListResponse, error) {
	result, err := c.Call("session.list", nil)
	if err != nil {
		return nil, err
	}

	var resp types.SessionListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse sessions response: %w", err)
	}

	// Debug: log session descriptions to verify they're being returned
	for _, s := range resp.Sessions {
		slog.Debug("ListSessions result", "id", s.ID, "name", s.Name, "description", s.Description)
	}

	return &resp, nil
}

// AttachSession attaches a client to a session.
func (c *RPCClient) AttachSession(sessionID, clientID string) error {
	params := map[string]string{
		"session_id": sessionID,
		"client_id":  clientID,
	}
	_, err := c.Call("session.attach", params)
	return err
}

// DetachSession detaches a client from a session.
func (c *RPCClient) DetachSession(sessionID, clientID string) error {
	params := map[string]string{
		"session_id": sessionID,
		"client_id":  clientID,
	}
	_, err := c.Call("session.detach", params)
	return err
}

// GetMostRecentSession gets the most recently active session.
func (c *RPCClient) GetMostRecentSession() (*types.Session, error) {
	result, err := c.Call("session.get_most_recent", nil)
	if err != nil {
		return nil, err
	}

	var resp types.Session
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse session response: %w", err)
	}

	return &resp, nil
}

// DeleteSession deletes a session by ID.
func (c *RPCClient) DeleteSession(sessionID string) error {
	params := map[string]string{"id": sessionID}
	_, err := c.Call("session.delete", params)
	return err
}

// SaveSessionMessages saves messages for a session.
func (c *RPCClient) SaveSessionMessages(sessionID string, messages []types.SessionMessage) error {
	params := map[string]any{
		"session_id": sessionID,
		"messages":   messages,
	}
	_, err := c.Call("session.messages.save", params)
	return err
}

// GetSessionMessages retrieves messages for a session with pagination.
func (c *RPCClient) GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error) {
	params := map[string]any{
		"session_id": sessionID,
		"offset":     offset,
		"limit":      limit,
	}
	result, err := c.Call("session.messages.get", params)
	if err != nil {
		return nil, err
	}

	var resp types.SessionMessagesResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse session messages response: %w", err)
	}

	return &resp, nil
}

// UpdateSessionDescription updates a session's description.
func (c *RPCClient) UpdateSessionDescription(sessionID, description string) error {
	params := map[string]string{
		"session_id":  sessionID,
		"description": description,
	}
	_, err := c.Call("session.update_description", params)
	return err
}

// GenerateSessionDescription uses LLM to generate a session description.
func (c *RPCClient) GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error) {
	params := map[string]string{
		"session_id":    sessionID,
		"first_message": firstMessage,
		"project_name":  projectName,
	}
	result, err := c.Call("session.generate_description", params)
	if err != nil {
		return nil, err
	}

	var resp types.GenerateDescriptionResult
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ============================================================================
// Session Work Control Methods
// ============================================================================

// StopSession stops all work for a session.
func (c *RPCClient) StopSession(sessionID string) (*types.StopSessionResponse, error) {
	params := map[string]string{"session_id": sessionID}
	result, err := c.Call("session.stop", params)
	if err != nil {
		return nil, err
	}

	var resp types.StopSessionResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse stop session response: %w", err)
	}

	return &resp, nil
}

// GetSessionChildTasks gets tasks associated with a session.
func (c *RPCClient) GetSessionChildTasks(sessionID string) ([]string, error) {
	params := map[string]string{"session_id": sessionID}
	result, err := c.Call("session.get_child_tasks", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Tasks []string `json:"tasks"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse child tasks response: %w", err)
	}

	return resp.Tasks, nil
}

// ============================================================================
// Task Methods
// ============================================================================

// CreateTask creates a new task.
func (c *RPCClient) CreateTask(name, description string) (*types.Task, error) {
	params := map[string]string{
		"name":        name,
		"description": description,
	}
	result, err := c.Call("task.create", params)
	if err != nil {
		return nil, err
	}

	var resp types.Task
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse task response: %w", err)
	}

	return &resp, nil
}

// GetTask retrieves a task by ID.
func (c *RPCClient) GetTask(taskID string) (*types.Task, error) {
	params := map[string]string{"id": taskID}
	result, err := c.Call("task.get", params)
	if err != nil {
		return nil, err
	}

	var resp types.Task
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse task response: %w", err)
	}

	return &resp, nil
}

// ListTasks gets all tasks.
func (c *RPCClient) ListTasks(state string, limit int) (*types.TaskListResponse, error) {
	params := map[string]any{
		"state": state,
		"limit": limit,
	}
	result, err := c.Call("task.list", params)
	if err != nil {
		return nil, err
	}

	var resp types.TaskListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse tasks response: %w", err)
	}

	return &resp, nil
}

// ListTasksExtended gets all tasks with memory context fields.
func (c *RPCClient) ListTasksExtended() (*types.TaskExtendedListResponse, error) {
	result, err := c.Call("task.list_extended", nil)
	if err != nil {
		// Fallback to regular task list and convert
		regularResp, fallbackErr := c.ListTasks("", 100)
		if fallbackErr != nil {
			return nil, err // Return original error
		}
		// Convert Task to TaskExtended
		extended := make([]types.TaskExtended, len(regularResp.Tasks))
		for i, t := range regularResp.Tasks {
			extended[i] = types.TaskExtended{Task: t}
		}
		return &types.TaskExtendedListResponse{Tasks: extended}, nil
	}

	var resp types.TaskExtendedListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse tasks extended response: %w", err)
	}

	return &resp, nil
}

// ListTaskSteps returns the steps for a task.
func (c *RPCClient) ListTaskSteps(taskID string) (*types.TaskStepsResponse, error) {
	params := map[string]string{"task_id": taskID}
	result, err := c.Call("task.steps", params)
	if err != nil {
		return nil, err
	}

	var resp types.TaskStepsResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse task steps response: %w", err)
	}

	return &resp, nil
}

// DeleteTask deletes a task by ID.
func (c *RPCClient) DeleteTask(taskID string) error {
	params := map[string]string{"id": taskID}
	_, err := c.Call("task.delete", params)
	return err
}

// LinkTaskSession links a session to a task.
func (c *RPCClient) LinkTaskSession(taskID, sessionID string) error {
	params := map[string]string{
		"task_id":    taskID,
		"session_id": sessionID,
	}
	_, err := c.Call("task.link", params)
	return err
}

// UnlinkTaskSession removes a session-task link.
func (c *RPCClient) UnlinkTaskSession(taskID, sessionID string) error {
	params := map[string]string{
		"task_id":    taskID,
		"session_id": sessionID,
	}
	_, err := c.Call("task.unlink", params)
	return err
}

// ============================================================================
// Queue Methods
// ============================================================================

// GetQueueStats gets queue statistics.
func (c *RPCClient) GetQueueStats() (*types.QueueStatsResponse, error) {
	result, err := c.Call("queue.stats", nil)
	if err != nil {
		return nil, err
	}

	var resp types.QueueStatsResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse queue stats response: %w", err)
	}

	return &resp, nil
}

// ListQueueJobs gets jobs in a given state.
func (c *RPCClient) ListQueueJobs(state string, limit int) (*types.QueueJobListResponse, error) {
	params := map[string]any{
		"state": state,
		"limit": limit,
	}
	result, err := c.Call("queue.list", params)
	if err != nil {
		return nil, err
	}

	var resp types.QueueJobListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse queue jobs response: %w", err)
	}

	return &resp, nil
}

// RetryQueueJob retries a failed job.
func (c *RPCClient) RetryQueueJob(jobID string) error {
	params := map[string]string{"job_id": jobID}
	_, err := c.Call("queue.retry", params)
	return err
}

// ============================================================================
// Worker Pool Methods
// ============================================================================

// GetWorkerPoolStats gets worker pool statistics.
func (c *RPCClient) GetWorkerPoolStats() (*types.WorkerPoolStats, error) {
	result, err := c.Call("worker.stats", nil)
	if err != nil {
		return nil, err
	}

	var resp types.WorkerPoolStats
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse worker pool stats response: %w", err)
	}

	return &resp, nil
}

// ListPoolWorkers gets all workers in the pool.
func (c *RPCClient) ListPoolWorkers() (*types.WorkerPoolResponse, error) {
	result, err := c.Call("worker.list", nil)
	if err != nil {
		return nil, err
	}

	var resp types.WorkerPoolResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse workers response: %w", err)
	}

	return &resp, nil
}

// ScaleWorkerPool adjusts the number of workers.
func (c *RPCClient) ScaleWorkerPool(targetCount int) error {
	params := map[string]int{"target_count": targetCount}
	_, err := c.Call("worker.scale", params)
	return err
}
