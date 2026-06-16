// Package tui provides the terminal user interface for meept.
package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// clientConnection holds resolved connection settings for a single CLI invocation.
type clientConnection struct {
	transport  string // "rpc", "http", or "auto"
	address    string // socket path or "host:port"
	timeout    time.Duration
	maxRetries int
	retryDelay time.Duration
}

// resolveConnection reads client.json5 and returns connection settings.
func resolveConnection() *clientConnection {
	cfg, err := LoadClientConfig()
	if err != nil {
		// Fall back to defaults
		cfg = DefaultClientConfig()
	}

	conn := cfg.Connection.unwrap()
	return conn
}

// NewDaemonClient creates a DaemonClient using settings from client.json5.
// It respects client.transport ("rpc", "http", "auto") and client.connection settings.
func NewDaemonClient() DaemonClient {
	conn := resolveConnection()
	if conn.transport == "http" {
		return NewHTTPClient(conn.address)
	}
	return NewRPCClient(conn.address)
}

// NewDaemonClientWithAddress creates a DaemonClient connected to a specific address.
// The address is a Unix socket path for RPC transport.
func NewDaemonClientWithAddress(address string) DaemonClient {
	return NewRPCClient(address)
}

// ConnectionConfig holds client-side connection settings.
type ConnectionConfig struct {
	// Transport selects the transport to use: "rpc", "http", or "auto" (default: "auto")
	Transport string `json:"transport"`

	// Address overrides the default daemon endpoint (socket path for RPC, "host:port" for HTTP)
	Address string `json:"address"`

	// Timeout is the default connection timeout (default: 5s, format: e.g. "5s" or "10s")
	Timeout string `json:"timeout"`

	// Retry holds reconnection settings
	Retry RetryConfig `json:"retry"`
}

// RetryConfig holds retry settings.
type RetryConfig struct {
	Attempts int    `json:"attempts"` // Max retry attempts (default: 3)
	Delay    string `json:"delay"`    // Delay between retries (default: "500ms")
}

// unwrap returns a resolved clientConnection with defaults applied.
func (c *ConnectionConfig) unwrap() *clientConnection {
	conn := &clientConnection{
		transport:  "rpc",
		address:    "~/.meept/meept.sock",
		timeout:    5 * time.Second,
		maxRetries: 3,
		retryDelay: 500 * time.Millisecond,
	}

	if c.Transport != "" {
		conn.transport = c.Transport
	}
	if c.Address != "" {
		conn.address = c.Address
	}
	if c.Timeout != "" {
		if d, err := time.ParseDuration(c.Timeout); err == nil {
			conn.timeout = d
		}
	}
	if c.Retry.Attempts > 0 {
		conn.maxRetries = c.Retry.Attempts
	}
	if c.Retry.Delay != "" {
		if d, err := time.ParseDuration(c.Retry.Delay); err == nil {
			conn.retryDelay = d
		}
	}

	// "auto" defaults to RPC for now
	if conn.transport == "auto" {
		conn.transport = "rpc"
	}

	// Expand ~ to home directory for socket path
	if conn.address[:1] == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			conn.address = filepath.Join(home, conn.address[1:])
		}
	}

	return conn
}
