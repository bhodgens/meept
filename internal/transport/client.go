package transport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
	"github.com/caimlas/meept/pkg/constants"
	"github.com/caimlas/meept/pkg/tlsutil"
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
	Transport          string        // "rpc" or "http"
	SocketPath         string        // For RPC transport
	HTTPBaseURL        string        // For HTTP transport (e.g. "https://localhost:8081")
	InsecureSkipVerify bool          // Skip TLS certificate verification (for self-signed certs)
	Timeout            time.Duration // Per-call timeout
}

// DefaultConfig returns the default client transport config.
//
// InsecureSkipVerify defaults to true only for loopback targets when no
// fingerprint pin is available — matching the historical out-of-the-box
// behaviour for self-signed localhost certs. Non-loopback targets default to
// false, and a fingerprint pin (loaded from ~/.meept/tls/fingerprint.txt via
// transport.New) always takes precedence and forces chain validation on.
func DefaultConfig() *Config {
	httpBase := "https://localhost:8081"
	return &Config{
		Transport:          "rpc",
		SocketPath:         "~/.meept/meept.sock",
		HTTPBaseURL:        httpBase,
		InsecureSkipVerify: isLoopbackBaseURL(httpBase),
		Timeout:            120 * time.Second,
	}
}

// New creates a transport client based on config.
func New(cfg *Config) (Client, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	switch cfg.Transport {
	case "http":
		// Use SDK-backed client for HTTP transport
		apiKey := constants.DefaultDevAPIKey
		if certFP, spkiFP := loadFingerprint(); certFP != "" || spkiFP != "" {
			// Fingerprint loading succeeds, use pinned cert client
			opts := []HTTPClientOption{WithInsecureSkipVerify(cfg.InsecureSkipVerify)}
			opts = append(opts, WithPinnedFingerprint(certFP, spkiFP))
			opts = append(opts, WithAPIKey(apiKey))
			return NewHTTPClient(cfg.HTTPBaseURL, cfg.Timeout, opts...), nil
		}
		// No fingerprint, use SDK client with default TLS
		return NewSDKClient(cfg.HTTPBaseURL, cfg.Timeout, apiKey), nil
	case "rpc", "unix", "socket":
		return NewRPCClient(cfg.SocketPath, cfg.Timeout), nil
	default:
		return nil, fmt.Errorf("unknown transport: %s", cfg.Transport)
	}
}

// loadFingerprint attempts to read the server cert fingerprint from the
// default location (~/.meept/tls/fingerprint.txt).
func loadFingerprint() (certFP, spkiFP string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	fpPath := filepath.Join(homeDir, ".meept", "tls", "fingerprint.txt")
	certFP, spkiFP, err = tlsutil.LoadExpectedFingerprint(fpPath)
	if err != nil {
		return "", ""
	}
	return certFP, spkiFP
}
