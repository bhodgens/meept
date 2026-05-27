// Package tui provides the terminal user interface for meept.
package tui

import (
	"bufio"
	"encoding/json"
	"errors"
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
	connMu     sync.Mutex  // Protects conn, reader, writer during connect/disconnect
	callMu     sync.Mutex  // Serializes RPC calls (prevents request/response interleaving)
	connected  atomic.Bool // Lock-free connection status for UI queries
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
	// Check for EOF explicitly (daemon exited/crashed)
	if errors.Is(err, io.EOF) {
		return true
	}
	errStr := err.Error()
	return strings.Contains(errStr, "failed to write") ||
		strings.Contains(errStr, "failed to read") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "not connected") ||
		strings.Contains(errStr, "connection refused")
}

// callOnce makes a single RPC call attempt.
// Uses split-lock design: connMu briefly to get conn references, callMu to serialize calls.
func (c *RPCClient) callOnce(method string, params any) (json.RawMessage, error) {
	// Fast path: check connection status without lock
	if !c.connected.Load() {
		return nil, errors.New(ErrNotConnected)
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
		return nil, errors.New(ErrNotConnected)
	}

	// Get connection references under connMu (brief hold)
	c.connMu.Lock()
	conn := c.conn
	reader := c.reader
	writer := c.writer
	c.connMu.Unlock()

	if conn == nil {
		c.connected.Store(false)
		return nil, errors.New(ErrNotConnected)
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
		ParamMessage:        message,
		ParamConversationID: conversationID,
	}

	result, err := c.Call("chat", params)
	if err != nil {
		return "", err
	}

	var resp struct {
		Reply string `json:"reply"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("failed to parse chat response: %w", err)
	}

	if resp.Error != "" {
		return resp.Reply, fmt.Errorf("%s", resp.Error)
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
		"query":    query,
		ParamLimit: limit,
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
		ParamLimit: limit,
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
	params := map[string]string{ParamName: name}
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
		ParamSessionID: sessionID,
		ParamClientID:  clientID,
	}
	_, err := c.Call("session.attach", params)
	return err
}

// DetachSession detaches a client from a session.
func (c *RPCClient) DetachSession(sessionID, clientID string) error {
	params := map[string]string{
		ParamSessionID: sessionID,
		ParamClientID:  clientID,
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
		ParamSessionID: sessionID,
		"messages":     messages,
	}
	_, err := c.Call("session.messages.save", params)
	return err
}

// GetSessionMessages retrieves messages for a session with pagination.
func (c *RPCClient) GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error) {
	params := map[string]any{
		ParamSessionID: sessionID,
		"offset":       offset,
		ParamLimit:     limit,
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
		ParamSessionID:   sessionID,
		ParamDescription: description,
	}
	_, err := c.Call("session.update_description", params)
	return err
}

// GenerateSessionDescription uses LLM to generate a session description.
func (c *RPCClient) GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error) {
	params := map[string]string{
		ParamSessionID:  sessionID,
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
	params := map[string]string{ParamSessionID: sessionID}
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
	params := map[string]string{ParamSessionID: sessionID}
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
		ParamName:        name,
		ParamDescription: description,
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
		ParamState: state,
		ParamLimit: limit,
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
	params := map[string]string{ParamTaskID: taskID}
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

// CancelTask transitions a task to StateCancelled. This is a state flip
// only; it does not interrupt any in-flight jobs.
func (c *RPCClient) CancelTask(taskID string) error {
	params := map[string]string{"id": taskID}
	_, err := c.Call("task.cancel", params)
	return err
}

// LinkTaskSession links a session to a task.
func (c *RPCClient) LinkTaskSession(taskID, sessionID string) error {
	params := map[string]string{
		ParamTaskID:    taskID,
		ParamSessionID: sessionID,
	}
	_, err := c.Call("task.link", params)
	return err
}

// UnlinkTaskSession removes a session-task link.
func (c *RPCClient) UnlinkTaskSession(taskID, sessionID string) error {
	params := map[string]string{
		ParamTaskID:    taskID,
		ParamSessionID: sessionID,
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
		ParamState: state,
		ParamLimit: limit,
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

// ============================================================================
// Cache Methods
// ============================================================================

// CacheStats gets token cache statistics.
func (c *RPCClient) CacheStats() (*types.CacheStatsResponse, error) {
	result, err := c.Call("cache.stats", nil)
	if err != nil {
		return nil, err
	}

	var resp types.CacheStatsResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse cache stats response: %w", err)
	}

	return &resp, nil
}

// CacheClear clears all cache entries.
func (c *RPCClient) CacheClear() error {
	_, err := c.Call("cache.clear", nil)
	return err
}

// CacheInvalidate invalidates cache entries for a specific file.
func (c *RPCClient) CacheInvalidate(filePath string) error {
	params := map[string]string{"file_path": filePath}
	_, err := c.Call("cache.invalidate", params)
	return err
}

// CacheInspect inspects cache entries matching a prompt hash.
func (c *RPCClient) CacheInspect(promptHash string) (*types.CacheInspectResponse, error) {
	params := map[string]string{"prompt_hash": promptHash}
	result, err := c.Call("cache.inspect", params)
	if err != nil {
		return nil, err
	}

	var resp types.CacheInspectResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse cache inspect response: %w", err)
	}

	return &resp, nil
}

// ============================================================================
// Steering and Follow-Up Queue Methods
// ============================================================================

// Steer sends a steering message to an active conversation.
func (c *RPCClient) Steer(message, conversationID string) error {
	req := map[string]string{
		ParamMessage:        message,
		ParamConversationID: conversationID,
		"source":            "tui",
	}
	_, err := c.Call("chat.steer", req)
	return err
}

// FollowUp sends a follow-up message to an active conversation.
func (c *RPCClient) FollowUp(message, conversationID string) error {
	req := map[string]string{
		ParamMessage:        message,
		ParamConversationID: conversationID,
		"source":            "tui",
	}
	_, err := c.Call("chat.followup", req)
	return err
}

// GetQueueStatus returns the current queue state for a conversation.
func (c *RPCClient) GetQueueStatus(conversationID string) (*types.QueueStatusResponse, error) {
	req := map[string]string{ParamConversationID: conversationID}
	result, err := c.Call("chat.queue_status", req)
	if err != nil {
		return nil, err
	}

	var resp types.QueueStatusResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse queue status response: %w", err)
	}

	return &resp, nil
}

// RestorePendingFollowUps checks for persisted follow-ups for a conversation.
func (c *RPCClient) RestorePendingFollowUps(conversationID string) error {
	req := map[string]string{ParamConversationID: conversationID}
	_, err := c.Call("chat.queue.restore", req)
	return err
}

// ============================================================================
// Branch Methods
// ============================================================================

// NavigateBranch navigates to a prior message in a session, creating a new branch.
func (c *RPCClient) NavigateBranch(sessionID string, targetMessageID int64) error {
	params := map[string]any{
		ParamSessionID:      sessionID,
		"target_message_id": targetMessageID,
	}
	_, err := c.Call("session.branch.navigate", params)
	return err
}

// BranchInfo is an alias for the shared type.
type BranchInfo = types.BranchInfo

// ListBranches lists all branches in a session.
func (c *RPCClient) ListBranches(sessionID string) ([]BranchInfo, error) {
	params := map[string]string{ParamSessionID: sessionID}
	result, err := c.Call("session.branches.list", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Branches []BranchInfo `json:"branches"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse branches response: %w", err)
	}

	return resp.Branches, nil
}

// ForkSession forks a session from a specific message, returning the new session ID.
func (c *RPCClient) ForkSession(sessionID string, fromMessageID int64, name string) (string, error) {
	params := map[string]any{
		ParamSessionID:    sessionID,
		"from_message_id": fromMessageID,
		ParamName:         name,
	}
	result, err := c.Call("session.fork", params)
	if err != nil {
		return "", err
	}

	var resp struct {
		NewSessionID string `json:"new_session_id"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("failed to parse fork response: %w", err)
	}

	return resp.NewSessionID, nil
}

// TreeNodeInfo is an alias for the shared type.
type TreeNodeInfo = types.TreeNodeInfo

// GetTree returns the conversation tree for a session.
func (c *RPCClient) GetTree(sessionID string) ([]TreeNodeInfo, error) {
	params := map[string]string{ParamSessionID: sessionID}
	result, err := c.Call("session.tree.get", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Nodes []TreeNodeInfo `json:"nodes"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse tree response: %w", err)
	}

	return resp.Nodes, nil
}

// ============================================================================
// Template Methods
// ============================================================================

// TemplateInfo holds basic info about a template returned by ListTemplates.
type TemplateInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
}

// ListTemplates fetches all available template names from the daemon.
// Returns template names suitable for autocomplete and invocation.
func (c *RPCClient) ListTemplates() ([]TemplateInfo, error) {
	result, err := c.Call("templates.list", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Templates []TemplateInfo `json:"templates"`
		Count     int            `json:"count"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse templates list response: %w", err)
	}

	return resp.Templates, nil
}

// InvokeTemplate substitutes arguments into a template and executes it via the
// daemon. Returns the substituted prompt text. If the daemon has an executor
// configured, it also returns the LLM response content.
func (c *RPCClient) InvokeTemplate(name string, args []string) (string, error) {
	params := map[string]any{
		ParamName: name,
		"args":    args,
	}

	result, err := c.Call("templates.invoke", params)
	if err != nil {
		return "", err
	}

	var resp struct {
		Prompt  string `json:"prompt"`
		Content string `json:"content"`
		Success bool   `json:"success"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("failed to parse template invoke response: %w", err)
	}

	// If the daemon executed the template and returned LLM content, use that.
	if resp.Content != "" {
		return resp.Content, nil
	}

	// Otherwise return the substituted prompt text.
	return resp.Prompt, nil
}

// ============================================================================
// Project Methods
// ============================================================================

// ListProjects fetches all registered projects from the daemon.
func (c *RPCClient) ListProjects() (*types.ProjectListResponse, error) {
	result, err := c.Call("project.list", nil)
	if err != nil {
		return nil, err
	}

	var resp types.ProjectListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse projects response: %w", err)
	}

	return &resp, nil
}

// GetProject fetches a single project by ID.
func (c *RPCClient) GetProject(id string) (*types.ProjectInfo, error) {
	params := map[string]string{"id": id}
	result, err := c.Call("project.get", params)
	if err != nil {
		return nil, err
	}

	var resp types.ProjectInfo
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse project response: %w", err)
	}

	return &resp, nil
}

// RegisterProject registers a new project with the daemon.
func (c *RPCClient) RegisterProject(name, gitURL, localPath string) (*types.ProjectInfo, error) {
	params := map[string]string{
		ParamName: name,
	}
	if gitURL != "" {
		params["git_url"] = gitURL
	}
	if localPath != "" {
		params["local_path"] = localPath
	}

	result, err := c.Call("project.register", params)
	if err != nil {
		return nil, err
	}

	var resp types.ProjectInfo
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse project register response: %w", err)
	}

	return &resp, nil
}

// UnregisterProject removes a project registration from the daemon.
func (c *RPCClient) UnregisterProject(id string) error {
	params := map[string]string{"id": id}
	_, err := c.Call("project.unregister", params)
	return err
}

// SetProject binds a project to a session.
func (c *RPCClient) SetProject(sessionID, projectID string) error {
	params := map[string]string{
		ParamSessionID: sessionID,
		"project_id":   projectID,
	}
	_, err := c.Call("project.set", params)
	return err
}

// SyncProject synchronizes a project (pulls latest for git projects).
func (c *RPCClient) SyncProject(id string) error {
	params := map[string]string{"id": id}
	_, err := c.Call("project.sync", params)
	return err
}

// ProjectStatus gets the runtime status of a project (branch, dirty, etc.).
func (c *RPCClient) ProjectStatus(id string) (*types.ProjectStatusResponse, error) {
	params := map[string]string{"id": id}
	result, err := c.Call("project.status", params)
	if err != nil {
		return nil, err
	}

	var resp types.ProjectStatusResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse project status response: %w", err)
	}

	return &resp, nil
}

// DetectProject attempts to detect a project from a filesystem path.
func (c *RPCClient) DetectProject(path string) (*types.ProjectInfo, error) {
	params := map[string]string{"path": path}
	result, err := c.Call("project.detect", params)
	if err != nil {
		return nil, err
	}

	var resp types.ProjectInfo
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse project detect response: %w", err)
	}

	return &resp, nil
}
