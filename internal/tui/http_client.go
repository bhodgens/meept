// Package tui provides the terminal user interface for meept.
package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

// HTTPClient implements DaemonClient by calling the HTTP REST API.
// It is used when client.transport is set to "http" or when RPC is unavailable.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

// NewHTTPClient creates an HTTP client for the daemon REST API.
func NewHTTPClient(baseURL string) *HTTPClient {
	if baseURL == "" {
		baseURL = "http://localhost:8081"
	}
	return &HTTPClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		timeout: 30 * time.Second,
	}
}

// Connect verifies the HTTP endpoint is reachable.
func (c *HTTPClient) Connect() error {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/health")
	if err != nil {
		return fmt.Errorf("failed to connect to daemon HTTP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon health check returned %d", resp.StatusCode)
	}
	return nil
}

// Close is a no-op for HTTP client.
func (c *HTTPClient) Close() error { return nil }

// IsConnected checks if the daemon is reachable.
func (c *HTTPClient) IsConnected() bool {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// SetTimeout sets the request timeout.
func (c *HTTPClient) SetTimeout(d time.Duration) {
	c.timeout = d
	c.httpClient.Timeout = d
}

// Chat sends a chat message via HTTP and returns the response.
func (c *HTTPClient) Chat(message, conversationID string) (string, error) {
	params := map[string]string{
		"message":         message,
		"conversation_id": conversationID,
	}
	body, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Post(
		c.baseURL+"/api/v1/chat",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var result struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	return result.Reply, nil
}

// callAPI sends a JSON POST to the daemon bus proxy endpoint.
func (c *HTTPClient) callAPI(method string, params any) (json.RawMessage, error) {
	payload := map[string]any{
		"method": method,
		"params": params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Post(
		c.baseURL+"/api/v1/bus/call",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	var result struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return data, nil
	}
	if result.Error != nil {
		return nil, fmt.Errorf("[%d] %s", result.Error.Code, result.Error.Message)
	}
	return result.Result, nil
}

// Call makes a generic API call.
func (c *HTTPClient) Call(method string, params any) (json.RawMessage, error) {
	return c.callAPI(method, params)
}

// Status gets daemon status via HTTP.
func (c *HTTPClient) Status() (*types.DaemonStatusResponse, error) {
	result, err := c.callAPI("status", nil)
	if err != nil {
		return nil, err
	}
	var resp types.DaemonStatusResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListJobs lists scheduled jobs.
func (c *HTTPClient) ListJobs() (*types.JobListResponse, error) {
	result, err := c.callAPI("scheduler.list_jobs", nil)
	if err != nil {
		return nil, err
	}
	var resp types.JobListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// QueryMemory queries the memory store.
func (c *HTTPClient) QueryMemory(query string, limit int) (*types.MemoryQueryResponse, error) {
	result, err := c.callAPI("memory.query", map[string]any{"query": query, "limit": limit})
	if err != nil {
		return nil, err
	}
	var resp types.MemoryQueryResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetRecentMemories retrieves recent memories.
func (c *HTTPClient) GetRecentMemories(limit int) (*types.MemoryQueryResponse, error) {
	result, err := c.callAPI("memory.recent", map[string]any{"limit": limit})
	if err != nil {
		return nil, err
	}
	var resp types.MemoryQueryResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListWorkers lists active workers.
func (c *HTTPClient) ListWorkers() (*types.WorkerListResponse, error) {
	result, err := c.callAPI("agent.workers.list", nil)
	if err != nil {
		return nil, err
	}
	var resp types.WorkerListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Session methods
func (c *HTTPClient) CreateSession(name string) (*types.Session, error) {
	result, err := c.callAPI("session.create", map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	var resp types.Session
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListSessions lists all sessions.
func (c *HTTPClient) ListSessions() (*types.SessionListResponse, error) {
	result, err := c.callAPI("session.list", nil)
	if err != nil {
		return nil, err
	}
	var resp types.SessionListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AttachSession attaches a client to a session.
func (c *HTTPClient) AttachSession(sessionID, clientID string) error {
	_, err := c.callAPI("session.attach", map[string]string{"session_id": sessionID, "client_id": clientID})
	return err
}

// DetachSession detaches a client.
func (c *HTTPClient) DetachSession(sessionID, clientID string) error {
	_, err := c.callAPI("session.detach", map[string]string{"session_id": sessionID, "client_id": clientID})
	return err
}

// GetMostRecentSession gets the most recent session.
func (c *HTTPClient) GetMostRecentSession() (*types.Session, error) {
	result, err := c.callAPI("session.get_most_recent", nil)
	if err != nil {
		return nil, err
	}
	var resp types.Session
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteSession deletes a session.
func (c *HTTPClient) DeleteSession(sessionID string) error {
	_, err := c.callAPI("session.delete", map[string]string{"id": sessionID})
	return err
}

// SaveSessionMessages saves messages.
func (c *HTTPClient) SaveSessionMessages(sessionID string, messages []types.SessionMessage) error {
	_, err := c.callAPI("session.messages.save", map[string]any{"session_id": sessionID, "messages": messages})
	return err
}

// GetSessionMessages retrieves messages.
func (c *HTTPClient) GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error) {
	result, err := c.callAPI("session.messages.get", map[string]any{"session_id": sessionID, "offset": offset, "limit": limit})
	if err != nil {
		return nil, err
	}
	var resp types.SessionMessagesResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateSessionDescription updates description.
func (c *HTTPClient) UpdateSessionDescription(sessionID, description string) error {
	_, err := c.callAPI("session.update_description", map[string]string{"session_id": sessionID, "description": description})
	return err
}

// GenerateSessionDescription generates a description.
func (c *HTTPClient) GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error) {
	result, err := c.callAPI("session.generate_description", map[string]string{
		"session_id":    sessionID,
		"first_message": firstMessage,
		"project_name":  projectName,
	})
	if err != nil {
		return nil, err
	}
	var resp types.GenerateDescriptionResult
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// StopSession stops a session.
func (c *HTTPClient) StopSession(sessionID string) (*types.StopSessionResponse, error) {
	result, err := c.callAPI("session.stop", map[string]string{"session_id": sessionID})
	if err != nil {
		return nil, err
	}
	var resp types.StopSessionResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetSessionChildTasks gets child tasks.
func (c *HTTPClient) GetSessionChildTasks(sessionID string) ([]string, error) {
	result, err := c.callAPI("session.get_child_tasks", map[string]string{"session_id": sessionID})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tasks []string `json:"tasks"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// Task methods
func (c *HTTPClient) CreateTask(name, description string) (*types.Task, error) {
	result, err := c.callAPI("task.create", map[string]string{"name": name, "description": description})
	if err != nil {
		return nil, err
	}
	var resp types.Task
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTask retrieves a task.
func (c *HTTPClient) GetTask(taskID string) (*types.Task, error) {
	result, err := c.callAPI("task.get", map[string]string{"id": taskID})
	if err != nil {
		return nil, err
	}
	var resp types.Task
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListTasks lists tasks.
func (c *HTTPClient) ListTasks(state string, limit int) (*types.TaskListResponse, error) {
	result, err := c.callAPI("task.list", map[string]any{"state": state, "limit": limit})
	if err != nil {
		return nil, err
	}
	var resp types.TaskListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListTasksExtended lists extended tasks.
func (c *HTTPClient) ListTasksExtended() (*types.TaskExtendedListResponse, error) {
	result, err := c.callAPI("task.list_extended", nil)
	if err != nil {
		return nil, err
	}
	var resp types.TaskExtendedListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListTaskSteps returns steps.
func (c *HTTPClient) ListTaskSteps(taskID string) (*types.TaskStepsResponse, error) {
	result, err := c.callAPI("task.steps", map[string]string{"task_id": taskID})
	if err != nil {
		return nil, err
	}
	var resp types.TaskStepsResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteTask deletes a task.
func (c *HTTPClient) DeleteTask(taskID string) error {
	_, err := c.callAPI("task.delete", map[string]string{"id": taskID})
	return err
}

// CancelTask cancels a task.
func (c *HTTPClient) CancelTask(taskID string) error {
	_, err := c.callAPI("task.cancel", map[string]string{"id": taskID})
	return err
}

// LinkTaskSession links session to task.
func (c *HTTPClient) LinkTaskSession(taskID, sessionID string) error {
	_, err := c.callAPI("task.link", map[string]string{"task_id": taskID, "session_id": sessionID})
	return err
}

// UnlinkTaskSession removes link.
func (c *HTTPClient) UnlinkTaskSession(taskID, sessionID string) error {
	_, err := c.callAPI("task.unlink", map[string]string{"task_id": taskID, "session_id": sessionID})
	return err
}

// Queue methods
func (c *HTTPClient) GetQueueStats() (*types.QueueStatsResponse, error) {
	result, err := c.callAPI("queue.stats", nil)
	if err != nil {
		return nil, err
	}
	var resp types.QueueStatsResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListQueueJobs lists queue jobs.
func (c *HTTPClient) ListQueueJobs(state string, limit int) (*types.QueueJobListResponse, error) {
	result, err := c.callAPI("queue.list", map[string]any{"state": state, "limit": limit})
	if err != nil {
		return nil, err
	}
	var resp types.QueueJobListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RetryQueueJob retries a job.
func (c *HTTPClient) RetryQueueJob(jobID string) error {
	_, err := c.callAPI("queue.retry", map[string]string{"job_id": jobID})
	return err
}

// Worker pool methods
func (c *HTTPClient) GetWorkerPoolStats() (*types.WorkerPoolStats, error) {
	result, err := c.callAPI("worker.stats", nil)
	if err != nil {
		return nil, err
	}
	var resp types.WorkerPoolStats
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListPoolWorkers lists workers.
func (c *HTTPClient) ListPoolWorkers() (*types.WorkerPoolResponse, error) {
	result, err := c.callAPI("worker.list", nil)
	if err != nil {
		return nil, err
	}
	var resp types.WorkerPoolResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ScaleWorkerPool scales the pool.
func (c *HTTPClient) ScaleWorkerPool(targetCount int) error {
	_, err := c.callAPI("worker.scale", map[string]int{"target_count": targetCount})
	return err
}

// Cache methods
func (c *HTTPClient) CacheStats() (*types.CacheStatsResponse, error) {
	result, err := c.callAPI("cache.stats", nil)
	if err != nil {
		return nil, err
	}
	var resp types.CacheStatsResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CacheClear clears the cache.
func (c *HTTPClient) CacheClear() error {
	_, err := c.callAPI("cache.clear", nil)
	return err
}

// CacheInvalidate invalidates cache entries.
func (c *HTTPClient) CacheInvalidate(filePath string) error {
	_, err := c.callAPI("cache.invalidate", map[string]string{"file_path": filePath})
	return err
}

// Ensure HTTPClient implements DaemonClient at compile time.
var _ DaemonClient = (*HTTPClient)(nil)
