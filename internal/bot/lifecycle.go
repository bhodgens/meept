package bot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// runningBot tracks an active bot's goroutines and state.
type runningBot struct {
	runner *BotRunner
	cancel context.CancelFunc
	state  *BotState
}

// Manager orchestrates bot lifecycle: creation, deletion, start/stop, health.
type Manager struct {
	store  *Store
	router *EventActionRouter

	mu      sync.RWMutex
	running map[string]*runningBot
	logger  *slog.Logger
}

// NewManager creates a new bot lifecycle manager.
func NewManager(store *Store, router *EventActionRouter) *Manager {
	return &Manager{
		store:   store,
		router:  router,
		running: make(map[string]*runningBot),
		logger:  slog.Default(),
	}
}

// CreateBot validates and persists a new bot definition.
func (m *Manager) CreateBot(ctx context.Context, def BotDefinition) error {
	if err := def.Validate(); err != nil {
		return fmt.Errorf("validation: %w", err)
	}
	def.CreatedAt = time.Now().UTC()
	def.UpdatedAt = time.Now().UTC()
	return m.store.Create(ctx, def)
}

// GetBot retrieves a bot definition by ID.
func (m *Manager) GetBot(ctx context.Context, id string) (*BotDefinition, error) {
	return m.store.Get(ctx, id)
}

// ListBots returns all bot definitions.
func (m *Manager) ListBots(ctx context.Context) ([]BotDefinition, error) {
	return m.store.List(ctx)
}

// UpdateBot updates an existing bot definition.
func (m *Manager) UpdateBot(ctx context.Context, def BotDefinition) error {
	if err := def.Validate(); err != nil {
		return fmt.Errorf("validation: %w", err)
	}
	def.UpdatedAt = time.Now().UTC()
	return m.store.Update(ctx, def)
}

// DeleteBot removes a bot, stopping it if running.
func (m *Manager) DeleteBot(ctx context.Context, id string) error {
	m.mu.Lock()
	if rb, ok := m.running[id]; ok {
		rb.cancel()
		delete(m.running, id)
	}
	m.mu.Unlock()

	if m.router != nil {
		m.router.Unregister(id)
	}

	return m.store.Delete(ctx, id)
}

// PauseBot disables a bot without removing it.
func (m *Manager) PauseBot(ctx context.Context, id string) error {
	def, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	def.Enabled = false
	return m.store.Update(ctx, *def)
}

// ResumeBot re-enables a paused bot.
func (m *Manager) ResumeBot(ctx context.Context, id string) error {
	def, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	def.Enabled = true
	return m.store.Update(ctx, *def)
}

// StartAll loads all enabled bots and starts their triggers.
func (m *Manager) StartAll(ctx context.Context) error {
	bots, err := m.store.List(ctx)
	if err != nil {
		return err
	}
	for _, def := range bots {
		if !def.Enabled {
			continue
		}
		if err := m.startBot(ctx, def); err != nil {
			m.logger.Error("failed to start bot", "bot_id", def.ID, "error", err)
		}
	}
	return nil
}

// StopAll gracefully stops all running bots.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, rb := range m.running {
		rb.cancel()
		delete(m.running, id)
		m.logger.Info("stopped bot", "bot_id", id)
	}
}

// GetBotStatus returns the runtime state for a bot.
func (m *Manager) GetBotStatus(ctx context.Context, id string) (*BotState, error) {
	m.mu.RLock()
	if rb, ok := m.running[id]; ok {
		m.mu.RUnlock()
		return rb.state, nil
	}
	m.mu.RUnlock()
	return m.store.GetState(ctx, id)
}

func (m *Manager) startBot(ctx context.Context, def BotDefinition) error {
	runner := NewBotRunner(def)
	botCtx, cancel := context.WithCancel(ctx)

	state, err := m.store.GetState(botCtx, def.ID)
	if err != nil {
		state = &BotState{DefinitionID: def.ID, Status: BotStatusStopped}
	}
	state.Status = BotStatusRunning

	rb := &runningBot{
		runner: runner,
		cancel: cancel,
		state:  state,
	}

	m.mu.Lock()
	m.running[def.ID] = rb
	m.mu.Unlock()

	if m.router != nil {
		m.router.Register(def)
	}

	m.logger.Info("started bot", "bot_id", def.ID, "triggers", len(def.Triggers))
	return nil
}
