package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory/memvid"
)

// Manager is the unified facade over episodic, task, and personality memory.
// It provides a single entry-point for the rest of meept to interact with
// the memory system. Supports memvid as primary backend with SQLite fallback.
type Manager struct {
	config    config.MemoryConfig
	memvidCfg config.MemvidConfig
	dataDir   string

	// Memvid client (primary backend when configured)
	memvid    *memvid.Client
	useMemvid bool // true if memvid is active for Store/Search

	// SQLite backends (fallback or when explicitly configured)
	episodic    *EpisodicMemory
	task        *TaskMemory
	personality *PersonalityMemory

	consolidator *Consolidator
	initialized  bool
	mu           sync.RWMutex
	logger       *slog.Logger
}

// ManagerConfig holds configuration for creating a Manager.
type ManagerConfig struct {
	// Config is the memory configuration from meept.toml.
	Config config.MemoryConfig
	// MemvidConfig is the memvid service configuration.
	MemvidConfig config.MemvidConfig
	// Logger for operations.
	Logger *slog.Logger
}

// NewManager creates a new memory manager.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Manager{
		config:    cfg.Config,
		memvidCfg: cfg.MemvidConfig,
		logger:    cfg.Logger,
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

	backend := "sqlite"
	if m.useMemvid {
		backend = "memvid"
	}
	m.initialized = true
	m.logger.Info("MemoryManager fully initialized",
		"backend", backend,
		"data_dir", dataDir,
	)
	return nil
}

// initSQLiteBackends initializes the SQLite-based episodic and task memory.
func (m *Manager) initSQLiteBackends(ctx context.Context) error {
	if m.config.Episodic.Enabled {
		episodicDir := filepath.Join(m.dataDir, "episodic")
		m.episodic = NewEpisodicMemory(EpisodicConfig{
			DataDir: episodicDir,
			Logger:  m.logger.With("subsystem", "episodic"),
		})
		if err := m.episodic.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize episodic memory: %w", err)
		}
		m.logger.Info("Episodic memory subsystem initialized (SQLite)")
	} else {
		m.logger.Info("Episodic memory disabled by configuration")
	}

	if m.config.Task.Enabled {
		taskDir := filepath.Join(m.dataDir, "task")
		domains := m.config.Task.Domains
		if len(domains) == 0 {
			domains = []string{"general", "code", "commands"}
		}
		m.task = NewTaskMemory(TaskMemoryConfig{
			DataDir: taskDir,
			Domains: domains,
			Logger:  m.logger.With("subsystem", "task"),
		})
		if err := m.task.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize task memory: %w", err)
		}
		m.logger.Info("Task memory subsystem initialized (SQLite)", "domains", domains)
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
	useMemvid := m.useMemvid
	m.mu.RUnlock()

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

	m.memvid = nil
	m.useMemvid = false
	m.initialized = false
	m.logger.Info("MemoryManager closed")
	return lastErr
}
