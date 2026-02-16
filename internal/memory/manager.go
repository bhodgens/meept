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
)

// Manager is the unified facade over episodic, task, and personality memory.
// It provides a single entry-point for the rest of meept to interact with
// the memory system.
type Manager struct {
	config      config.MemoryConfig
	dataDir     string
	episodic    *EpisodicMemory
	task        *TaskMemory
	personality *PersonalityMemory
	consolidator *Consolidator
	initialized bool
	mu          sync.RWMutex
	logger      *slog.Logger
}

// ManagerConfig holds configuration for creating a Manager.
type ManagerConfig struct {
	// Config is the memory configuration from meept.toml.
	Config config.MemoryConfig
	// Logger for operations.
	Logger *slog.Logger
}

// NewManager creates a new memory manager.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Manager{
		config: cfg.Config,
		logger: cfg.Logger,
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

	// Initialize episodic memory
	if m.config.Episodic.Enabled {
		episodicDir := filepath.Join(dataDir, "episodic")
		m.episodic = NewEpisodicMemory(EpisodicConfig{
			DataDir: episodicDir,
			Logger:  m.logger.With("subsystem", "episodic"),
		})
		if err := m.episodic.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize episodic memory: %w", err)
		}
		m.logger.Info("Episodic memory subsystem initialized")
	} else {
		m.logger.Info("Episodic memory disabled by configuration")
	}

	// Initialize task memory
	if m.config.Task.Enabled {
		taskDir := filepath.Join(dataDir, "task")
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
		m.logger.Info("Task memory subsystem initialized", "domains", domains)
	} else {
		m.logger.Info("Task memory disabled by configuration")
	}

	// Initialize personality memory
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

	// Initialize consolidator
	m.consolidator = NewConsolidator(ConsolidatorConfig{
		Manager: m,
		Logger:  m.logger.With("subsystem", "consolidator"),
	})

	m.initialized = true
	m.logger.Info("MemoryManager fully initialized", "data_dir", dataDir)
	return nil
}

// Store persists content in the appropriate memory subsystem.
func (m *Manager) Store(ctx context.Context, mem Memory) (string, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return "", errors.New("memory manager not initialized")
	}
	m.mu.RUnlock()

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
	m.mu.RUnlock()

	if query.Limit <= 0 {
		query.Limit = 10
	}

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
	m.mu.RUnlock()

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
	m.mu.RUnlock()

	// Allocate budget across subsystems
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

// GetStats returns aggregate statistics across all subsystems.
func (m *Manager) GetStats(ctx context.Context) (*MemoryStats, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, errors.New("memory manager not initialized")
	}
	m.mu.RUnlock()

	stats := &MemoryStats{}
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

// Consolidate runs memory consolidation.
func (m *Manager) Consolidate(ctx context.Context) (*ConsolidationReport, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, errors.New("memory manager not initialized")
	}
	m.mu.RUnlock()

	if m.consolidator == nil {
		return nil, errors.New("consolidator not initialized")
	}

	hours := m.config.ConsolidationIntervalHours
	if hours <= 0 {
		hours = 24
	}

	return m.consolidator.Run(ctx, hours)
}

// StartPeriodicConsolidation starts background consolidation.
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

// Episodic returns the episodic memory subsystem.
func (m *Manager) Episodic() *EpisodicMemory {
	return m.episodic
}

// Task returns the task memory subsystem.
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

	m.initialized = false
	m.logger.Info("MemoryManager closed")
	return lastErr
}
