package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/internal/security"
)

// Manager is the unified facade over episodic, task, and personality memory.
// It provides a single entry-point for the rest of meept to interact with
// the memory system. Supports memvid as primary backend with SQLite fallback.
type Manager struct {
	config       config.MemoryConfig
	memvidCfg    config.MemvidConfig
	distributedCfg config.DistributedMemoryConfig
	dataDir      string

	// Memvid client (primary backend when configured)
	memvid    *memvid.Client
	useMemvid bool // true if memvid is active for Store/Search

	// SQLite backends (fallback or when explicitly configured)
	episodic    *EpisodicMemory
	task        *TaskMemory
	personality *PersonalityMemory

	// Knowledge graph for relationship tracking and PageRank scoring
	graph *KnowledgeGraph

	// Distributed sync (when distributed_memory.enabled && mode == "distributed")
	distributed bool

	consolidator *Consolidator
	initialized  bool
	mu           sync.RWMutex
	logger       *slog.Logger

	// Security components for memory store validation
	sanitizer    *security.InputSanitizer
	securityCfg  config.MemorySecurityConfig

	// Prefetch cache and service for automatic context retrieval (Hermes pattern)
	prefetchCache    sync.Map // map[string]string - query -> cached context
	prefetchQueue    chan prefetchRequest
	prefetchShutdown chan struct{}
	prefetchWg       sync.WaitGroup
}

// prefetchRequest represents a request to prefetch context for a query.
type prefetchRequest struct {
	query    string
	maxItems int
}

// ManagerConfig holds configuration for creating a Manager.
type ManagerConfig struct {
	// Config is the memory configuration from meept.toml.
	Config config.MemoryConfig
	// MemvidConfig is the memvid service configuration.
	MemvidConfig config.MemvidConfig
	// DistributedConfig is the distributed memory sync configuration.
	DistributedConfig config.DistributedMemoryConfig
	// Logger for operations.
	Logger *slog.Logger
	// Sanitizer is the input sanitizer for memory store validation.
	Sanitizer *security.InputSanitizer
	// SecurityConfig is the memory security configuration.
	SecurityConfig config.MemorySecurityConfig
}

// NewManager creates a new memory manager.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Manager{
		config:         cfg.Config,
		memvidCfg:      cfg.MemvidConfig,
		distributedCfg: cfg.DistributedConfig,
		logger:         cfg.Logger,
		sanitizer:      cfg.Sanitizer,
		securityCfg:    cfg.SecurityConfig,
	}
}

// Initialize bootstraps every enabled memory subsystem.
func (m *Manager) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.initialized {
		return nil
	}

	// Expand data directory path
	dataDir := m.config.DataDir
	if dataDir == "" {
		dataDir = "~/.meept/memory"
	}
	if len(dataDir) > 0 && dataDir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		dataDir = filepath.Join(home, dataDir[1:])
	}
	m.dataDir = dataDir

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Determine backend strategy
	wantMemvid := m.config.Backend == "" || m.config.Backend == config.MemoryBackendMemvid
	wantSQLite := m.config.Backend == config.MemoryBackendSQLite

	// Try memvid if it's the desired backend
	if wantMemvid && m.memvidCfg.Enabled {
		m.memvid = memvid.NewClient(memvid.ClientConfig{
			Endpoint: m.memvidCfg.Endpoint,
			Zone:     "default",
			Timeout:  time.Duration(m.memvidCfg.Timeout) * time.Second,
		})

		if m.memvid.IsAvailable(ctx) {
			m.useMemvid = true
			m.logger.Info("Memvid backend active",
				"endpoint", m.memvidCfg.Endpoint,
			)
		} else {
			m.logger.Warn("Memvid configured but unavailable, falling back to SQLite",
				"endpoint", m.memvidCfg.Endpoint,
			)
			m.memvid = nil
			wantSQLite = true // fall back
		}
	} else if wantMemvid && !m.memvidCfg.Enabled {
		m.logger.Info("Memvid backend selected but not enabled in [memvid] config, using SQLite")
		wantSQLite = true
	}

	// Initialize SQLite backends when needed (explicit sqlite backend, or memvid fallback)
	if wantSQLite || !m.useMemvid {
		if err := m.initSQLiteBackends(ctx); err != nil {
			return err
		}
	}

	// Personality always uses local storage (markdown files, not suited for memvid)
	if m.config.Personality.Enabled {
		personalityDir := filepath.Join(dataDir, "personality")
		m.personality = NewPersonalityMemory(PersonalityMemoryConfig{
			DataDir: personalityDir,
			Logger:  m.logger.With("subsystem", "personality"),
		})
		if err := m.personality.Load(ctx); err != nil {
			return fmt.Errorf("failed to load personality: %w", err)
		}
		m.logger.Info("Personality model loaded")
	} else {
		m.logger.Info("Personality model disabled by configuration")
	}

	// Initialize consolidator (only useful for SQLite backends)
	if !m.useMemvid {
		m.consolidator = NewConsolidator(ConsolidatorConfig{
			Manager: m,
			Logger:  m.logger.With("subsystem", "consolidator"),
		})
	}

	// Initialize knowledge graph
	graphDir := filepath.Join(dataDir, "graph")
	m.graph = NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: graphDir,
		Logger:  m.logger.With("subsystem", "graph"),
	})
	if err := m.graph.Initialize(ctx); err != nil {
		m.logger.Warn("Failed to initialize knowledge graph", "error", err)
		// Graph is optional, continue without it
		m.graph = nil
	} else {
		m.logger.Info("Knowledge graph initialized")
	}

	// Check if distributed mode is enabled
	m.distributed = m.distributedCfg.Enabled && m.distributedCfg.Mode == "distributed"

	backend := "sqlite"
	if m.useMemvid {
		backend = "memvid"
	}
	m.initialized = true
	m.logger.Info("MemoryManager fully initialized",
		"backend", backend,
		"data_dir", dataDir,
		"graph_enabled", m.graph != nil,
		"distributed", m.distributed,
	)
	return nil
}

// initSQLiteBackends initializes the SQLite-based episodic and task memory.
func (m *Manager) initSQLiteBackends(ctx context.Context) error {
	if m.config.Episodic.Enabled {
		episodicDir := filepath.Join(m.dataDir, "episodic")
		episodic, err := NewEpisodicMemory(EpisodicConfig{
			DataDir: episodicDir,
			Logger:  m.logger.With("subsystem", "episodic"),
		})
		if err != nil {
			return fmt.Errorf("failed to create episodic memory: %w", err)
		}
		m.episodic = episodic
		if err := m.episodic.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize episodic memory: %w", err)
		}
		fts5Status := "FTS5"
		if !m.episodic.HasFTS5() {
			fts5Status = "LIKE-fallback"
		}
		m.logger.Info("Episodic memory subsystem initialized",
			"backend", "SQLite",
			"search", fts5Status,
		)
	} else {
		m.logger.Info("Episodic memory disabled by configuration")
	}

	if m.config.Task.Enabled {
		taskDir := filepath.Join(m.dataDir, "task")
		domains := m.config.Task.Domains
		if len(domains) == 0 {
			domains = []string{"general", "code", "commands"}
		}
		taskMem, err := NewTaskMemory(TaskMemoryConfig{
			DataDir: taskDir,
			Domains: domains,
			Logger:  m.logger.With("subsystem", "task"),
		})
		if err != nil {
			return fmt.Errorf("failed to create task memory: %w", err)
		}
		m.task = taskMem
		if err := m.task.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize task memory: %w", err)
		}
		fts5Status := "FTS5"
		if !m.task.HasFTS5() {
			fts5Status = "LIKE-fallback"
		}
		m.logger.Info("Task memory subsystem initialized",
			"backend", "SQLite",
			"search", fts5Status,
			"domains", domains,
		)
	} else {
		m.logger.Info("Task memory disabled by configuration")
	}

	return nil
}

// memvidZone maps a MemoryType to a memvid zone name.
func memvidZone(memType MemoryType, category string) string {
	switch memType {
	case MemoryTypeEpisodic:
		return "episodic"
	case MemoryTypeTask:
		if category != "" {
			return "task:" + category
		}
		return "task:general"
	case MemoryTypePersonality:
		return "personality"
	default:
		return "default"
	}
}

// Store persists content in the appropriate memory subsystem.
func (m *Manager) Store(ctx context.Context, mem Memory) (string, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return "", errors.New("memory manager not initialized")
	}
	// Check security configuration
	securityEnabled := m.securityCfg.Enabled
	failClosed := m.securityCfg.FailClosed
	logBlocked := m.securityCfg.LogBlocked
	useMemvid := m.useMemvid
	m.mu.RUnlock()

	// Security scan before storage (if enabled)
	if securityEnabled && m.sanitizer != nil {
		result := m.sanitizer.Sanitize(mem.Content)
		if len(result.ThreatsDetected) > 0 {
			// Build threat summary for logging
			threats := make([]string, len(result.ThreatsDetected))
			for i, t := range result.ThreatsDetected {
				threats[i] = t.Type
			}
			if logBlocked {
				m.logger.Warn("Memory store blocked by security scanner",
					"threats", strings.Join(threats, ", "),
					"agent_id", mem.AgentID,
				)
			}
			if failClosed {
				return "", fmt.Errorf("memory content failed security scan: %s", strings.Join(threats, ", "))
			}
			// If not fail-closed, log and continue (sanitized content)
			mem.Content = result.CleanText
		}
	} else if securityEnabled && m.sanitizer == nil && failClosed {
		// Security enabled but sanitizer not available - fail closed
		return "", errors.New("memory security enabled but sanitizer not initialized")
	}

	// Character limit enforcement (after security scan)
	// Note: ProjectPath is not available on Memory struct, use empty string for global defaults
	limits := m.config.GetLimitsForProject("")
	var limit config.MemoryCategoryLimit
	switch mem.Type {
	case MemoryTypeEpisodic:
		limit = limits.Episodic
	case MemoryTypeTask:
		switch mem.Category {
		case "code":
			limit = limits.TaskCode
		case "commands":
			limit = limits.TaskCommands
		default:
			limit = limits.TaskGeneral
		}
	case MemoryTypePersonality:
		limit = limits.Personality
	default:
		// No limit for unknown types
		limit = config.MemoryCategoryLimit{Enabled: false}
	}

	// Enforce limit
	if limit.Enabled && len(mem.Content) > limit.CharacterLimit {
		return "", fmt.Errorf("memory content exceeds limit of %d characters", limit.CharacterLimit)
	}

	// Route through memvid when active
	if useMemvid && m.memvid != nil {
		return m.storeViaMemvid(ctx, mem)
	}

	// SQLite fallback
	return m.storeViaSQLite(ctx, mem)
}

// storeViaMemvid stores a memory through the memvid service.
func (m *Manager) storeViaMemvid(ctx context.Context, mem Memory) (string, error) {
	zone := memvidZone(mem.Type, mem.Category)
	client := m.memvid.WithZone(zone)

	// Enrich metadata with attribution
	metadata := mem.Metadata
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["type"] = string(mem.Type)
	metadata["category"] = mem.Category
	if mem.AgentID != "" {
		metadata["agent_id"] = mem.AgentID
	}
	if mem.SessionID != "" {
		metadata["session_id"] = mem.SessionID
	}
	if mem.TaskID != "" {
		metadata["task_id"] = mem.TaskID
	}

	id, err := client.Store(ctx, mem.Content, metadata)
	if err != nil {
		m.logger.Warn("Memvid store failed, trying SQLite fallback", "error", err)
		return m.storeViaSQLite(ctx, mem)
	}

	return id, nil
}

// storeViaSQLite stores a memory through the SQLite backends.
func (m *Manager) storeViaSQLite(ctx context.Context, mem Memory) (string, error) {
	switch mem.Type {
	case MemoryTypeEpisodic:
		if m.episodic == nil {
			return "", errors.New("episodic memory is disabled")
		}
		category := mem.Category
		if category == "" {
			category = "conversation"
		}
		return m.episodic.Store(ctx, mem.Content, category, mem.Metadata)

	case MemoryTypeTask:
		if m.task == nil {
			return "", errors.New("task memory is disabled")
		}
		domain := mem.Category
		if domain == "" {
			domain = "general"
		}
		return m.task.Store(ctx, mem.Content, domain, mem.Metadata)

	default:
		return "", fmt.Errorf("unknown memory type: %s", mem.Type)
	}
}

// Search finds memories matching the query.
func (m *Manager) Search(ctx context.Context, query MemoryQuery) ([]MemoryResult, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, errors.New("memory manager not initialized")
	}
	useMemvid := m.useMemvid
	m.mu.RUnlock()

	if query.Limit <= 0 {
		query.Limit = 10
	}

	// Route through memvid when active
	if useMemvid && m.memvid != nil {
		results, err := m.searchViaMemvid(ctx, query)
		if err != nil {
			m.logger.Warn("Memvid search failed, trying SQLite fallback", "error", err)
			return m.searchViaSQLite(ctx, query)
		}
		return results, nil
	}

	return m.searchViaSQLite(ctx, query)
}

// searchViaMemvid searches memories through the memvid service.
func (m *Manager) searchViaMemvid(ctx context.Context, query MemoryQuery) ([]MemoryResult, error) {
	// Determine which zone(s) to search
	var zone string
	if query.Type != "" {
		zone = memvidZone(query.Type, query.Domain)
	}
	// Empty zone = search all zones

	var mvResults []memvid.MemoryResult
	var err error

	if zone == "" {
		mvResults, err = m.memvid.SearchAllZones(ctx, query.Query, query.Limit)
	} else {
		client := m.memvid.WithZone(zone)
		mvResults, err = client.Search(ctx, query.Query, query.Limit)
	}

	if err != nil {
		return nil, err
	}

	// Convert memvid results to memory results
	results := make([]MemoryResult, 0, len(mvResults))
	for _, mr := range mvResults {
		memType := MemoryTypeEpisodic
		source := mr.Memory.Zone

		// Derive memory type from zone
		if len(mr.Memory.Zone) >= 4 && mr.Memory.Zone[:4] == "task" {
			memType = MemoryTypeTask
		} else if mr.Memory.Zone == "personality" {
			memType = MemoryTypePersonality
		}

		// Extract category from metadata if available
		category := ""
		if cat, ok := mr.Memory.Metadata["category"].(string); ok {
			category = cat
		}

		// Extract attribution from metadata
		agentID := ""
		if aid, ok := mr.Memory.Metadata["agent_id"].(string); ok {
			agentID = aid
		}
		sessionID := ""
		if sid, ok := mr.Memory.Metadata["session_id"].(string); ok {
			sessionID = sid
		}
		taskID := ""
		if tid, ok := mr.Memory.Metadata["task_id"].(string); ok {
			taskID = tid
		}

		results = append(results, MemoryResult{
			Memory: Memory{
				ID:        mr.Memory.ID,
				Content:   mr.Memory.Content,
				Type:      memType,
				Category:  category,
				Metadata:  mr.Memory.Metadata,
				CreatedAt: mr.Memory.CreatedAt,
				AgentID:   agentID,
				SessionID: sessionID,
				TaskID:    taskID,
			},
			RelevanceScore: mr.RelevanceScore,
			Source:         source,
		})
	}

	// Filter by minimum relevance
	if query.MinRelevance > 0 {
		filtered := results[:0]
		for _, r := range results {
			if r.RelevanceScore >= query.MinRelevance {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	if len(results) > query.Limit {
		results = results[:query.Limit]
	}

	return results, nil
}

// searchViaSQLite searches memories through SQLite backends.
func (m *Manager) searchViaSQLite(ctx context.Context, query MemoryQuery) ([]MemoryResult, error) {
	var results []MemoryResult

	searchEpisodic := query.Type == "" || query.Type == MemoryTypeEpisodic
	searchTask := query.Type == "" || query.Type == MemoryTypeTask

	if searchEpisodic && m.episodic != nil {
		episodicResults, err := m.episodic.Search(ctx, query.Query, query.Limit)
		if err != nil {
			m.logger.Warn("Episodic search failed", "error", err)
		} else {
			results = append(results, episodicResults...)
		}
	}

	if searchTask && m.task != nil {
		taskResults, err := m.task.Search(ctx, query.Query, query.Domain, query.Limit)
		if err != nil {
			m.logger.Warn("Task search failed", "error", err)
		} else {
			results = append(results, taskResults...)
		}
	}

	// Sort by relevance descending, then by created_at descending
	sort.Slice(results, func(i, j int) bool {
		if results[i].RelevanceScore != results[j].RelevanceScore {
			return results[i].RelevanceScore > results[j].RelevanceScore
		}
		return results[i].Memory.CreatedAt.After(results[j].Memory.CreatedAt)
	})

	// Filter by minimum relevance
	if query.MinRelevance > 0 {
		filtered := results[:0]
		for _, r := range results {
			if r.RelevanceScore >= query.MinRelevance {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	// Apply limit
	if len(results) > query.Limit {
		results = results[:query.Limit]
	}

	return results, nil
}

// GetRecent retrieves the most recent memories.
func (m *Manager) GetRecent(ctx context.Context, limit int) ([]MemoryResult, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, errors.New("memory manager not initialized")
	}
	useMemvid := m.useMemvid
	m.mu.RUnlock()

	// Memvid doesn't have a native "get recent" -- use a broad search
	if useMemvid && m.memvid != nil {
		results, err := m.searchViaMemvid(ctx, MemoryQuery{
			Query: "*",
			Limit: limit,
		})
		if err == nil && len(results) > 0 {
			return results, nil
		}
		m.logger.Warn("Memvid get-recent failed, trying SQLite fallback", "error", err)
	}

	var results []MemoryResult

	if m.episodic != nil {
		episodicResults, err := m.episodic.GetRecent(ctx, limit)
		if err != nil {
			m.logger.Warn("Failed to get recent episodic memories", "error", err)
		} else {
			results = append(results, episodicResults...)
		}
	}

	if m.task != nil {
		taskResults, err := m.task.GetRecent(ctx, "", limit)
		if err != nil {
			m.logger.Warn("Failed to get recent task memories", "error", err)
		} else {
			results = append(results, taskResults...)
		}
	}

	// Sort by created_at descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Memory.CreatedAt.After(results[j].Memory.CreatedAt)
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// GetRelevantContext retrieves memories relevant to a query with smart ranking.
func (m *Manager) GetRelevantContext(ctx context.Context, query string, maxItems int) ([]MemoryResult, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, errors.New("memory manager not initialized")
	}
	useMemvid := m.useMemvid
	m.mu.RUnlock()

	// Memvid handles cross-zone search natively
	if useMemvid && m.memvid != nil {
		results, err := m.searchViaMemvid(ctx, MemoryQuery{
			Query: query,
			Limit: maxItems,
		})
		if err == nil {
			return results, nil
		}
		m.logger.Warn("Memvid context search failed, trying SQLite fallback", "error", err)
	}

	// SQLite fallback with subsystem-budget allocation
	episodicLimit := maxItems / 2
	if episodicLimit == 0 {
		episodicLimit = maxItems
	}
	taskLimit := maxItems - episodicLimit
	if taskLimit == 0 {
		taskLimit = maxItems
	}

	var results []MemoryResult
	seenIDs := make(map[string]bool)

	// Search episodic memories
	if m.episodic != nil {
		episodicResults, err := m.episodic.Search(ctx, query, episodicLimit)
		if err != nil {
			m.logger.Warn("Episodic search failed", "error", err)
		} else {
			for _, r := range episodicResults {
				if !seenIDs[r.Memory.ID] {
					results = append(results, r)
					seenIDs[r.Memory.ID] = true
				}
			}
		}

		// Also include very recent memories for conversational continuity
		recent, err := m.episodic.GetRecent(ctx, 5)
		if err == nil {
			for _, r := range recent {
				if !seenIDs[r.Memory.ID] {
					results = append(results, r)
					seenIDs[r.Memory.ID] = true
				}
			}
		}
	}

	// Search task memories
	if m.task != nil {
		taskResults, err := m.task.Search(ctx, query, "", taskLimit)
		if err != nil {
			m.logger.Warn("Task search failed", "error", err)
		} else {
			for _, r := range taskResults {
				if !seenIDs[r.Memory.ID] {
					results = append(results, r)
					seenIDs[r.Memory.ID] = true
				}
			}
		}
	}

	// Apply graph ranking if available
	if m.graph != nil && len(results) > 0 {
		results, _ = m.graph.RankResults(ctx, results, 0.3)
	}

	// Sort by relevance descending, recency as tie-breaker
	sort.Slice(results, func(i, j int) bool {
		if results[i].RelevanceScore != results[j].RelevanceScore {
			return results[i].RelevanceScore > results[j].RelevanceScore
		}
		return results[i].Memory.CreatedAt.After(results[j].Memory.CreatedAt)
	})

	if len(results) > maxItems {
		results = results[:maxItems]
	}

	return results, nil
}

// GetByIDs retrieves memories by their IDs (memvid only).
func (m *Manager) GetByIDs(ctx context.Context, ids []string) ([]Memory, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, errors.New("memory manager not initialized")
	}
	useMemvid := m.useMemvid
	m.mu.RUnlock()

	if !useMemvid || m.memvid == nil {
		return nil, errors.New("GetByIDs requires memvid backend")
	}

	mvMemories, err := m.memvid.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	memories := make([]Memory, 0, len(mvMemories))
	for _, mv := range mvMemories {
		memories = append(memories, Memory{
			ID:        mv.ID,
			Content:   mv.Content,
			Metadata:  mv.Metadata,
			CreatedAt: mv.CreatedAt,
		})
	}

	return memories, nil
}

// GetStats returns aggregate statistics across all subsystems.
func (m *Manager) GetStats(ctx context.Context) (*MemoryStats, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, errors.New("memory manager not initialized")
	}
	useMemvid := m.useMemvid
	m.mu.RUnlock()

	stats := &MemoryStats{}

	// Get stats from memvid if active
	if useMemvid && m.memvid != nil {
		health, err := m.memvid.Health(ctx)
		if err == nil {
			stats.TotalCount = health.Memories
			return stats, nil
		}
		m.logger.Warn("Memvid health check failed", "error", err)
	}

	// SQLite stats
	var oldest, newest []time.Time

	if m.episodic != nil {
		count, err := m.episodic.Count(ctx)
		if err == nil {
			stats.EpisodicCount = count
		}

		ts, err := m.episodic.GetOldestTimestamp(ctx)
		if err == nil && ts != nil {
			oldest = append(oldest, *ts)
		}

		ts, err = m.episodic.GetNewestTimestamp(ctx)
		if err == nil && ts != nil {
			newest = append(newest, *ts)
		}
	}

	if m.task != nil {
		count, err := m.task.Count(ctx)
		if err == nil {
			stats.TaskCount = count
		}

		ts, err := m.task.GetOldestTimestamp(ctx)
		if err == nil && ts != nil {
			oldest = append(oldest, *ts)
		}

		ts, err = m.task.GetNewestTimestamp(ctx)
		if err == nil && ts != nil {
			newest = append(newest, *ts)
		}
	}

	stats.TotalCount = stats.EpisodicCount + stats.TaskCount

	if len(oldest) > 0 {
		sort.Slice(oldest, func(i, j int) bool {
			return oldest[i].Before(oldest[j])
		})
		stats.Oldest = &oldest[0]
	}

	if len(newest) > 0 {
		sort.Slice(newest, func(i, j int) bool {
			return newest[i].After(newest[j])
		})
		stats.Newest = &newest[0]
	}

	return stats, nil
}

// Consolidate runs memory consolidation (SQLite backend only).
func (m *Manager) Consolidate(ctx context.Context) (*ConsolidationReport, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, errors.New("memory manager not initialized")
	}
	m.mu.RUnlock()

	if m.consolidator == nil {
		return nil, errors.New("consolidator not initialized (not applicable for memvid backend)")
	}

	hours := m.config.ConsolidationIntervalHours
	if hours <= 0 {
		hours = 24
	}

	return m.consolidator.Run(ctx, hours)
}

// StartPeriodicConsolidation starts background consolidation (SQLite backend only).
func (m *Manager) StartPeriodicConsolidation(ctx context.Context) {
	m.mu.RLock()
	if !m.initialized || m.consolidator == nil {
		m.mu.RUnlock()
		return
	}
	m.mu.RUnlock()

	hours := m.config.ConsolidationIntervalHours
	if hours <= 0 {
		hours = 6
	}

	interval := time.Duration(hours) * time.Hour
	m.consolidator.StartPeriodicConsolidation(ctx, interval, hours)
}

// Episodic returns the episodic memory subsystem (SQLite backend only).
func (m *Manager) Episodic() *EpisodicMemory {
	return m.episodic
}

// Task returns the task memory subsystem (SQLite backend only).
func (m *Manager) Task() *TaskMemory {
	return m.task
}

// Personality returns the personality memory subsystem.
func (m *Manager) Personality() *PersonalityMemory {
	return m.personality
}

// Config returns the memory configuration.
func (m *Manager) Config() config.MemoryConfig {
	return m.config
}

// MemvidClient returns the memvid client if active.
func (m *Manager) MemvidClient() *memvid.Client {
	return m.memvid
}

// IsMemvidActive returns true if memvid is the active backend.
func (m *Manager) IsMemvidActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.useMemvid
}

// Backend returns the active backend name.
func (m *Manager) Backend() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.useMemvid {
		return "memvid"
	}
	return "sqlite"
}

// Graph returns the knowledge graph if enabled.
func (m *Manager) Graph() *KnowledgeGraph {
	return m.graph
}

// SearchWithGraph searches memories and applies graph-aware ranking.
// The alpha parameter controls PageRank influence: 0 = pure relevance, 1 = pure PageRank.
func (m *Manager) SearchWithGraph(ctx context.Context, query MemoryQuery, alpha float64) ([]MemoryResult, error) {
	results, err := m.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	// Apply PageRank re-ranking if graph is available
	if m.graph != nil && len(results) > 0 {
		results, err = m.graph.RankResults(ctx, results, alpha)
		if err != nil {
			m.logger.Warn("Graph ranking failed", "error", err)
			// Return unranked results
		}
	}

	return results, nil
}

// GetRelatedMemories returns memories related to the given memory via the knowledge graph.
func (m *Manager) GetRelatedMemories(ctx context.Context, memoryID string, limit int) ([]MemoryResult, error) {
	if m.graph == nil {
		return nil, errors.New("knowledge graph not enabled")
	}

	// Get related IDs from graph
	relatedIDs, err := m.graph.GetRelatedMemoryIDs(ctx, memoryID, limit)
	if err != nil {
		return nil, err
	}

	if len(relatedIDs) == 0 {
		return nil, nil
	}

	// Fetch full memory content
	if m.useMemvid && m.memvid != nil {
		memories, err := m.GetByIDs(ctx, relatedIDs)
		if err != nil {
			return nil, err
		}

		results := make([]MemoryResult, len(memories))
		for i, mem := range memories {
			pr, _ := m.graph.GetPageRank(ctx, mem.ID)
			results[i] = MemoryResult{
				Memory:         mem,
				RelevanceScore: pr,
				Source:         "graph",
			}
		}
		return results, nil
	}

	// MEM-16 FIX: Use direct GetByID lookups instead of FTS Search() on UUIDs.
	// FTS5 tokenizes hyphens, so searching for a UUID like "abc-123" fails
	// because the tokenized index stores "abc" and "123" separately.
	// GetByID queries WHERE id = ? which is O(1) and correct.
	var results []MemoryResult
	for _, id := range relatedIDs {
		// Try episodic first
		if m.episodic != nil {
			epResult, err := m.episodic.GetByID(ctx, id)
			if err == nil && epResult != nil {
				pr, _ := m.graph.GetPageRank(ctx, id)
				epResult.RelevanceScore = pr
				epResult.Source = "graph:episodic"
				results = append(results, *epResult)
				continue
			}
		}

		// Try task memory
		if m.task != nil {
			taskResult, err := m.task.GetByID(ctx, id)
			if err == nil && taskResult != nil {
				pr, _ := m.graph.GetPageRank(ctx, id)
				taskResult.RelevanceScore = pr
				taskResult.Source = "graph:task"
				results = append(results, *taskResult)
			}
		}
	}

	return results, nil
}

// AddMemoryRelation creates a relationship between two memories.
func (m *Manager) AddMemoryRelation(ctx context.Context, sourceID, targetID string, edgeType EdgeType, weight float64) error {
	if m.graph == nil {
		return errors.New("knowledge graph not enabled")
	}

	return m.graph.AddEdge(ctx, MemoryEdge{
		SourceID: sourceID,
		TargetID: targetID,
		EdgeType: edgeType,
		Weight:   weight,
	})
}

// RecordSessionMemories creates temporal edges between memories from a session.
func (m *Manager) RecordSessionMemories(ctx context.Context, sessionID string, memoryIDs []string) error {
	if m.graph == nil {
		return nil // Silently skip if graph not available
	}

	return m.graph.CreateTemporalEdges(ctx, sessionID, memoryIDs)
}

// UpdateGraphMetrics recomputes PageRank and community detection.
func (m *Manager) UpdateGraphMetrics(ctx context.Context) error {
	if m.graph == nil {
		return errors.New("knowledge graph not enabled")
	}

	if err := m.graph.ComputePageRank(ctx); err != nil {
		return fmt.Errorf("PageRank computation failed: %w", err)
	}

	if _, err := m.graph.DetectCommunities(ctx); err != nil {
		return fmt.Errorf("community detection failed: %w", err)
	}

	return nil
}

// GetGraphStats returns statistics about the knowledge graph.
func (m *Manager) GetGraphStats(ctx context.Context) (*GraphStats, error) {
	if m.graph == nil {
		return nil, errors.New("knowledge graph not enabled")
	}

	return m.graph.GetStats(ctx)
}

// StoreOptions holds options for versioned memory storage.
type StoreOptions struct {
	CreateVersion bool
	ParentID      string
}

// StoreVersioned stores a memory with version tracking.
// If CreateVersion is true and mem.ID is set, it creates a new version of the memory.
func (m *Manager) StoreVersioned(ctx context.Context, mem Memory, opts StoreOptions) (string, error) {
	if opts.CreateVersion && mem.ID != "" {
		// Mark old version as non-current
		if err := m.markVersionNonCurrent(ctx, mem.ID); err != nil {
			m.logger.Warn("Failed to mark version non-current", "error", err)
		}

		// Get current version number
			currentVersion := m.getCurrentVersion(ctx, mem.ID)

		// Create new version
		newMem := mem
		newMem.ID = "" // Will get new ID
		if newMem.Metadata == nil {
			newMem.Metadata = make(map[string]any)
		}
		newMem.Metadata["parent_id"] = opts.ParentID
		newMem.Metadata["version"] = currentVersion + 1
		newMem.Metadata["is_current"] = 1

		return m.Store(ctx, newMem)
	}
	return m.Store(ctx, mem)
}

// markVersionNonCurrent marks a memory version as non-current.
func (m *Manager) markVersionNonCurrent(ctx context.Context, id string) error {
	if m.episodic == nil {
		return errors.New("episodic memory not available")
	}

	pool := m.episodic.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		return err
	}
	defer pool.Put(db)

	_, err = db.ExecContext(ctx, "UPDATE episodic_memories SET is_current = 0 WHERE id = ?", id)
	return err
}

// getCurrentVersion returns the current version number for a memory.
// It uses the SQL version and parent_id columns directly.
// MEM-13 FIX: accept context so cancellation/deadline propagates to the DB call.
func (m *Manager) getCurrentVersion(ctx context.Context, id string) int {
	if m.episodic == nil {
		return 0
	}

	pool := m.episodic.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		m.logger.Warn("Failed to get database connection for version check", "error", err)
		return 0
	}
	defer pool.Put(db)

	// Find the parent_id of this memory (if it's a version) using the SQL column
	var parentID sql.NullString
	row := db.QueryRowContext(ctx, `
		SELECT parent_id
		FROM episodic_memories
		WHERE id = ?
		LIMIT 1
	`, id)

	err = row.Scan(&parentID)
	if err != nil {
		// Memory might not exist yet, start at version 0
		return 0
	}

	// Determine the root ID: if this memory has a parent, use that; otherwise use id
	rootID := id
	if parentID.Valid && parentID.String != "" {
		rootID = parentID.String
	}

	// Get max version from the SQL column for all memories in the version chain
	var maxVersion int
	err = db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0)
		FROM episodic_memories
		WHERE id = ? OR parent_id = ?
	`, rootID, rootID).Scan(&maxVersion)

	if err != nil {
		return 0
	}

	return maxVersion
}

// GetVersionHistory retrieves all versions of a memory by ID or parent ID.
// Uses the SQL parent_id column for efficient querying.
func (m *Manager) GetVersionHistory(ctx context.Context, id string) ([]Memory, error) {
	if m.episodic == nil {
		return nil, errors.New("episodic memory not available")
	}

	pool := m.episodic.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Put(db)

	// Find all versions using the SQL parent_id column
	rows, err := db.QueryContext(ctx, `
		SELECT id, content, category, metadata_json, created_at, last_accessed_at
		FROM episodic_memories
		WHERE id = ?
		   OR parent_id = ?
		   OR id = (SELECT parent_id FROM episodic_memories WHERE id = ?)
		ORDER BY created_at ASC
	`, id, id, id)

	if err != nil {
		return nil, fmt.Errorf("failed to query version history: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var mem Memory
		var metaJSON string
		var lastAccessedStr sql.NullString
		
		err := rows.Scan(&mem.ID, &mem.Content, &mem.Category, &metaJSON, &mem.CreatedAt, &lastAccessedStr)
		if err != nil {
			return nil, fmt.Errorf("failed to scan memory: %w", err)
		}

		mem.Metadata = ParseMetadata(metaJSON)
		mem.Type = MemoryTypeEpisodic
		
		if lastAccessedStr.Valid {
			if t, err := time.Parse(time.RFC3339Nano, lastAccessedStr.String); err == nil {
				mem.LastAccessedAt = &t
			}
		}

		memories = append(memories, mem)
	}

	return memories, rows.Err()
}

// GetByID retrieves a memory by its ID.
func (m *Manager) GetByID(ctx context.Context, id string) (*Memory, error) {
	if m.episodic == nil {
		return nil, errors.New("episodic memory not available")
	}

	pool := m.episodic.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Put(db)

	row := db.QueryRowContext(ctx, `
		SELECT id, content, category, metadata_json, created_at, last_accessed_at
		FROM episodic_memories
		WHERE id = ? AND is_current = 1
	`, id)

	var mem Memory
	var metaJSON string
	var lastAccessedStr sql.NullString
	err = row.Scan(&mem.ID, &mem.Content, &mem.Category, &metaJSON, &mem.CreatedAt, &lastAccessedStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	// Handle empty string or NULL for last_accessed_at
	if lastAccessedStr.Valid && lastAccessedStr.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, lastAccessedStr.String); err == nil {
			mem.LastAccessedAt = &t
		}
	}

	mem.Metadata = ParseMetadata(metaJSON)
	mem.Type = MemoryTypeEpisodic
	return &mem, nil
}

// GetExpiredMemories returns memories that haven't been accessed in the specified number of days.
func (m *Manager) GetExpiredMemories(ctx context.Context, days int) ([]Memory, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, errors.New("memory manager not initialized")
	}
	useMemvid := m.useMemvid
	m.mu.RUnlock()

	// Memvid backend doesn't support expiration tracking yet
	if useMemvid && m.memvid != nil {
		return nil, errors.New("expiration tracking not supported for memvid backend")
	}

	// SQLite backend implementation
	if m.episodic == nil {
		return nil, errors.New("episodic memory is disabled")
	}

	cutoffDays := days
	if cutoffDays <= 0 {
		cutoffDays = 90 // Default to 90 days
	}
	cutoffTime := time.Now().AddDate(0, 0, -cutoffDays)

	pool := m.episodic.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Put(db)

	cutoffISO := cutoffTime.UTC().Format(time.RFC3339Nano)
	rows, err := db.QueryContext(ctx, `
		SELECT id, content, category, metadata_json, created_at, last_accessed_at
		FROM episodic_memories
		WHERE COALESCE(NULLIF(last_accessed_at, ''), created_at) < ?
		ORDER BY COALESCE(NULLIF(last_accessed_at, ''), created_at) ASC
	`, cutoffISO)

	if err != nil {
		return nil, fmt.Errorf("failed to query expired memories: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var id, content, category, metaJSON, createdAtStr, lastAccessedStr string
		err := rows.Scan(&id, &content, &category, &metaJSON, &createdAtStr, &lastAccessedStr)
		if err != nil {
			return nil, fmt.Errorf("failed to scan memory: %w", err)
		}

		createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)
		var lastAccessed *time.Time
		if lastAccessedStr != "" {
			if t, err := time.Parse(time.RFC3339Nano, lastAccessedStr); err == nil {
				lastAccessed = &t
			}
			// If parse fails, leave lastAccessed as nil rather than zero time
		}

		memories = append(memories, Memory{
			ID:        id,
			Content:   content,
			Type:      MemoryTypeEpisodic,
			Category:  category,
			Metadata:  ParseMetadata(metaJSON),
			CreatedAt: createdAt,
			UpdatedAt: lastAccessed,
		})
	}

	return memories, nil
}

// Delete removes a memory by ID from the appropriate backend.
func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return errors.New("memory manager not initialized")
	}
	useMemvid := m.useMemvid
	m.mu.RUnlock()

	// Route through memvid when active
	if useMemvid && m.memvid != nil {
		// Memvid doesn't support direct deletion by ID yet
		return errors.New("delete by ID not supported for memvid backend")
	}

	// Try episodic memory first
	if m.episodic != nil {
		err := m.episodic.Delete(ctx, id)
		if err == nil {
			return nil
		}
	}

	// Try task memory
	if m.task != nil {
		err := m.task.Delete(ctx, id)
		if err == nil {
			return nil
		}
	}

	return fmt.Errorf("memory with ID %s not found", id)
}

// StartPrefetchService starts the background prefetch service.
func (m *Manager) StartPrefetchService(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.prefetchQueue != nil {
		return // Already started
	}

	m.prefetchQueue = make(chan prefetchRequest, 10)
	m.prefetchShutdown = make(chan struct{})

	m.prefetchWg.Add(1)
	go func() {
		defer m.prefetchWg.Done()
		for {
			select {
			case req := <-m.prefetchQueue:
				m.prefetchWg.Add(1)
				go func() {
					defer m.prefetchWg.Done()
					m.doPrefetch(ctx, req)
				}()
			case <-m.prefetchShutdown:
				return
			}
		}
	}()

	m.logger.Info("Prefetch service started")
}

// doPrefetch performs the actual prefetch operation in the background.
func (m *Manager) doPrefetch(ctx context.Context, req prefetchRequest) {
	// Perform the search to warm the cache
	results, err := m.GetRelevantContext(ctx, req.query, req.maxItems)
	if err != nil {
		m.logger.Warn("Prefetch failed", "query", req.query, "error", err)
		return
	}

	// Convert results to context string
	var contextBuilder strings.Builder
	for i, result := range results {
		if i > 0 {
			contextBuilder.WriteString("\n\n")
		}
		contextBuilder.WriteString(result.Memory.Content)
	}

	// Store in cache
	cacheKey := m.generatePrefetchCacheKey(req.query, req.maxItems)
	m.prefetchCache.Store(cacheKey, contextBuilder.String())

	m.logger.Debug("Prefetch completed", "query", req.query, "items", len(results))
}

// GetCachedPrefetch retrieves prefetched context from cache.
func (m *Manager) GetCachedPrefetch(query string, maxItems int) (string, bool) {
	cacheKey := m.generatePrefetchCacheKey(query, maxItems)
	if cached, ok := m.prefetchCache.Load(cacheKey); ok {
		return cached.(string), true
	}
	return "", false
}

// QueuePrefetch queues a query for background prefetching.
func (m *Manager) QueuePrefetch(query string, maxItems int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.prefetchQueue == nil {
		return // Service not started
	}

	select {
	case m.prefetchQueue <- prefetchRequest{query: query, maxItems: maxItems}:
		m.logger.Debug("Prefetch queued", "query", query, "maxItems", maxItems)
	default:
		m.logger.Warn("Prefetch queue full, dropping request", "query", query)
	}
}

// StopPrefetchService stops the prefetch service.
func (m *Manager) StopPrefetchService() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.prefetchQueue == nil {
		return // Not running
	}

	close(m.prefetchShutdown)
	m.prefetchWg.Wait()

	close(m.prefetchQueue)
	m.prefetchQueue = nil
	m.prefetchShutdown = nil

	// Clear cache
	m.prefetchCache = sync.Map{}

	m.logger.Info("Prefetch service stopped")
}

// generatePrefetchCacheKey creates a unique cache key for a query and maxItems combination.
func (m *Manager) generatePrefetchCacheKey(query string, maxItems int) string {
	return fmt.Sprintf("%s:%d", query, maxItems)
}

// Close gracefully shuts down all subsystems.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.initialized {
		return nil
	}

	var lastErr error

	if m.consolidator != nil {
		m.consolidator.Stop()
	}

	// MEM-5 FIX: Stop prefetch service before closing other subsystems
	if m.prefetchQueue != nil {
		// Need to unlock mutex temporarily for StopPrefetchService to acquire it
		m.mu.Unlock()
		m.StopPrefetchService()
		m.mu.Lock()
	}

	if m.episodic != nil {
		if err := m.episodic.Close(); err != nil {
			lastErr = err
		}
		m.episodic = nil
	}

	if m.task != nil {
		if err := m.task.Close(); err != nil {
			lastErr = err
		}
		m.task = nil
	}

	if m.personality != nil {
		if err := m.personality.Close(); err != nil {
			lastErr = err
		}
		m.personality = nil
	}

	if m.graph != nil {
		if err := m.graph.Close(); err != nil {
			lastErr = err
		}
		m.graph = nil
	}

	m.memvid = nil
	m.useMemvid = false
	m.distributed = false
	m.initialized = false
	m.logger.Info("MemoryManager closed")
	return lastErr
}

// Compile-time assertion that Manager implements io.Closer.
var _ io.Closer = (*Manager)(nil)

// IsDistributed returns true if distributed memory sync is enabled.
func (m *Manager) IsDistributed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.distributed
}

// IsInitialized returns true if the memory manager was successfully initialized.
// This should be checked before using memory tools to avoid "not initialized" errors.
func (m *Manager) IsInitialized() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initialized
}

// DistributedConfig returns the distributed memory configuration.
func (m *Manager) DistributedConfig() config.DistributedMemoryConfig {
	return m.distributedCfg
}
