// Package transport provides SDK-backed HTTP client implementation.
package transport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
	meeptclient "github.com/caimlas/meept/sdk/go"
)

// SDKClient implements Client using the generated OpenAPI SDK.
type SDKClient struct {
	cfg     *meeptclient.Configuration
	http    *http.Client
	baseURL string
	apiKey  string
	timeout time.Duration
}

// NewSDKClient creates a new SDK-backed HTTP client.
func NewSDKClient(baseURL string, timeout time.Duration, apiKey string) *SDKClient {
	cfg := meeptclient.NewConfiguration()
	cfg.HTTPClient = &http.Client{Timeout: timeout}
	if apiKey != "" {
		cfg.AddDefaultHeader("Authorization", "Bearer "+apiKey)
	}

	return &SDKClient{
		cfg:     cfg,
		http:    cfg.HTTPClient,
		baseURL: baseURL,
		apiKey:  apiKey,
		timeout: timeout,
	}
}

// Connect verifies the endpoint is reachable.
func (c *SDKClient) Connect() error {
	resp, err := c.http.Get(c.baseURL + "/api/v1/health")
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
func (c *SDKClient) Close() error { return nil }

// IsConnected checks if the daemon is reachable.
func (c *SDKClient) IsConnected() bool {
	resp, err := c.http.Get(c.baseURL + "/api/v1/health")
	return err == nil && resp.StatusCode == http.StatusOK
}

// SetTimeout sets the request timeout.
func (c *SDKClient) SetTimeout(d time.Duration) {
	c.timeout = d
	c.http.Timeout = d
}

// Call makes a generic API call via the bus proxy.
func (c *SDKClient) Call(method string, params any) (json.RawMessage, error) {
	payload := map[string]any{
		"method": method,
		"params": params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Post(c.baseURL+"/api/v1/bus/call", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("[%d] %s", result.Error.Code, result.Error.Message)
	}
	return result.Result, nil
}

// Chat sends a chat message and returns the response.
func (c *SDKClient) Chat(message, conversationID string) (string, error) {
	req := meeptclient.NewChatRequest(message, conversationID)

	reqBody, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal chat request: %w", err)
	}

	resp, err := c.cfg.HTTPClient.Post(
		c.baseURL+"/api/v1/chat",
		"application/json",
		bytes.NewReader(reqBody),
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

// Status gets daemon status.
func (c *SDKClient) Status() (*types.DaemonStatusResponse, error) {
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
func (c *SDKClient) ListJobs() (*types.JobListResponse, error) {
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
func (c *SDKClient) QueryMemory(query string, limit int) (*types.MemoryQueryResponse, error) {
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
func (c *SDKClient) GetRecentMemories(limit int) (*types.MemoryQueryResponse, error) {
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
func (c *SDKClient) ListWorkers() (*types.WorkerListResponse, error) {
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

// GetQueueStats gets queue statistics.
func (c *SDKClient) GetQueueStats() (*types.QueueStatsResponse, error) {
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
func (c *SDKClient) ListQueueJobs(state string, limit int) (*types.QueueJobListResponse, error) {
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
func (c *SDKClient) ListTasks(state string, limit int) (*types.TaskListResponse, error) {
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
func (c *SDKClient) CreateTask(name, description string) (*types.Task, error) {
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
func (c *SDKClient) GetTask(taskID string) (*types.Task, error) {
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

// CacheStats gets cache statistics.
func (c *SDKClient) CacheStats() (*types.CacheStatsResponse, error) {
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
func (c *SDKClient) CacheClear() error {
	_, err := c.callAPI("cache.clear", nil)
	return err
}

// CacheInvalidate invalidates cache entries.
func (c *SDKClient) CacheInvalidate(filePath string) error {
	_, err := c.callAPI("cache.invalidate", map[string]string{"file_path": filePath})
	return err
}

// CacheInspect inspects a cache entry.
func (c *SDKClient) CacheInspect(promptHash string) (*types.CacheInspectResponse, error) {
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

func (c *SDKClient) ListSessions() (*types.SessionListResponse, error) {
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

func (c *SDKClient) CreateSession(name string) (*types.Session, error) {
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

func (c *SDKClient) AttachSession(sessionID, clientID string) error {
	_, err := c.callAPI("session.attach", map[string]string{"session_id": sessionID, "client_id": clientID})
	return err
}

func (c *SDKClient) DetachSession(sessionID, clientID string) error {
	_, err := c.callAPI("session.detach", map[string]string{"session_id": sessionID, "client_id": clientID})
	return err
}

func (c *SDKClient) GetMostRecentSession() (*types.Session, error) {
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

func (c *SDKClient) GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error) {
	result, err := c.callAPI("session.messages.get", map[string]any{
		"session_id": sessionID,
		"offset":     offset,
		"limit":      limit,
	})
	if err != nil {
		return nil, err
	}
	var resp types.SessionMessagesResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *SDKClient) SaveSessionMessages(sessionID string, messages []types.SessionMessage) error {
	_, err := c.callAPI("session.messages.save", map[string]any{
		"session_id": sessionID,
		"messages":   messages,
	})
	return err
}

func (c *SDKClient) UpdateSessionDescription(sessionID, description string) error {
	_, err := c.callAPI("session.update_description", map[string]string{
		"session_id":  sessionID,
		"description": description,
	})
	return err
}

func (c *SDKClient) GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error) {
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

func (c *SDKClient) DeleteSession(sessionID string) error {
	_, err := c.callAPI("session.delete", map[string]string{"id": sessionID})
	return err
}

func (c *SDKClient) StopSession(sessionID string) (*types.StopSessionResponse, error) {
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

func (c *SDKClient) GetSessionChildTasks(sessionID string) ([]string, error) {
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

func (c *SDKClient) ListTasksExtended() (*types.TaskExtendedListResponse, error) {
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

func (c *SDKClient) ListTaskSteps(taskID string) (*types.TaskStepsResponse, error) {
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

func (c *SDKClient) DeleteTask(taskID string) error {
	_, err := c.callAPI("task.delete", map[string]string{"id": taskID})
	return err
}

func (c *SDKClient) CancelTask(taskID string) error {
	_, err := c.callAPI("task.cancel", map[string]string{"id": taskID})
	return err
}

func (c *SDKClient) LinkTaskSession(taskID, sessionID string) error {
	_, err := c.callAPI("task.link", map[string]string{
		"task_id":    taskID,
		"session_id": sessionID,
	})
	return err
}

func (c *SDKClient) UnlinkTaskSession(taskID, sessionID string) error {
	_, err := c.callAPI("task.unlink", map[string]string{
		"task_id":    taskID,
		"session_id": sessionID,
	})
	return err
}

// Queue methods

func (c *SDKClient) RetryQueueJob(jobID string) error {
	_, err := c.callAPI("queue.retry", map[string]string{"job_id": jobID})
	return err
}

// Worker pool methods

func (c *SDKClient) GetWorkerPoolStats() (*types.WorkerPoolStats, error) {
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

func (c *SDKClient) ListPoolWorkers() (*types.WorkerPoolResponse, error) {
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

func (c *SDKClient) ScaleWorkerPool(targetCount int) error {
	_, err := c.callAPI("worker.scale", map[string]int{"target_count": targetCount})
	return err
}

// callAPI sends a JSON POST to the daemon bus proxy endpoint.
func (c *SDKClient) callAPI(method string, params any) (json.RawMessage, error) {
	return c.Call(method, params)
}


// Ensure SDKClient implements Client at compile time.
var _ Client = (*SDKClient)(nil)
