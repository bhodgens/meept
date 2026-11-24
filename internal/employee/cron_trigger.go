package employee

import (
	"context"
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/bot"
	"github.com/robfig/cron/v3"
)

// cronClaim records one live cron registration so it can be removed when
// the bot is deleted or paused. Manager.cronRegs stays a plain dedupe set
// (its field type is fixed in manager.go), so the removable claims live in
// this file-local registry keyed by Manager pointer — one Manager per
// process, confined to this file's helpers.
type cronClaim struct {
	schedule string
	entryID  cron.EntryID
}

var (
	cronClaimsMu sync.Mutex
	cronClaims   = make(map[*Manager]map[string][]cronClaim) // manager -> botID -> claims
)

func recordCronClaim(m *Manager, botID string, schedule string, entryID cron.EntryID) {
	cronClaimsMu.Lock()
	defer cronClaimsMu.Unlock()
	byBot, ok := cronClaims[m]
	if !ok {
		byBot = make(map[string][]cronClaim)
		cronClaims[m] = byBot
	}
	byBot[botID] = append(byBot[botID], cronClaim{schedule: schedule, entryID: entryID})
}

func claimsFor(m *Manager, botID string) []cronClaim {
	cronClaimsMu.Lock()
	defer cronClaimsMu.Unlock()
	return append([]cronClaim(nil), cronClaims[m][botID]...)
}

func dropClaim(m *Manager, botID, schedule string) {
	cronClaimsMu.Lock()
	defer cronClaimsMu.Unlock()
	byBot, ok := cronClaims[m]
	if !ok {
		return
	}
	kept := byBot[botID][:0]
	for _, c := range byBot[botID] {
		if c.schedule != schedule {
			kept = append(kept, c)
		}
	}
	if len(kept) == 0 {
		delete(byBot, botID)
	} else {
		byBot[botID] = kept
	}
}

// installCronScheduler owns the exported Cron field and the lifecycle hooks
// bot.Manager calls from startBot/DeleteBot/PauseBot/ResumeBot/UpdateBot.
// It must run on EVERY constructor that receives a *bot.Manager:
// NewManager (test/gate path) and the production wiring path
// (NewManagerFromConfig -> NewManagerWithStores -> installCronScheduler in
// wiring.go). Without it m.Cron stays nil and every cron trigger is dead.
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
	bm.SetDeleteHook(m.releaseCronClaimsHook)
	bm.SetPauseHook(m.releaseCronClaimsHook)
	bm.SetResumeHook(m.reregisterCronHook)
	bm.SetUpdateHook(m.resyncCronHook)
}

// registerEnabledCronTriggers registers one scheduler job per enabled cron
// trigger. A second registration of the same bot id and schedule is a
// no-op. The job calls Trigger with source "cron" and the trigger prompt.
// Trigger stamps ResultTopic through resultTopicForTrigger; this path does
// not set a loop flag and does not call a runner Execute.
//
// m.Cron.Start() runs UNCONDITIONALLY: robfig's Start is a no-op while the
// scheduler loop is running, and after a StopAll (daemon restart cycle)
// every claim still exists so `added` would stay false — gating Start on
// it left the scheduler permanently dead after the first stop.
func (m *Manager) registerEnabledCronTriggers(_ context.Context, def bot.BotDefinition) {
	if m == nil || m.Cron == nil {
		return
	}
	for _, trigger := range def.Triggers {
		if trigger.Type != bot.TriggerTypeCron || !trigger.Enabled || trigger.Schedule == "" {
			continue
		}
		if !m.claimCronRegistration(def.ID, trigger.Schedule) {
			continue
		}
		botID := def.ID
		schedule := trigger.Schedule
		prompt := trigger.PromptTemplate
		entryID, err := m.Cron.AddFunc(schedule, func() {
			m.fireCronTrigger(botID, schedule, prompt)
		})
		if err != nil {
			m.releaseCronRegistration(botID, schedule)
			m.logCron("cron trigger registration failed", botID, err)
			continue
		}
		recordCronClaim(m, botID, schedule, entryID)
	}
	// Start after the adds so Entries() sees them without racing the
	// scheduler's add channel. Start is idempotent (robfig: "no-op if
	// already started"), so this is safe on every re-registration.
	m.Cron.Start()
}

// fireCronTrigger is the AddFunc closure body. It re-checks at fire time
// that the bot still exists, is enabled, and still carries THIS cron
// trigger enabled — a job registered before a DeleteBot/PauseBot/UpdateBot
// (or one that survived a claim leak) must not call Trigger.
//
// Prompt freshness (bughunt): the prompt is RE-READ from the fetched
// BotDefinition at fire time, not taken from the registration-captured
// prompt. resyncCronHook keys claims on schedule only, so an UpdateBot
// that changed prompt_template while keeping the schedule would otherwise
// keep firing the STALE captured prompt until a daemon restart. The
// registration-time prompt is kept only as a fallback for the (should-not-
// happen) case where the fire-time definition no longer carries this
// schedule — cronTriggerEnabled above already refuses that shape.
func (m *Manager) fireCronTrigger(botID, schedule, prompt string) {
	if m == nil {
		return
	}
	if m.botManager == nil {
		m.logCronSkip("cron trigger skipped: no bot backend", botID)
		return
	}
	ctx := context.Background()
	def, err := m.botManager.GetBot(ctx, botID)
	if err != nil || def == nil {
		m.logCronSkip("cron trigger skipped: bot gone", botID)
		return
	}
	if !def.Enabled || !cronTriggerEnabled(def, schedule) {
		m.logCronSkip("cron trigger skipped: bot or trigger disabled", botID)
		return
	}
	// Read the prompt fresh from the current definition (the closure's
	// captured prompt is only the stale-registration fallback).
	if fresh := cronTriggerPrompt(def, schedule); fresh != "" {
		prompt = fresh
	}
	if _, err := m.Trigger(ctx, botID, map[string]any{
		"source": "cron",
		"prompt": prompt,
	}); err != nil {
		m.logCron("cron trigger failed", botID, err)
	}
}

// cronTriggerPrompt returns the PromptTemplate of def's enabled cron
// trigger with this exact schedule ("" when absent).
func cronTriggerPrompt(def *bot.BotDefinition, schedule string) string {
	if def == nil {
		return ""
	}
	for _, t := range def.Triggers {
		if t.Type == bot.TriggerTypeCron && t.Enabled && t.Schedule == schedule {
			return t.PromptTemplate
		}
	}
	return ""
}

// cronTriggerEnabled reports whether def still has an ENABLED cron trigger
// with this exact schedule.
func cronTriggerEnabled(def *bot.BotDefinition, schedule string) bool {
	if def == nil {
		return false
	}
	for _, t := range def.Triggers {
		if t.Type == bot.TriggerTypeCron && t.Enabled && t.Schedule == schedule {
			return true
		}
	}
	return false
}

// releaseCronClaimsHook adapts ReleaseCronClaims to the bot.Manager
// delete/pause hook signature.
func (m *Manager) releaseCronClaimsHook(_ context.Context, botID string) {
	m.ReleaseCronClaims(botID)
}

// ReleaseCronClaims removes every cron job registered for botID and drops
// its registration claims. Called on bot delete and pause (via the
// bot.Manager hooks) so a removed or disabled employee stops firing; also
// the seam for any future direct caller in the retire path. Nil-safe.
// Returns the number of jobs removed.
func (m *Manager) ReleaseCronClaims(botID string) int {
	if m == nil || m.Cron == nil {
		return 0
	}
	claims := claimsFor(m, botID)
	for _, c := range claims {
		m.Cron.Remove(c.entryID)
		m.releaseCronRegistration(botID, c.schedule)
		dropClaim(m, botID, c.schedule)
	}
	return len(claims)
}

// reregisterCronHook re-registers a resumed bot's cron triggers. Pause
// removed its jobs and claims, so resume must rebuild them.
func (m *Manager) reregisterCronHook(ctx context.Context, botID string) {
	if m == nil || m.Cron == nil || m.botManager == nil {
		return
	}
	def, err := m.botManager.GetBot(ctx, botID)
	if err != nil || def == nil || !def.Enabled {
		return
	}
	m.registerEnabledCronTriggers(ctx, *def)
}

// resyncCronHook reconciles cron registrations after UpdateBot: registers
// newly enabled triggers (dedupe makes existing ones no-ops), then removes
// jobs whose schedule is no longer an enabled cron trigger on the new
// definition (trigger disabled through an edit must stop firing).
func (m *Manager) resyncCronHook(ctx context.Context, def bot.BotDefinition) {
	if m == nil || m.Cron == nil {
		return
	}
	m.registerEnabledCronTriggers(ctx, def)
	enabled := make(map[string]bool)
	for _, t := range def.Triggers {
		if t.Type == bot.TriggerTypeCron && t.Enabled && t.Schedule != "" {
			enabled[t.Schedule] = true
		}
	}
	for _, c := range claimsFor(m, def.ID) {
		if enabled[c.schedule] {
			continue
		}
		m.Cron.Remove(c.entryID)
		m.releaseCronRegistration(def.ID, c.schedule)
		dropClaim(m, def.ID, c.schedule)
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

// logCronSkip reports a skipped fire at DEBUG: a stale job colliding with a
// lifecycle change is expected behavior now, not a failure.
func (m *Manager) logCronSkip(msg, botID string) {
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Debug(msg, "bot_id", botID)
}
