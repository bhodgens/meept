package services

import (
	"context"
	"time"

	"github.com/caimlas/meept/internal/calendar"
)

// CalendarService provides calendar operations.
type CalendarService struct {
	client *calendar.Client
}

// NewCalendarService creates a calendar service.
func NewCalendarService(client *calendar.Client) *CalendarService {
	return &CalendarService{client: client}
}

// CalendarEvent represents a calendar event for API responses.
type CalendarEvent struct {
	ID          string         `json:"id"`
	Summary     string         `json:"summary"`
	Description string         `json:"description,omitempty"`
	Location    string         `json:"location,omitempty"`
	Start       time.Time      `json:"start"`
	End         time.Time      `json:"end"`
	AllDay      bool           `json:"all_day"`
	Status      string         `json:"status,omitempty"`
	HTMLLink    string         `json:"html_link,omitempty"`
	Attendees   []AttendeeInfo `json:"attendees,omitempty"`
}

// AttendeeInfo represents event attendee information.
type AttendeeInfo struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
	Response    string `json:"response,omitempty"`
}

// ListEventsRequest contains list parameters.
type ListEventsRequest struct {
	TimeMin    time.Time `json:"time_min,omitempty"`
	TimeMax    time.Time `json:"time_max,omitempty"`
	MaxResults int       `json:"max_results,omitempty"`
}

// ListEventsResponse contains list response.
type ListEventsResponse struct {
	Events []CalendarEvent `json:"events"`
	Count  int             `json:"count"`
}

// CreateEventRequest contains create parameters.
type CreateEventRequest struct {
	Summary     string    `json:"summary"`
	Description string    `json:"description,omitempty"`
	Location    string    `json:"location,omitempty"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Attendees   []string  `json:"attendees,omitempty"`
}

// UpdateEventRequest contains update parameters.
type UpdateEventRequest struct {
	ID          string     `json:"id"`
	Summary     string     `json:"summary,omitempty"`
	Description string     `json:"description,omitempty"`
	Location    string     `json:"location,omitempty"`
	Start       *time.Time `json:"start,omitempty"`
	End         *time.Time `json:"end,omitempty"`
}

// ListEvents returns calendar events within a time range.
func (s *CalendarService) ListEvents(ctx context.Context, req ListEventsRequest) (*ListEventsResponse, error) {
	if s.client == nil {
		return nil, wrapError("calendar", "ListEvents", ErrUnavailable)
	}

	timeMin := req.TimeMin
	if timeMin.IsZero() {
		timeMin = time.Now()
	}

	timeMax := req.TimeMax
	if timeMax.IsZero() {
		timeMax = timeMin.Add(24 * time.Hour)
	}

	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}

	events, err := s.client.ListEvents(ctx, timeMin, timeMax, maxResults)
	if err != nil {
		return nil, wrapError("calendar", "ListEvents", err)
	}

	result := make([]CalendarEvent, 0, len(events))
	for _, e := range events {
		startTime, _ := e.Start.Time()
		endTime, _ := e.End.Time()
		result = append(result, CalendarEvent{
			ID:          e.ID,
			Summary:     e.Summary,
			Description: e.Description,
			Location:    e.Location,
			Start:       startTime,
			End:         endTime,
			AllDay:      e.Start.Date != "",
			Status:      e.Status,
			HTMLLink:    e.HTMLLink,
			Attendees:   convertAttendees(e.Attendees),
		})
	}

	return &ListEventsResponse{
		Events: result,
		Count:  len(result),
	}, nil
}

// GetEvent returns a specific event.
func (s *CalendarService) GetEvent(ctx context.Context, eventID string) (*CalendarEvent, error) {
	if s.client == nil {
		return nil, wrapError("calendar", "GetEvent", ErrUnavailable)
	}
	if eventID == "" {
		return nil, wrapError("calendar", "GetEvent", ErrInvalidInput)
	}

	event, err := s.client.GetEvent(ctx, eventID)
	if err != nil {
		return nil, wrapError("calendar", "GetEvent", err)
	}

	startTime, _ := event.Start.Time()
	endTime, _ := event.End.Time()

	return &CalendarEvent{
		ID:          event.ID,
		Summary:     event.Summary,
		Description: event.Description,
		Location:    event.Location,
		Start:       startTime,
		End:         endTime,
		AllDay:      event.Start.Date != "",
		Status:      event.Status,
		HTMLLink:    event.HTMLLink,
		Attendees:   convertAttendees(event.Attendees),
	}, nil
}

// CreateEvent creates a new calendar event.
func (s *CalendarService) CreateEvent(ctx context.Context, req CreateEventRequest) (*CalendarEvent, error) {
	if s.client == nil {
		return nil, wrapError("calendar", "CreateEvent", ErrUnavailable)
	}
	if req.Summary == "" {
		return nil, wrapError("calendar", "CreateEvent", ErrInvalidInput)
	}
	if req.Start.IsZero() || req.End.IsZero() {
		return nil, wrapError("calendar", "CreateEvent", ErrInvalidInput)
	}

	event := &calendar.Event{
		Summary:     req.Summary,
		Description: req.Description,
		Location:    req.Location,
		Start:       calendar.EventTime{DateTime: req.Start.Format(time.RFC3339)},
		End:         calendar.EventTime{DateTime: req.End.Format(time.RFC3339)},
	}

	created, err := s.client.CreateEvent(ctx, event)
	if err != nil {
		return nil, wrapError("calendar", "CreateEvent", err)
	}

	startTime, _ := created.Start.Time()
	endTime, _ := created.End.Time()

	return &CalendarEvent{
		ID:          created.ID,
		Summary:     created.Summary,
		Description: created.Description,
		Location:    created.Location,
		Start:       startTime,
		End:         endTime,
		AllDay:      created.Start.Date != "",
		Status:      created.Status,
		HTMLLink:    created.HTMLLink,
		Attendees:   convertAttendees(created.Attendees),
	}, nil
}

// UpdateEvent updates an existing event.
func (s *CalendarService) UpdateEvent(ctx context.Context, req UpdateEventRequest) (*CalendarEvent, error) {
	if s.client == nil {
		return nil, wrapError("calendar", "UpdateEvent", ErrUnavailable)
	}
	if req.ID == "" {
		return nil, wrapError("calendar", "UpdateEvent", ErrInvalidInput)
	}

	event := &calendar.Event{}
	if req.Summary != "" {
		event.Summary = req.Summary
	}
	if req.Description != "" {
		event.Description = req.Description
	}
	if req.Location != "" {
		event.Location = req.Location
	}
	if req.Start != nil {
		event.Start = calendar.EventTime{DateTime: req.Start.Format(time.RFC3339)}
	}
	if req.End != nil {
		event.End = calendar.EventTime{DateTime: req.End.Format(time.RFC3339)}
	}

	updated, err := s.client.UpdateEvent(ctx, req.ID, event)
	if err != nil {
		return nil, wrapError("calendar", "UpdateEvent", err)
	}

	startTime, _ := updated.Start.Time()
	endTime, _ := updated.End.Time()

	return &CalendarEvent{
		ID:          updated.ID,
		Summary:     updated.Summary,
		Description: updated.Description,
		Location:    updated.Location,
		Start:       startTime,
		End:         endTime,
		AllDay:      updated.Start.Date != "",
		Status:      updated.Status,
		HTMLLink:    updated.HTMLLink,
		Attendees:   convertAttendees(updated.Attendees),
	}, nil
}

// DeleteEvent removes an event from the calendar.
func (s *CalendarService) DeleteEvent(ctx context.Context, eventID string) error {
	if s.client == nil {
		return wrapError("calendar", "DeleteEvent", ErrUnavailable)
	}
	if eventID == "" {
		return wrapError("calendar", "DeleteEvent", ErrInvalidInput)
	}

	return s.client.DeleteEvent(ctx, eventID)
}

// QuickAdd creates an event from natural language text.
func (s *CalendarService) QuickAdd(ctx context.Context, text string) (*CalendarEvent, error) {
	if s.client == nil {
		return nil, wrapError("calendar", "QuickAdd", ErrUnavailable)
	}
	if text == "" {
		return nil, wrapError("calendar", "QuickAdd", ErrInvalidInput)
	}

	event, err := s.client.QuickAdd(ctx, text)
	if err != nil {
		return nil, wrapError("calendar", "QuickAdd", err)
	}

	startTime, _ := event.Start.Time()
	endTime, _ := event.End.Time()

	return &CalendarEvent{
		ID:          event.ID,
		Summary:     event.Summary,
		Description: event.Description,
		Location:    event.Location,
		Start:       startTime,
		End:         endTime,
		AllDay:      event.Start.Date != "",
		Status:      event.Status,
		HTMLLink:    event.HTMLLink,
		Attendees:   convertAttendees(event.Attendees),
	}, nil
}

// GetToday returns today's events.
func (s *CalendarService) GetToday(ctx context.Context) (*ListEventsResponse, error) {
	if s.client == nil {
		return nil, wrapError("calendar", "GetToday", ErrUnavailable)
	}

	events, err := s.client.GetToday(ctx)
	if err != nil {
		return nil, wrapError("calendar", "GetToday", err)
	}

	result := make([]CalendarEvent, 0, len(events))
	for _, e := range events {
		startTime, _ := e.Start.Time()
		endTime, _ := e.End.Time()
		result = append(result, CalendarEvent{
			ID:          e.ID,
			Summary:     e.Summary,
			Description: e.Description,
			Location:    e.Location,
			Start:       startTime,
			End:         endTime,
			AllDay:      e.Start.Date != "",
			Status:      e.Status,
			HTMLLink:    e.HTMLLink,
			Attendees:   convertAttendees(e.Attendees),
		})
	}

	return &ListEventsResponse{
		Events: result,
		Count:  len(result),
	}, nil
}

// GetUpcoming returns upcoming events within a duration.
func (s *CalendarService) GetUpcoming(ctx context.Context, duration time.Duration, maxResults int) (*ListEventsResponse, error) {
	if s.client == nil {
		return nil, wrapError("calendar", "GetUpcoming", ErrUnavailable)
	}

	events, err := s.client.GetUpcoming(ctx, duration, maxResults)
	if err != nil {
		return nil, wrapError("calendar", "GetUpcoming", err)
	}

	result := make([]CalendarEvent, 0, len(events))
	for _, e := range events {
		startTime, _ := e.Start.Time()
		endTime, _ := e.End.Time()
		result = append(result, CalendarEvent{
			ID:          e.ID,
			Summary:     e.Summary,
			Description: e.Description,
			Location:    e.Location,
			Start:       startTime,
			End:         endTime,
			AllDay:      e.Start.Date != "",
			Status:      e.Status,
			HTMLLink:    e.HTMLLink,
			Attendees:   convertAttendees(e.Attendees),
		})
	}

	return &ListEventsResponse{
		Events: result,
		Count:  len(result),
	}, nil
}

func convertAttendees(attendees []calendar.Attendee) []AttendeeInfo {
	result := make([]AttendeeInfo, 0, len(attendees))
	for _, a := range attendees {
		result = append(result, AttendeeInfo{
			Email:       a.Email,
			DisplayName: a.DisplayName,
			Response:    a.ResponseStatus,
		})
	}
	return result
}
