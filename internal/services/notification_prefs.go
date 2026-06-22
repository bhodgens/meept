package services

import (
	"time"
)

// NotificationPreferences holds user notification preferences.
type NotificationPreferences struct {
	// PerType enables/disables specific notification types.
	// Keys: "task_completed", "requires_approval", "bot_finished", etc.
	PerType map[string]bool `json:"per_type,omitempty"`

	// PerChannel enables/disables specific delivery channels.
	// Keys: "telegram", "tui", "http", "cli"
	PerChannel map[string]bool `json:"per_channel,omitempty"`

	// DoNotDisturb configures quiet hours.
	DoNotDisturb DNDConfig `json:"do_not_disturb"`

	// RateLimit is the maximum number of notifications per hour.
	// 0 = no limit.
	RateLimit int `json:"rate_limit"`
}

// DNDConfig configures "Do Not Disturb" quiet hours.
type DNDConfig struct {
	// Enabled toggles DND mode.
	Enabled bool `json:"enabled"`

	// StartTime is the DND start time in "HH:MM" format (e.g., "22:00").
	StartTime string `json:"start_time"`

	// EndTime is the DND end time in "HH:MM" format (e.g., "07:00").
	EndTime string `json:"end_time"`
}

// DefaultNotificationPreferences returns preferences with sensible defaults.
func DefaultNotificationPreferences() NotificationPreferences {
	return NotificationPreferences{
		PerType:      make(map[string]bool),
		PerChannel:   make(map[string]bool),
		DoNotDisturb: DNDConfig{Enabled: false},
		RateLimit:    60, // 60 per hour default
	}
}

// IsTypeEnabled checks if a notification type is allowed.
// Returns true if not configured (default allow).
func (p *NotificationPreferences) IsTypeEnabled(notifType string) bool {
	if p.PerType == nil || len(p.PerType) == 0 {
		return true // no filters = all allowed
	}
	enabled, ok := p.PerType[notifType]
	if !ok {
		return true // not specified = allowed
	}
	return enabled
}

// IsChannelEnabled checks if a channel is allowed.
// Returns true if not configured (default allow).
func (p *NotificationPreferences) IsChannelEnabled(channel string) bool {
	if p.PerChannel == nil || len(p.PerChannel) == 0 {
		return true // no filters = all allowed
	}
	enabled, ok := p.PerChannel[channel]
	if !ok {
		return true // not specified = allowed
	}
	return enabled
}

// IsInDND checks if the current time falls within DND hours.
func (p *NotificationPreferences) IsInDND(now time.Time) bool {
	if !p.DoNotDisturb.Enabled {
		return false
	}

	startHour, startMin := parseTime(p.DoNotDisturb.StartTime)
	endHour, endMin := parseTime(p.DoNotDisturb.EndTime)

	currentMin := now.Hour()*60 + now.Minute()
	startMinTotal := startHour*60 + startMin
	endMinTotal := endHour*60 + endMin

	if startMinTotal <= endMinTotal {
		// Same-day range (e.g., 09:00-17:00)
		return currentMin >= startMinTotal && currentMin <= endMinTotal
	}
	// Overnight range (e.g., 22:00-07:00)
	return currentMin >= startMinTotal || currentMin <= endMinTotal
}

// parseTime parses "HH:MM" format into hour and minute.
func parseTime(s string) (hour, min int) {
	if len(s) < 5 {
		return 0, 0
	}
	hour = (int(s[0]) - '0') * 10
	hour += int(s[1]) - '0'
	min = (int(s[3]) - '0') * 10
	min += int(s[4]) - '0'
	return hour, min
}

// NotificationRateLimiter tracks notification rates per session.
type NotificationRateLimiter struct {
	window map[string][]time.Time // session_id -> timestamps
	limit  int
}

// NewNotificationRateLimiter creates a rate limiter.
func NewNotificationRateLimiter(limit int) *NotificationRateLimiter {
	return &NotificationRateLimiter{
		window: make(map[string][]time.Time),
		limit:  limit,
	}
}

// Allow checks if a notification is within rate limits.
func (r *NotificationRateLimiter) Allow(sessionID string) bool {
	if r.limit <= 0 {
		return true // no limit
	}

	now := time.Now()
	oneHourAgo := now.Add(-time.Hour)

	// Prune old entries
	times := r.window[sessionID]
	idx := 0
	for idx < len(times) && times[idx].Before(oneHourAgo) {
		idx++
	}
	if idx > 0 {
		r.window[sessionID] = times[idx:]
		times = r.window[sessionID]
	}

	if len(times) >= r.limit {
		return false
	}

	r.window[sessionID] = append(times, now)
	return true
}
