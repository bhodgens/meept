package transport

import (
	"time"

	"github.com/caimlas/meept/internal/tui"
	"github.com/caimlas/meept/internal/tui/types"
)

// rpcAdapter adapts tui.RPCClient to the transport.Client interface.
type rpcAdapter struct {
	client *tui.RPCClient
}

// NewRPCClient creates an RPC-backed transport client.
func NewRPCClient(socketPath string, timeout time.Duration) Client {
	c := tui.NewRPCClient(socketPath)
	if timeout > 0 {
		c.SetTimeout(timeout)
	}
	return &rpcAdapter{client: c}
}

func (a *rpcAdapter) Connect() error { return a.client.Connect() }
func (a *rpcAdapter) Close() error   { return a.client.Close() }
func (a *rpcAdapter) IsConnected() bool { return a.client.IsConnected() }

func (a *rpcAdapter) Chat(message, conversationID string) (string, error) { return a.client.Chat(message, conversationID) }
func (a *rpcAdapter) Status() (*types.DaemonStatusResponse, error)       { return a.client.Status() }
func (a *rpcAdapter) ListJobs() (*types.JobListResponse, error)           { return a.client.ListJobs() }
func (a *rpcAdapter) QueryMemory(query string, limit int) (*types.MemoryQueryResponse, error) { return a.client.QueryMemory(query, limit) }
func (a *rpcAdapter) GetRecentMemories(limit int) (*types.MemoryQueryResponse, error)        { return a.client.GetRecentMemories(limit) }
func (a *rpcAdapter) ListWorkers() (*types.WorkerListResponse, error)     { return a.client.ListWorkers() }
func (a *rpcAdapter) GetQueueStats() (*types.QueueStatsResponse, error)   { return a.client.GetQueueStats() }
func (a *rpcAdapter) ListQueueJobs(state string, limit int) (*types.QueueJobListResponse, error) { return a.client.ListQueueJobs(state, limit) }
func (a *rpcAdapter) ListTasks(state string, limit int) (*types.TaskListResponse, error)         { return a.client.ListTasks(state, limit) }
func (a *rpcAdapter) CreateTask(name, description string) (*types.Task, error)                  { return a.client.CreateTask(name, description) }
func (a *rpcAdapter) GetTask(taskID string) (*types.Task, error)                                  { return a.client.GetTask(taskID) }
func (a *rpcAdapter) CacheStats() (*types.CacheStatsResponse, error)    { return a.client.CacheStats() }
func (a *rpcAdapter) CacheClear() error                                  { return a.client.CacheClear() }
func (a *rpcAdapter) CacheInvalidate(filePath string) error              { return a.client.CacheInvalidate(filePath) }
func (a *rpcAdapter) ListSessions() (*types.SessionListResponse, error)  { return a.client.ListSessions() }
func (a *rpcAdapter) CreateSession(name string) (*types.Session, error)  { return a.client.CreateSession(name) }
func (a *rpcAdapter) AttachSession(sessionID, clientID string) error     { return a.client.AttachSession(sessionID, clientID) }
func (a *rpcAdapter) DetachSession(sessionID, clientID string) error     { return a.client.DetachSession(sessionID, clientID) }
func (a *rpcAdapter) GetMostRecentSession() (*types.Session, error)      { return a.client.GetMostRecentSession() }
func (a *rpcAdapter) GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error) { return a.client.GetSessionMessages(sessionID, offset, limit) }
func (a *rpcAdapter) SaveSessionMessages(sessionID string, messages []types.SessionMessage) error                  { return a.client.SaveSessionMessages(sessionID, messages) }
func (a *rpcAdapter) UpdateSessionDescription(sessionID, description string) error                               { return a.client.UpdateSessionDescription(sessionID, description) }
func (a *rpcAdapter) GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error) { return a.client.GenerateSessionDescription(sessionID, firstMessage, projectName) }
func (a *rpcAdapter) DeleteSession(sessionID string) error                                                     { return a.client.DeleteSession(sessionID) }
func (a *rpcAdapter) StopSession(sessionID string) (*types.StopSessionResponse, error)                         { return a.client.StopSession(sessionID) }
func (a *rpcAdapter) GetSessionChildTasks(sessionID string) ([]string, error)                                 { return a.client.GetSessionChildTasks(sessionID) }
func (a *rpcAdapter) ListTasksExtended() (*types.TaskExtendedListResponse, error)                            { return a.client.ListTasksExtended() }
func (a *rpcAdapter) ListTaskSteps(taskID string) (*types.TaskStepsResponse, error)                          { return a.client.ListTaskSteps(taskID) }
func (a *rpcAdapter) DeleteTask(taskID string) error                                                          { return a.client.DeleteTask(taskID) }
func (a *rpcAdapter) CancelTask(taskID string) error                                                          { return a.client.CancelTask(taskID) }
func (a *rpcAdapter) LinkTaskSession(taskID, sessionID string) error                                         { return a.client.LinkTaskSession(taskID, sessionID) }
func (a *rpcAdapter) UnlinkTaskSession(taskID, sessionID string) error                                       { return a.client.UnlinkTaskSession(taskID, sessionID) }
func (a *rpcAdapter) RetryQueueJob(jobID string) error                                                        { return a.client.RetryQueueJob(jobID) }
func (a *rpcAdapter) ListPoolWorkers() (*types.WorkerPoolResponse, error)                                    { return a.client.ListPoolWorkers() }
func (a *rpcAdapter) GetWorkerPoolStats() (*types.WorkerPoolStats, error)                                     { return a.client.GetWorkerPoolStats() }
func (a *rpcAdapter) ScaleWorkerPool(targetCount int) error                                                   { return a.client.ScaleWorkerPool(targetCount) }
