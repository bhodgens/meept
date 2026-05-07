package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

// httpClient implements transport.Client over HTTP REST.
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
	req, _ := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/health", nil)
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *httpClient) Chat(message, conversationID string) (string, error) {
	return "", fmt.Errorf("chat over HTTP not yet implemented; use --transport=rpc")
}

func (c *httpClient) Status() (*types.DaemonStatusResponse, error) {
	resp, err := c.client.Get(c.baseURL + "/api/v1/daemon/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	return &types.DaemonStatusResponse{
		Status:            "running",
		UptimeSeconds:     0,
		RegisteredMethods: []string{},
	}, nil
}

func (c *httpClient) doNotImplemented(name string) error {
	return fmt.Errorf("%s over HTTP not yet implemented; use --transport=rpc", name)
}

func (c *httpClient) ListJobs() (*types.JobListResponse, error)                { return nil, c.doNotImplemented("ListJobs") }
func (c *httpClient) QueryMemory(query string, limit int) (*types.MemoryQueryResponse, error) {
	return nil, c.doNotImplemented("QueryMemory")
}
func (c *httpClient) GetRecentMemories(limit int) (*types.MemoryQueryResponse, error) {
	return nil, c.doNotImplemented("GetRecentMemories")
}
func (c *httpClient) ListWorkers() (*types.WorkerListResponse, error)            { return nil, c.doNotImplemented("ListWorkers") }
func (c *httpClient) GetQueueStats() (*types.QueueStatsResponse, error)          { return nil, c.doNotImplemented("GetQueueStats") }
func (c *httpClient) ListQueueJobs(state string, limit int) (*types.QueueJobListResponse, error) {
	return nil, c.doNotImplemented("ListQueueJobs")
}
func (c *httpClient) ListTasks(state string, limit int) (*types.TaskListResponse, error)       { return nil, c.doNotImplemented("ListTasks") }
func (c *httpClient) CreateTask(name, description string) (*types.Task, error)   { return nil, c.doNotImplemented("CreateTask") }
func (c *httpClient) GetTask(taskID string) (*types.Task, error)                 { return nil, c.doNotImplemented("GetTask") }
func (c *httpClient) CacheStats() (*types.CacheStatsResponse, error)             { return nil, c.doNotImplemented("CacheStats") }
func (c *httpClient) CacheClear() error                                          { return c.doNotImplemented("CacheClear") }
func (c *httpClient) CacheInvalidate(filePath string) error                      { return c.doNotImplemented("CacheInvalidate") }
func (c *httpClient) ListSessions() (*types.SessionListResponse, error)          { return nil, c.doNotImplemented("ListSessions") }
func (c *httpClient) CreateSession(name string) (*types.Session, error)          { return nil, c.doNotImplemented("CreateSession") }
func (c *httpClient) AttachSession(sessionID, clientID string) error             { return c.doNotImplemented("AttachSession") }
func (c *httpClient) DetachSession(sessionID, clientID string) error             { return c.doNotImplemented("DetachSession") }
func (c *httpClient) GetMostRecentSession() (*types.Session, error)              { return nil, c.doNotImplemented("GetMostRecentSession") }
func (c *httpClient) GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error) {
	return nil, c.doNotImplemented("GetSessionMessages")
}
func (c *httpClient) SaveSessionMessages(sessionID string, messages []types.SessionMessage) error {
	return c.doNotImplemented("SaveSessionMessages")
}
func (c *httpClient) UpdateSessionDescription(sessionID, description string) error { return c.doNotImplemented("UpdateSessionDescription") }
func (c *httpClient) GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error) {
	return nil, c.doNotImplemented("GenerateSessionDescription")
}
func (c *httpClient) DeleteSession(sessionID string) error                       { return c.doNotImplemented("DeleteSession") }
func (c *httpClient) StopSession(sessionID string) (*types.StopSessionResponse, error) {
	return nil, c.doNotImplemented("StopSession")
}
func (c *httpClient) GetSessionChildTasks(sessionID string) ([]string, error)    { return nil, c.doNotImplemented("GetSessionChildTasks") }
func (c *httpClient) ListTasksExtended() (*types.TaskExtendedListResponse, error) { return nil, c.doNotImplemented("ListTasksExtended") }
func (c *httpClient) ListTaskSteps(taskID string) (*types.TaskStepsResponse, error) { return nil, c.doNotImplemented("ListTaskSteps") }
func (c *httpClient) DeleteTask(taskID string) error                             { return c.doNotImplemented("DeleteTask") }
func (c *httpClient) CancelTask(taskID string) error                             { return c.doNotImplemented("CancelTask") }
func (c *httpClient) LinkTaskSession(taskID, sessionID string) error             { return c.doNotImplemented("LinkTaskSession") }
func (c *httpClient) UnlinkTaskSession(taskID, sessionID string) error           { return c.doNotImplemented("UnlinkTaskSession") }
func (c *httpClient) RetryQueueJob(jobID string) error                           { return c.doNotImplemented("RetryQueueJob") }
func (c *httpClient) ListPoolWorkers() (*types.WorkerPoolResponse, error)        { return nil, c.doNotImplemented("ListPoolWorkers") }
func (c *httpClient) GetWorkerPoolStats() (*types.WorkerPoolStats, error)         { return nil, c.doNotImplemented("GetWorkerPoolStats") }
func (c *httpClient) ScaleWorkerPool(targetCount int) error                      { return c.doNotImplemented("ScaleWorkerPool") }

// Helper to read a response body and close it.
func (c *httpClient) readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}
