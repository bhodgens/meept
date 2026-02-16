// Package calendar provides Google Calendar integration for meept.
package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	googleCalendarAPIBase = "https://www.googleapis.com/calendar/v3"
	defaultTimeout        = 30 * time.Second
)

// Event represents a calendar event.
type Event struct {
	ID          string    `json:"id"`
	Summary     string    `json:"summary"`
	Description string    `json:"description,omitempty"`
	Location    string    `json:"location,omitempty"`
	Start       EventTime `json:"start"`
	End         EventTime `json:"end"`
	Status      string    `json:"status,omitempty"`
	HTMLLink    string    `json:"htmlLink,omitempty"`
	Created     time.Time `json:"created,omitempty"`
	Updated     time.Time `json:"updated,omitempty"`
	Attendees   []Attendee `json:"attendees,omitempty"`
	Reminders   *Reminders `json:"reminders,omitempty"`
}

// EventTime represents an event start/end time.
type EventTime struct {
	DateTime string `json:"dateTime,omitempty"` // RFC3339
	Date     string `json:"date,omitempty"`     // YYYY-MM-DD for all-day
	TimeZone string `json:"timeZone,omitempty"`
}

// Time returns the time as a time.Time.
func (et EventTime) Time() (time.Time, error) {
	if et.DateTime != "" {
		return time.Parse(time.RFC3339, et.DateTime)
	}
	if et.Date != "" {
		return time.Parse("2006-01-02", et.Date)
	}
	return time.Time{}, fmt.Errorf("no time specified")
}

// Attendee represents an event attendee.
type Attendee struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	Optional       bool   `json:"optional,omitempty"`
}

// Reminders represents event reminders.
type Reminders struct {
	UseDefault bool       `json:"useDefault"`
	Overrides  []Reminder `json:"overrides,omitempty"`
}

// Reminder represents a single reminder.
type Reminder struct {
	Method  string `json:"method"` // "email" or "popup"
	Minutes int    `json:"minutes"`
}

// eventsListResponse is the API response for events list.
type eventsListResponse struct {
	Items         []Event `json:"items"`
	NextPageToken string  `json:"nextPageToken,omitempty"`
}

// Client is the Google Calendar API client.
type Client struct {
	httpClient  *http.Client
	accessToken string
	calendarID  string
	logger      *slog.Logger
}

// ClientConfig holds configuration for the calendar client.
type ClientConfig struct {
	AccessToken string // OAuth2 access token
	CalendarID  string // Calendar ID (default: "primary")
}

// NewClient creates a new Google Calendar client.
func NewClient(cfg ClientConfig, logger *slog.Logger) (*Client, error) {
	if cfg.AccessToken == "" {
		return nil, fmt.Errorf("access token is required")
	}
	if cfg.CalendarID == "" {
		cfg.CalendarID = "primary"
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		accessToken: cfg.AccessToken,
		calendarID:  cfg.CalendarID,
		logger:      logger,
	}, nil
}

// ListEvents lists events in the calendar.
func (c *Client) ListEvents(ctx context.Context, timeMin, timeMax time.Time, maxResults int) ([]Event, error) {
	params := url.Values{}
	params.Set("timeMin", timeMin.Format(time.RFC3339))
	params.Set("timeMax", timeMax.Format(time.RFC3339))
	params.Set("singleEvents", "true")
	params.Set("orderBy", "startTime")
	if maxResults > 0 {
		params.Set("maxResults", fmt.Sprintf("%d", maxResults))
	}

	apiURL := fmt.Sprintf("%s/calendars/%s/events?%s",
		googleCalendarAPIBase, url.PathEscape(c.calendarID), params.Encode())

	data, err := c.doRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}

	var resp eventsListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp.Items, nil
}

// GetEvent gets a single event by ID.
func (c *Client) GetEvent(ctx context.Context, eventID string) (*Event, error) {
	apiURL := fmt.Sprintf("%s/calendars/%s/events/%s",
		googleCalendarAPIBase, url.PathEscape(c.calendarID), url.PathEscape(eventID))

	data, err := c.doRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}

	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("failed to parse event: %w", err)
	}

	return &event, nil
}

// CreateEvent creates a new event.
func (c *Client) CreateEvent(ctx context.Context, event *Event) (*Event, error) {
	apiURL := fmt.Sprintf("%s/calendars/%s/events",
		googleCalendarAPIBase, url.PathEscape(c.calendarID))

	body, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event: %w", err)
	}

	data, err := c.doRequest(ctx, http.MethodPost, apiURL, body)
	if err != nil {
		return nil, err
	}

	var created Event
	if err := json.Unmarshal(data, &created); err != nil {
		return nil, fmt.Errorf("failed to parse created event: %w", err)
	}

	return &created, nil
}

// UpdateEvent updates an existing event.
func (c *Client) UpdateEvent(ctx context.Context, eventID string, event *Event) (*Event, error) {
	apiURL := fmt.Sprintf("%s/calendars/%s/events/%s",
		googleCalendarAPIBase, url.PathEscape(c.calendarID), url.PathEscape(eventID))

	body, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event: %w", err)
	}

	data, err := c.doRequest(ctx, http.MethodPut, apiURL, body)
	if err != nil {
		return nil, err
	}

	var updated Event
	if err := json.Unmarshal(data, &updated); err != nil {
		return nil, fmt.Errorf("failed to parse updated event: %w", err)
	}

	return &updated, nil
}

// DeleteEvent deletes an event.
func (c *Client) DeleteEvent(ctx context.Context, eventID string) error {
	apiURL := fmt.Sprintf("%s/calendars/%s/events/%s",
		googleCalendarAPIBase, url.PathEscape(c.calendarID), url.PathEscape(eventID))

	_, err := c.doRequest(ctx, http.MethodDelete, apiURL, nil)
	return err
}

// QuickAdd creates an event using natural language.
func (c *Client) QuickAdd(ctx context.Context, text string) (*Event, error) {
	params := url.Values{}
	params.Set("text", text)

	apiURL := fmt.Sprintf("%s/calendars/%s/events/quickAdd?%s",
		googleCalendarAPIBase, url.PathEscape(c.calendarID), params.Encode())

	data, err := c.doRequest(ctx, http.MethodPost, apiURL, nil)
	if err != nil {
		return nil, err
	}

	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("failed to parse event: %w", err)
	}

	return &event, nil
}

// GetUpcoming returns upcoming events for the next duration.
func (c *Client) GetUpcoming(ctx context.Context, duration time.Duration, maxResults int) ([]Event, error) {
	now := time.Now()
	return c.ListEvents(ctx, now, now.Add(duration), maxResults)
}

// GetToday returns events for today.
func (c *Client) GetToday(ctx context.Context) ([]Event, error) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)
	return c.ListEvents(ctx, startOfDay, endOfDay, 0)
}

// doRequest performs an HTTP request to the Google Calendar API.
func (c *Client) doRequest(ctx context.Context, method, apiURL string, body []byte) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, apiURL, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}

	return data, nil
}

// SetAccessToken updates the access token.
func (c *Client) SetAccessToken(token string) {
	c.accessToken = token
}
