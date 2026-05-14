package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

// httpClient implements transport.Client over HTTP REST.
// All RPC-style methods are proxied through the /api/v1/bus/call endpoint,
// which mirrors the JSON-RPC interface over HTTP.
type httpClient struct {
	baseURL string
	client  *http.Client
}

// NewHTTPClient creates an HTTP-backed transport client.
func NewHTTPClient(baseURL string, timeout time.Duration) Client {
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &httpClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *httpClient) Connect() error {
	resp, err := c.client.Get(c.baseURL + "/api/v1/health")
	if err != nil {
		return fmt.Errorf("daemon not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned %d", resp.StatusCode)
	}
	return nil
}

func (c *httpClient) Close() error { return nil }

func (c *httpClient) IsConnected() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/health", http.NoBody)
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *httpClient) SetTimeout(d time.Duration) {
	c.client.Timeout = d
}

// callAPI sends a JSON POST to the daemon bus proxy endpoint.
func (c *httpClient) callAPI(method string, params any) (json.RawMessage, error) {
	payload := map[string]any{
		"method": method,
		"params": params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Post(
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

// Call makes a generic API call via the bus proxy.
func (c *httpClient) Call(method string, params any) (json.RawMessage, error) {
	return c.callAPI(method, params)
}

// Chat sends a chat message via HTTP and returns the response.
func (c *httpClient) Chat(message, conversationID string) (string, error) {
	params := map[string]string{
		"message":         message,
		"conversation_id": conversationID,
	}
	body, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	resp, err := c.client.Post(
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
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	var result struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	return result.Reply, nil
}

// Status gets daemon status via the bus proxy.
func (c *httpClient) Status() (*types.DaemonStatusResponse, error) {
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
func (c *httpClient) ListJobs() (*types.JobListResponse, error) {
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
func (c *httpClient) QueryMemory(query string, limit int) (*types.MemoryQueryResponse, error) {
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
func (c *httpClient) GetRecentMemories(limit int) (*types.MemoryQueryResponse, error) {
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
func (c *httpClient) ListWorkers() (*types.WorkerListResponse, error) {
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

// GetQueueStats returns queue statistics.
func (c *httpClient) GetQueueStats() (*types.QueueStatsResponse, error) {
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
func (c *httpClient) ListQueueJobs(state string, limit int) (*types.QueueJobListResponse, error) {
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

// ListTasks lists tasks.
func (c *httpClient) ListTasks(state string, limit int) (*types.TaskListResponse, error) {
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

// CreateTask creates a new task.
func (c *httpClient) CreateTask(name, description string) (*types.Task, error) {
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

// GetTask retrieves a task by ID.
func (c *httpClient) GetTask(taskID string) (*types.Task, error) {
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

// CacheStats returns cache statistics.
func (c *httpClient) CacheStats() (*types.CacheStatsResponse, error) {
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

// CacheClear clears the token cache.
func (c *httpClient) CacheClear() error {
	_, err := c.callAPI("cache.clear", nil)
	return err
}

// CacheInvalidate invalidates cache entries for a file.
func (c *httpClient) CacheInvalidate(filePath string) error {
	_, err := c.callAPI("cache.invalidate", map[string]string{"file_path": filePath})
	return err
}

// CacheInspect inspects cache entries matching a prompt hash.
func (c *httpClient) CacheInspect(promptHash string) (*types.CacheInspectResponse, error) {
	result, err := c.callAPI("cache.inspect", map[string]string{"prompt_hash": promptHash})
	if err != nil {
		return nil, err
	}
	var resp types.CacheInspectResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Session methods

// ListSessions lists all sessions.
func (c *httpClient) ListSessions() (*types.SessionListResponse, error) {
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

// CreateSession creates a new session.
func (c *httpClient) CreateSession(name string) (*types.Session, error) {
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

// AttachSession attaches a client to a session.
func (c *httpClient) AttachSession(sessionID, clientID string) error {
	_, err := c.callAPI("session.attach", map[string]string{"session_id": sessionID, "client_id": clientID})
	return err
}

// DetachSession detaches a client from a session.
func (c *httpClient) DetachSession(sessionID, clientID string) error {
	_, err := c.callAPI("session.detach", map[string]string{"session_id": sessionID, "client_id": clientID})
	return err
}

// GetMostRecentSession returns the most recent session.
func (c *httpClient) GetMostRecentSession() (*types.Session, error) {
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

// GetSessionMessages retrieves session messages.
func (c *httpClient) GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error) {
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

// SaveSessionMessages saves messages to a session.
func (c *httpClient) SaveSessionMessages(sessionID string, messages []types.SessionMessage) error {
	_, err := c.callAPI("session.messages.save", map[string]any{"session_id": sessionID, "messages": messages})
	return err
}

// UpdateSessionDescription updates a session description.
func (c *httpClient) UpdateSessionDescription(sessionID, description string) error {
	_, err := c.callAPI("session.update_description", map[string]string{"session_id": sessionID, "description": description})
	return err
}

// GenerateSessionDescription generates a session description.
func (c *httpClient) GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error) {
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

// DeleteSession deletes a session.
func (c *httpClient) DeleteSession(sessionID string) error {
	_, err := c.callAPI("session.delete", map[string]string{"id": sessionID})
	return err
}

// StopSession stops a session.
func (c *httpClient) StopSession(sessionID string) (*types.StopSessionResponse, error) {
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

// GetSessionChildTasks returns child task IDs for a session.
func (c *httpClient) GetSessionChildTasks(sessionID string) ([]string, error) {
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

// ListTasksExtended lists tasks with extended fields.
func (c *httpClient) ListTasksExtended() (*types.TaskExtendedListResponse, error) {
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

// ListTaskSteps returns steps for a task.
func (c *httpClient) ListTaskSteps(taskID string) (*types.TaskStepsResponse, error) {
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
func (c *httpClient) DeleteTask(taskID string) error {
	_, err := c.callAPI("task.delete", map[string]string{"id": taskID})
	return err
}

// CancelTask cancels a task.
func (c *httpClient) CancelTask(taskID string) error {
	_, err := c.callAPI("task.cancel", map[string]string{"id": taskID})
	return err
}

// LinkTaskSession links a session to a task.
func (c *httpClient) LinkTaskSession(taskID, sessionID string) error {
	_, err := c.callAPI("task.link", map[string]string{"task_id": taskID, "session_id": sessionID})
	return err
}

// UnlinkTaskSession removes a session link from a task.
func (c *httpClient) UnlinkTaskSession(taskID, sessionID string) error {
	_, err := c.callAPI("task.unlink", map[string]string{"task_id": taskID, "session_id": sessionID})
	return err
}

// Queue methods

// RetryQueueJob retries a failed job.
func (c *httpClient) RetryQueueJob(jobID string) error {
	_, err := c.callAPI("queue.retry", map[string]string{"job_id": jobID})
	return err
}

// Worker methods

// ListPoolWorkers lists worker pool workers.
func (c *httpClient) ListPoolWorkers() (*types.WorkerPoolResponse, error) {
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

// GetWorkerPoolStats returns worker pool statistics.
func (c *httpClient) GetWorkerPoolStats() (*types.WorkerPoolStats, error) {
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

// ScaleWorkerPool scales the worker pool to the given size.
func (c *httpClient) ScaleWorkerPool(targetCount int) error {
	_, err := c.callAPI("worker.scale", map[string]int{"target_count": targetCount})
	return err
}
