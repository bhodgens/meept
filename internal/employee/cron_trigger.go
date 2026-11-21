package employee

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/bot"
	"github.com/robfig/cron/v3"
)

// installCronScheduler owns the exported Cron field and the start hook
// bot.Manager calls from startBot. Registration has to happen from the
// sequence the gate uses: bot.NewManager, employee.NewManager, CreateBot,
// bot.Manager.StartAll. The hook is the only seam that sequence provides.
func (m *Manager) installCronScheduler(bm *bot.Manager) {
	if m == nil {
		return
	}
	m.Cron = cron.New(cron.WithLogger(cron.DiscardLogger))
	m.cronRegs = make(map[string]struct{})
	if bm == nil {
		return
	}
	bm.SetStartHook(m.registerEnabledCronTriggers)
}

// registerEnabledCronTriggers registers one scheduler job per enabled cron
// trigger. A second registration of the same bot id and schedule is a
// no-op. The job calls Trigger with source "cron" and the trigger prompt.
// Trigger stamps ResultTopic through resultTopicForTrigger; this path does
// not set a loop flag and does not call a runner Execute.
func (m *Manager) registerEnabledCronTriggers(_ context.Context, def bot.BotDefinition) {
	if m == nil || m.Cron == nil {
		return
	}
	added := false
	for _, trigger := range def.Triggers {
		if trigger.Type != bot.TriggerTypeCron || !trigger.Enabled || trigger.Schedule == "" {
			continue
		}
		if !m.claimCronRegistration(def.ID, trigger.Schedule) {
			continue
		}
		botID := def.ID
		prompt := trigger.PromptTemplate
		if _, err := m.Cron.AddFunc(trigger.Schedule, func() {
			m.fireCronTrigger(botID, prompt)
		}); err != nil {
			m.releaseCronRegistration(botID, trigger.Schedule)
			m.logCron("cron trigger registration failed", botID, err)
			continue
		}
		added = true
	}
	if added {
		// Start after the adds so Entries() sees them without racing
		// the scheduler's add channel. Start is idempotent.
		m.Cron.Start()
	}
}

func (m *Manager) fireCronTrigger(botID, prompt string) {
	if m == nil {
		return
	}
	if _, err := m.Trigger(context.Background(), botID, map[string]any{
		"source": "cron",
		"prompt": prompt,
	}); err != nil {
		m.logCron("cron trigger failed", botID, err)
	}
}

func (m *Manager) claimCronRegistration(botID, schedule string) bool {
	key := cronRegKey(botID, schedule)
	m.cronRegsMu.Lock()
	defer m.cronRegsMu.Unlock()
	if m.cronRegs == nil {
		m.cronRegs = make(map[string]struct{})
	}
	if _, ok := m.cronRegs[key]; ok {
		return false
	}
	m.cronRegs[key] = struct{}{}
	return true
}

func (m *Manager) releaseCronRegistration(botID, schedule string) {
	m.cronRegsMu.Lock()
	delete(m.cronRegs, cronRegKey(botID, schedule))
	m.cronRegsMu.Unlock()
}

func cronRegKey(botID, schedule string) string {
	return botID + "\x00" + schedule
}

func (m *Manager) logCron(msg, botID string, err error) {
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error(msg, "bot_id", botID, "error", err)
}
