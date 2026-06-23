// Package components provides reusable TUI components.
package components

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// noColor is a sentinel for no color.
var noColor = lipgloss.Color("")

// NotificationLevel represents the severity of a notification.
type NotificationLevel int

const (
	NotifyInfo NotificationLevel = iota
	NotifySuccess
	NotifyWarning
	NotifyError
)

// Notification represents a toast notification.
type Notification struct {
	ID      int64
	Level   NotificationLevel
	Title   string
	Message string
	Action  string // Optional action command (e.g., "view task")
	TTL     time.Duration
	Created time.Time
}

// NotificationExpiredMsg is sent when a notification's TTL expires.
type NotificationExpiredMsg struct {
	ID int64
}

// NotificationManager manages toast notifications for the TUI.
type NotificationManager struct {
	notifications []Notification
	nextID        int64
	mu            sync.Mutex
	maxVisible    int
	defaultTTL    time.Duration
	doNotDisturb bool
}

// NewNotificationManager creates a new notification manager.
func NewNotificationManager() *NotificationManager {
	return &NotificationManager{
		notifications: make([]Notification, 0),
		nextID:        1,
		maxVisible:    5,
		defaultTTL:    4 * time.Second,
	}
}

// SetDoNotDisturb toggles suppression of all notifications. When true, Push and
// PushWithAction become no-ops returning a zero Notification and nil command.
// The setter is safe for concurrent use.
func (nm *NotificationManager) SetDoNotDisturb(enabled bool) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.doNotDisturb = enabled
}

// IsDoNotDisturb reports whether DND mode is currently active.
func (nm *NotificationManager) IsDoNotDisturb() bool {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	return nm.doNotDisturb
}

// Push adds a new notification and returns an expiry command.
func (nm *NotificationManager) Push(level NotificationLevel, title, message string) (Notification, tea.Cmd) {
	return nm.PushWithAction(level, title, message, "")
}

// PushWithAction adds a notification with an optional action.
func (nm *NotificationManager) PushWithAction(level NotificationLevel, title, message, action string) (Notification, tea.Cmd) {
	nm.mu.Lock()
	if nm.doNotDisturb {
		nm.mu.Unlock()
		return Notification{}, nil
	}
	defer nm.mu.Unlock()

	id := nm.nextID
	nm.nextID++

	n := Notification{
		ID:      id,
		Level:   level,
		Title:   title,
		Message: message,
		Action:  action,
		TTL:     nm.defaultTTL,
		Created: time.Now(),
	}

	nm.notifications = append(nm.notifications, n)

	// Trim old notifications if over max
	if len(nm.notifications) > nm.maxVisible {
		nm.notifications = nm.notifications[len(nm.notifications)-nm.maxVisible:]
	}

	// Return expiry command
	return n, nm.scheduleExpiry(n)
}

func (nm *NotificationManager) scheduleExpiry(n Notification) tea.Cmd {
	return tea.Tick(n.TTL, func(t time.Time) tea.Msg {
		return NotificationExpiredMsg{ID: n.ID}
	})
}

// Dismiss removes a notification by ID.
func (nm *NotificationManager) Dismiss(id int64) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	for i, n := range nm.notifications {
		if n.ID == id {
			nm.notifications = append(nm.notifications[:i], nm.notifications[i+1:]...)
			return
		}
	}
}

// DismissAll clears all notifications.
func (nm *NotificationManager) DismissAll() {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.notifications = nm.notifications[:0]
}

// Active returns the current visible notifications.
func (nm *NotificationManager) Active() []Notification {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	result := make([]Notification, len(nm.notifications))
	copy(result, nm.notifications)
	return result
}

// HasActive returns whether there are active notifications.
func (nm *NotificationManager) HasActive() bool {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	return len(nm.notifications) > 0
}

// Update handles notification messages.
func (nm *NotificationManager) Update(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(NotificationExpiredMsg); ok {
		nm.Dismiss(msg.ID)
	}
	return nil
}

// View renders the notification stack as a string.
// The notifications are rendered top-to-bottom, right-aligned.
func (nm *NotificationManager) View(screenWidth int) string {
	nm.mu.Lock()
	active := make([]Notification, len(nm.notifications))
	copy(active, nm.notifications)
	nm.mu.Unlock()

	if len(active) == 0 {
		return ""
	}

	var b strings.Builder
	for i, n := range active {
		if i >= nm.maxVisible {
			break
		}
		b.WriteString(renderNotification(n, screenWidth))
		if i < len(active)-1 && i < nm.maxVisible-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderNotification renders a single notification toast.
func renderNotification(n Notification, screenWidth int) string {
	// Color scheme based on level
	var icon string
	titleColor := noColor
	borderColor := noColor

	switch n.Level {
	case NotifyInfo:
		icon = "i"
		titleColor = lipgloss.Color("#06B6D4") // Cyan
		borderColor = lipgloss.Color("#06B6D4")
	case NotifySuccess:
		icon = "+"
		titleColor = lipgloss.Color("#10B981") // Green
		borderColor = lipgloss.Color("#10B981")
	case NotifyWarning:
		icon = "!"
		titleColor = lipgloss.Color("#F59E0B") // Amber
		borderColor = lipgloss.Color("#F59E0B")
	case NotifyError:
		icon = "x"
		titleColor = lipgloss.Color("#EF4444") // Red
		borderColor = lipgloss.Color("#EF4444")
	}

	// Notification box width: ~40 chars, right-aligned
	boxWidth := 40
	if screenWidth > 0 && boxWidth > screenWidth-4 {
		boxWidth = screenWidth - 4
	}

	// Render content
	titleStyle := lipgloss.NewStyle().
		Foreground(titleColor).
		Bold(true)

	msgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	// Build content lines
	var content strings.Builder
	fmt.Fprintf(&content, "[%s] %s", icon, titleStyle.Render(n.Title))
	content.WriteString("\n")

	// Truncate message to fit
	msg := n.Message
	maxMsgLen := boxWidth - 4
	if len(msg) > maxMsgLen {
		msg = msg[:maxMsgLen-3] + "..."
	}
	content.WriteString(msgStyle.Render(msg))

	if n.Action != "" {
		content.WriteString("\n")
		hintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Italic(true)
		content.WriteString(hintStyle.Render(fmt.Sprintf("[%s]  [Esc] dismiss", n.Action)))
	}

	// Wrap in styled box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(lipgloss.Color("#1F2937")).
		Padding(0, 1).
		Width(boxWidth - 4)

	return boxStyle.Render(content.String())
}
