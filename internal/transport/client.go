package transport

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

// Client is the unified interface for talking to the daemon.
// Both RPC (unix socket) and HTTP implementations satisfy this.
type Client interface {
	// Connect establishes the transport connection.
	Connect() error
	// Close tears down the connection.
	Close() error
	// IsConnected returns true if the underlying connection is alive.
	IsConnected() bool

	// Core methods used by both CLI and TUI
	Chat(message, conversationID string) (string, error)
	Status() (*types.DaemonStatusResponse, error)
	ListJobs() (*types.JobListResponse, error)
	QueryMemory(query string, limit int) (*types.MemoryQueryResponse, error)
	GetRecentMemories(limit int) (*types.MemoryQueryResponse, error)
	ListWorkers() (*types.WorkerListResponse, error)
	GetQueueStats() (*types.QueueStatsResponse, error)
	ListQueueJobs(state string, limit int) (*types.QueueJobListResponse, error)
	ListTasks(state string, limit int) (*types.TaskListResponse, error)
	CreateTask(name, description string) (*types.Task, error)
	GetTask(taskID string) (*types.Task, error)
	CacheStats() (*types.CacheStatsResponse, error)
	CacheClear() error
	CacheInvalidate(filePath string) error
	CacheInspect(promptHash string) (*types.CacheInspectResponse, error)

	// Session methods
	ListSessions() (*types.SessionListResponse, error)
	CreateSession(name string) (*types.Session, error)
	AttachSession(sessionID, clientID string) error
	DetachSession(sessionID, clientID string) error
	GetMostRecentSession() (*types.Session, error)
	GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error)
	SaveSessionMessages(sessionID string, messages []types.SessionMessage) error
	UpdateSessionDescription(sessionID, description string) error
	GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error)
	DeleteSession(sessionID string) error
	StopSession(sessionID string) (*types.StopSessionResponse, error)
	GetSessionChildTasks(sessionID string) ([]string, error)

	// Task methods
	ListTasksExtended() (*types.TaskExtendedListResponse, error)
	ListTaskSteps(taskID string) (*types.TaskStepsResponse, error)
	DeleteTask(taskID string) error
	CancelTask(taskID string) error
	LinkTaskSession(taskID, sessionID string) error
	UnlinkTaskSession(taskID, sessionID string) error

	// Queue methods
	RetryQueueJob(jobID string) error

	// Worker methods
	ListPoolWorkers() (*types.WorkerPoolResponse, error)
	GetWorkerPoolStats() (*types.WorkerPoolStats, error)
	ScaleWorkerPool(targetCount int) error

	// Low-level call for extensibility (skills, dev, selfimprove commands).
	Call(method string, params any) (json.RawMessage, error)

	// SetTimeout adjusts the per-call timeout.
	SetTimeout(d time.Duration)
}

// Config holds client-side transport configuration.
type Config struct {
	Transport   string        // "rpc" or "http"
	SocketPath  string        // For RPC transport
	HTTPBaseURL string        // For HTTP transport (e.g. "http://localhost:8081")
	Timeout     time.Duration // Per-call timeout
}

// DefaultConfig returns the default client transport config.
func DefaultConfig() *Config {
	return &Config{
		Transport:   "rpc",
		SocketPath:  "~/.meept/meept.sock",
		HTTPBaseURL: "http://localhost:8081",
		Timeout:     120 * time.Second,
	}
}

// New creates a transport client based on config.
func New(cfg *Config) (Client, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	switch cfg.Transport {
	case "http":
		return NewHTTPClient(cfg.HTTPBaseURL, cfg.Timeout), nil
	case "rpc", "unix", "socket":
		return NewRPCClient(cfg.SocketPath, cfg.Timeout), nil
	default:
		return nil, fmt.Errorf("unknown transport: %s", cfg.Transport)
	}
}
