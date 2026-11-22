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

	// startHook runs from startBot after bus-event registration.
	// employee's cron scheduler installs the cron registrar here. Nil is
	// a no-op. Unexported so a gate cannot fire it in place of the
	// scheduler the hook registers.
	startHook func(context.Context, BotDefinition)

	// Lifecycle hooks mirror startHook for cron-claim release and
	// re-registration (employee cron scheduler). All optional; nil is a
	// no-op. They are snapshotted under mu and called WITHOUT the lock —
	// they may do I/O (store reads, cron job removal fan-out).
	//
	// deleteHook/pauseHook run BEFORE the store write so a deleted bot's
	// cron jobs are gone before any later fire could observe the corpse;
	// resumeHook/updateHook run AFTER the store write with the fresh
	// definition so re-registration reads the new state.
	deleteHook func(context.Context, string)
	pauseHook  func(context.Context, string)
	resumeHook func(context.Context, string)
	updateHook func(context.Context, BotDefinition)
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

// UpdateBot updates an existing bot definition. The update hook fires
// AFTER the store write so re-registration reads the fresh definition
// (newly enabled triggers register; disabled/changed ones are removed).
func (m *Manager) UpdateBot(ctx context.Context, def BotDefinition) error {
	if err := def.Validate(); err != nil {
		return fmt.Errorf("validation: %w", err)
	}
	def.UpdatedAt = time.Now().UTC()
	if err := m.store.Update(ctx, def); err != nil {
		return err
	}
	m.mu.RLock()
	updateHook := m.updateHook
	m.mu.RUnlock()
	if updateHook != nil {
		updateHook(ctx, def)
	}
	return nil
}

// DeleteBot removes a bot, stopping it if running. The delete hook fires
// BEFORE the store write so registered cron jobs (and any other per-bot
// state) are released first — a job left behind would fire forever.
func (m *Manager) DeleteBot(ctx context.Context, id string) error {
	m.mu.Lock()
	if rb, ok := m.running[id]; ok {
		rb.cancel()
		delete(m.running, id)
	}
	deleteHook := m.deleteHook
	m.mu.Unlock()

	if deleteHook != nil {
		deleteHook(ctx, id)
	}

	if m.router != nil {
		m.router.Unregister(id)
	}

	return m.store.Delete(ctx, id)
}

// PauseBot disables a bot without removing it. The pause hook fires BEFORE
// the store write so registered cron jobs are released first — a job left
// behind would fire against a disabled bot forever.
func (m *Manager) PauseBot(ctx context.Context, id string) error {
	def, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	m.mu.RLock()
	pauseHook := m.pauseHook
	m.mu.RUnlock()
	if pauseHook != nil {
		pauseHook(ctx, id)
	}
	def.Enabled = false
	return m.store.Update(ctx, *def)
}

// ResumeBot re-enables a paused bot. The resume hook fires AFTER the store
// write so re-registration reads the fresh enabled state.
func (m *Manager) ResumeBot(ctx context.Context, id string) error {
	def, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	def.Enabled = true
	if err := m.store.Update(ctx, *def); err != nil {
		return err
	}
	m.mu.RLock()
	resumeHook := m.resumeHook
	m.mu.RUnlock()
	if resumeHook != nil {
		resumeHook(ctx, id)
	}
	return nil
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
	botCtx, cancel := context.WithCancel(ctx)

	state, err := m.store.GetState(botCtx, def.ID)
	if err != nil {
		state = &BotState{DefinitionID: def.ID, Status: BotStatusStopped}
	}
	state.Status = BotStatusRunning

	rb := &runningBot{
		cancel: cancel,
		state:  state,
	}

	m.mu.Lock()
	m.running[def.ID] = rb
	m.mu.Unlock()

	if m.router != nil {
		m.router.Register(def)
	}

	// Snapshot the hook and call it without m.mu. The hook registers
	// cron jobs and must not run under the lifecycle lock.
	m.mu.RLock()
	hook := m.startHook
	m.mu.RUnlock()
	if hook != nil {
		hook(botCtx, def)
	}

	m.logger.Info("started bot", "bot_id", def.ID, "triggers", len(def.Triggers))
	return nil
}

// SetStartHook installs fn, called from startBot after bus-event
// registration. Nil is ignored. The employee cron scheduler installs the
// registrar so enabled cron triggers reach EmployeeManager.Trigger
// without this package importing employee.
func (m *Manager) SetStartHook(fn func(context.Context, BotDefinition)) {
	if m == nil || fn == nil {
		return
	}
	m.mu.Lock()
	m.startHook = fn
	m.mu.Unlock()
}

// SetDeleteHook installs fn, called from DeleteBot BEFORE the store write
// (and before router unregister). Nil is ignored. The employee cron
// scheduler releases the bot's cron claims here so a deleted bot's jobs
// never fire again.
func (m *Manager) SetDeleteHook(fn func(context.Context, string)) {
	if m == nil || fn == nil {
		return
	}
	m.mu.Lock()
	m.deleteHook = fn
	m.mu.Unlock()
}

// SetPauseHook installs fn, called from PauseBot BEFORE the store write.
// Nil is ignored. The employee cron scheduler releases the bot's cron
// claims here so a paused bot's jobs never fire against the disabled bot.
func (m *Manager) SetPauseHook(fn func(context.Context, string)) {
	if m == nil || fn == nil {
		return
	}
	m.mu.Lock()
	m.pauseHook = fn
	m.mu.Unlock()
}

// SetResumeHook installs fn, called from ResumeBot AFTER the store write
// with the bot id. Nil is ignored. The employee cron scheduler
// re-registers the resumed bot's enabled cron triggers here.
func (m *Manager) SetResumeHook(fn func(context.Context, string)) {
	if m == nil || fn == nil {
		return
	}
	m.mu.Lock()
	m.resumeHook = fn
	m.mu.Unlock()
}

// SetUpdateHook installs fn, called from UpdateBot AFTER the store write
// with the fresh definition. Nil is ignored. The employee cron scheduler
// reconciles registrations here (add newly enabled, remove disabled or
// changed schedules).
func (m *Manager) SetUpdateHook(fn func(context.Context, BotDefinition)) {
	if m == nil || fn == nil {
		return
	}
	m.mu.Lock()
	m.updateHook = fn
	m.mu.Unlock()
}
