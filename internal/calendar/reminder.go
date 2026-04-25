package calendar

import (
	"context"
	"log/slog"
	"time"
)

// ReminderWatcher watches for upcoming events and publishes reminders to the message bus.
type ReminderWatcher struct {
	client         *Client
	publish        func(topic string, data map[string]any)
	logger         *slog.Logger
	interval       time.Duration
	advanceMinutes int
	stopCh         chan struct{}
}

// ReminderWatcherConfig holds configuration for the reminder watcher.
type ReminderWatcherConfig struct {
	Interval       time.Duration // How often to poll for upcoming events
	AdvanceMinutes int           // Trigger reminders this many minutes before an event
}

// DefaultReminderWatcherConfig returns sensible defaults.
func DefaultReminderWatcherConfig() ReminderWatcherConfig {
	return ReminderWatcherConfig{
		Interval:       5 * time.Minute,
		AdvanceMinutes: 10,
	}
}

// NewReminderWatcher creates a new reminder watcher.
//
// publish is a callback used to emit reminder messages. In production it wraps
// bus.MessageBus.Publish; passing nil disables publishing.
func NewReminderWatcher(client *Client, publish func(topic string, data map[string]any), cfg ReminderWatcherConfig, logger *slog.Logger) *ReminderWatcher {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	if cfg.AdvanceMinutes <= 0 {
		cfg.AdvanceMinutes = 10
	}
	return &ReminderWatcher{
		client:         client,
		publish:        publish,
		logger:         logger.With("component", "calendar-reminder"),
		interval:       cfg.Interval,
		advanceMinutes: cfg.AdvanceMinutes,
		stopCh:         make(chan struct{}),
	}
}

// Start begins the reminder polling loop. It blocks until ctx is cancelled or Stop is called.
func (w *ReminderWatcher) Start(ctx context.Context) {
	w.logger.Info("reminder watcher started",
		"interval", w.interval,
		"advance_minutes", w.advanceMinutes,
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Check immediately on start
	w.checkUpcoming(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("reminder watcher stopped", "reason", ctx.Err())
			return
		case <-w.stopCh:
			w.logger.Info("reminder watcher stopped by signal")
			return
		case <-ticker.C:
			w.checkUpcoming(ctx)
		}
	}
}

// Stop signals the watcher to stop.
func (w *ReminderWatcher) Stop() {
	select {
	case <-w.stopCh:
		// Already closed
	default:
		close(w.stopCh)
	}
}

func (w *ReminderWatcher) checkUpcoming(ctx context.Context) {
	// Look ahead slightly further than advanceMinutes so we catch events
	// that are just about to enter the reminder window.
	lookAhead := time.Duration(w.advanceMinutes+5) * time.Minute
	events, err := w.client.GetUpcoming(ctx, lookAhead, 20)
	if err != nil {
		w.logger.Error("failed to check upcoming events", "error", err)
		return
	}

	now := time.Now()
	for _, event := range events {
		start, err := event.Start.Time()
		if err != nil {
			continue
		}

		until := start.Sub(now)
		advance := time.Duration(w.advanceMinutes) * time.Minute

		// Trigger if the event is within the advance window but hasn't started yet
		if until > 0 && until <= advance {
			w.triggerReminder(event, until)
		}
	}
}

func (w *ReminderWatcher) triggerReminder(event Event, until time.Duration) {
	w.logger.Info("calendar reminder",
		"event", event.Summary,
		"event_id", event.ID,
		"starts_in", until.Round(time.Minute),
	)

	if w.publish != nil {
		w.publish("calendar.reminder", map[string]any{
			"event_id":  event.ID,
			"summary":   event.Summary,
			"location":  event.Location,
			"starts_in": until.Round(time.Minute).String(),
			"start":     event.Start.DateTime,
		})
	}
}
