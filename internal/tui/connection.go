// Package tui provides the terminal user interface for meept.
package tui

import (
	"encoding/json"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

// DaemonClient is the interface used by CLI commands to communicate with the daemon.
// Both RPCClient and any future HTTP client implement this interface.
type DaemonClient interface {
	// Connection lifecycle
	Connect() error
	Close() error
	IsConnected() bool

	// Core methods used by CLI commands
	Chat(message, conversationID string) (string, error)
	Status() (*types.DaemonStatusResponse, error)
	ListJobs() (*types.JobListResponse, error)
	QueryMemory(query string, limit int) (*types.MemoryQueryResponse, error)
	GetRecentMemories(limit int) (*types.MemoryQueryResponse, error)
	ListWorkers() (*types.WorkerListResponse, error)
	CreateSession(name string) (*types.Session, error)
	ListSessions() (*types.SessionListResponse, error)
	AttachSession(sessionID, clientID string) error
	DetachSession(sessionID, clientID string) error
	GetMostRecentSession() (*types.Session, error)
	DeleteSession(sessionID string) error
	SaveSessionMessages(sessionID string, messages []types.SessionMessage) error
	GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error)
	UpdateSessionDescription(sessionID, description string) error
	GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error)
	StopSession(sessionID string) (*types.StopSessionResponse, error)
	GetSessionChildTasks(sessionID string) ([]string, error)
	CreateTask(name, description string) (*types.Task, error)
	GetTask(taskID string) (*types.Task, error)
	ListTasks(state string, limit int) (*types.TaskListResponse, error)
	ListTasksExtended() (*types.TaskExtendedListResponse, error)
	ListTaskSteps(taskID string) (*types.TaskStepsResponse, error)
	DeleteTask(taskID string) error
	CancelTask(taskID string) error
	LinkTaskSession(taskID, sessionID string) error
	UnlinkTaskSession(taskID, sessionID string) error
	GetQueueStats() (*types.QueueStatsResponse, error)
	ListQueueJobs(state string, limit int) (*types.QueueJobListResponse, error)
	RetryQueueJob(jobID string) error
	GetWorkerPoolStats() (*types.WorkerPoolStats, error)
	ListPoolWorkers() (*types.WorkerPoolResponse, error)
	ScaleWorkerPool(targetCount int) error
	CacheStats() (*types.CacheStatsResponse, error)
	CacheClear() error
	CacheInvalidate(filePath string) error

	// Low-level call for extensibility (selfimprove, dev commands)
	Call(method string, params any) (json.RawMessage, error)

	// Configuration
	SetTimeout(d time.Duration)
}

// Ensure RPCClient implements DaemonClient at compile time.
var _ DaemonClient = (*RPCClient)(nil)

// NewDaemonClient creates a DaemonClient connected to the default socket path.
// Note: This function is largely unused; most callers use NewRPCClient directly.
func NewDaemonClient() DaemonClient {
	return NewRPCClient("~/.meept/meept.sock")
}

// NewDaemonClientWithAddress creates a DaemonClient connected to a specific address.
// The address is a Unix socket path for RPC transport.
func NewDaemonClientWithAddress(address string) DaemonClient {
	return NewRPCClient(address)
}
