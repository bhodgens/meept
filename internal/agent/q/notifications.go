package q

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// NotificationChannel defines how notifications are delivered.
type NotificationChannel string

const (
	NotificationChannelCLI     NotificationChannel = "cli"
	NotificationChannelChat    NotificationChannel = "chat"
	NotificationChannelMenuBar NotificationChannel = "menubar"
)

// NotificationConfig holds notification preferences.
type NotificationConfig struct {
	EnabledChannels []NotificationChannel `toml:"enabled_channels"`
	MenuBarURL      string                `toml:"menubar_url"`
}

// DefaultNotificationConfig returns sensible defaults.
func DefaultNotificationConfig() *NotificationConfig {
	return &NotificationConfig{
		EnabledChannels: []NotificationChannel{NotificationChannelCLI, NotificationChannelChat},
		MenuBarURL:      "http://localhost:8081/api/v1/notifications",
	}
}

// NotificationService delivers Q Agent notifications to various channels.
type NotificationService struct {
	logger *slog.Logger
	config *NotificationConfig
}

// NewNotificationService creates a new notification service.
func NewNotificationService(logger *slog.Logger, config *NotificationConfig) *NotificationService {
	return &NotificationService{
		logger: logger,
		config: config,
	}
}

// Notification represents a user notification.
type Notification struct {
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data,omitempty"`
}

// NotifyAnalysisComplete sends a notification when analysis completes.
func (s *NotificationService) NotifyAnalysisComplete(ctx context.Context, result *AnalysisResult) {
	notification := &Notification{
		Type:      "q_analysis_complete",
		Title:     "Q Agent Analysis Complete",
		Message:   result.Summary,
		Timestamp: time.Now(),
		Data: map[string]any{
			"sessions_analyzed": result.SessionsAnalyzed,
			"patterns_detected": len(result.PatternsDetected),
			"recommendations":   len(result.Recommendations),
		},
	}
	s.send(ctx, notification)
}

// NotifyRecommendationApproved sends a notification when a recommendation is approved.
func (s *NotificationService) NotifyRecommendationApproved(ctx context.Context, rec Recommendation) {
	notification := &Notification{
		Type:      "q_recommendation_approved",
		Title:     "Agent Improvement Recommended",
		Message:   rec.Title,
		Timestamp: time.Now(),
		Data:      rec,
	}
	s.send(ctx, notification)
}

// send delivers a notification to all enabled channels.
func (s *NotificationService) send(ctx context.Context, notification *Notification) {
	for _, channel := range s.config.EnabledChannels {
		switch channel {
		case NotificationChannelCLI:
			s.sendCLI(notification)
		case NotificationChannelChat:
			s.sendChat(ctx, notification)
		case NotificationChannelMenuBar:
			s.sendMenuBar(ctx, notification)
		}
	}
}

// sendCLI logs to console.
func (s *NotificationService) sendCLI(notification *Notification) {
	s.logger.Info("notification",
		"type", notification.Type,
		"title", notification.Title,
		"message", notification.Message,
	)
}

// sendChat sends notification via chat system.
func (s *NotificationService) sendChat(ctx context.Context, notification *Notification) {
	s.logger.Debug("chat notification",
		"title", notification.Title,
		"message", notification.Message,
	)
}

// sendMenuBar sends notification to macOS menu bar app via HTTP.
func (s *NotificationService) sendMenuBar(ctx context.Context, notification *Notification) {
	if s.config.MenuBarURL == "" {
		return
	}

	payload, err := json.Marshal(notification)
	if err != nil {
		s.logger.Warn("failed to marshal notification", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.config.MenuBarURL, strings.NewReader(string(payload)))
	if err != nil {
		s.logger.Debug("failed to create menu bar request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		s.logger.Debug("menu bar notification failed", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.logger.Debug("menu bar notification sent")
	} else {
		s.logger.Debug("menu bar notification rejected", "status", resp.StatusCode)
	}
}
