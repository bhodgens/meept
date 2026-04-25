package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/calendar"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// CalendarListTool lists calendar events within a time range.
type CalendarListTool struct {
	client *calendar.Client
}

// NewCalendarListTool creates a new calendar list tool.
func NewCalendarListTool(client *calendar.Client) *CalendarListTool {
	return &CalendarListTool{client: client}
}

func (t *CalendarListTool) Name() string { return "calendar_list" }

func (t *CalendarListTool) Description() string {
	return "List calendar events within a time range. Requires start and end times in RFC3339 format."
}

func (t *CalendarListTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"start": {
				Type:        "string",
				Description: "Start date/time in RFC3339 format (e.g., 2024-01-15T09:00:00Z)",
			},
			"end": {
				Type:        "string",
				Description: "End date/time in RFC3339 format",
			},
			"max_results": {
				Type:        "integer",
				Description: "Maximum number of events to return (default: 10)",
			},
		},
		Required: []string{"start", "end"},
	}
}

func (t *CalendarListTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	startStr, _ := args["start"].(string)
	endStr, _ := args["end"].(string)
	maxResults := 10
	if mr, ok := args["max_results"].(float64); ok && mr > 0 {
		maxResults = int(mr)
	}

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid start time: %v", err)), nil
	}

	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid end time: %v", err)), nil
	}

	events, err := t.client.ListEvents(ctx, start, end, maxResults)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("failed to list events: %v", err)), nil
	}

	return tools.NewSuccessResult(formatCalendarEvents(events)), nil
}

// CalendarCreateTool creates a new calendar event.
type CalendarCreateTool struct {
	client *calendar.Client
}

// NewCalendarCreateTool creates a new calendar create tool.
func NewCalendarCreateTool(client *calendar.Client) *CalendarCreateTool {
	return &CalendarCreateTool{client: client}
}

func (t *CalendarCreateTool) Name() string { return "calendar_create" }

func (t *CalendarCreateTool) Description() string {
	return "Create a new calendar event with a summary, start time, and end time."
}

func (t *CalendarCreateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"summary": {
				Type:        "string",
				Description: "Event title/summary",
			},
			"start": {
				Type:        "string",
				Description: "Start date/time in RFC3339 format",
			},
			"end": {
				Type:        "string",
				Description: "End date/time in RFC3339 format",
			},
			"description": {
				Type:        "string",
				Description: "Event description (optional)",
			},
			"location": {
				Type:        "string",
				Description: "Event location (optional)",
			},
		},
		Required: []string{"summary", "start", "end"},
	}
}

func (t *CalendarCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	summary, _ := args["summary"].(string)
	if summary == "" {
		return tools.NewErrorResult("summary is required"), nil
	}

	event := &calendar.Event{
		Summary:     summary,
		Description: calendarGetString(args, "description"),
		Location:    calendarGetString(args, "location"),
		Start: calendar.EventTime{
			DateTime: args["start"].(string),
		},
		End: calendar.EventTime{
			DateTime: args["end"].(string),
		},
	}

	created, err := t.client.CreateEvent(ctx, event)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("failed to create event: %v", err)), nil
	}

	return tools.NewSuccessResult(fmt.Sprintf("Created event: %s (ID: %s)", created.Summary, created.ID)), nil
}

// CalendarQuickAddTool creates events using natural language.
type CalendarQuickAddTool struct {
	client *calendar.Client
}

// NewCalendarQuickAddTool creates a new calendar quick add tool.
func NewCalendarQuickAddTool(client *calendar.Client) *CalendarQuickAddTool {
	return &CalendarQuickAddTool{client: client}
}

func (t *CalendarQuickAddTool) Name() string { return "calendar_quick_add" }

func (t *CalendarQuickAddTool) Description() string {
	return "Create a calendar event using natural language (e.g., 'Meeting with John tomorrow at 3pm')."
}

func (t *CalendarQuickAddTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"text": {
				Type:        "string",
				Description: "Natural language event description",
			},
		},
		Required: []string{"text"},
	}
}

func (t *CalendarQuickAddTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	text, _ := args["text"].(string)
	if text == "" {
		return tools.NewErrorResult("text is required"), nil
	}

	event, err := t.client.QuickAdd(ctx, text)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("failed to create event: %v", err)), nil
	}

	return tools.NewSuccessResult(fmt.Sprintf("Created event: %s", event.Summary)), nil
}

// CalendarTodayTool gets today's events.
type CalendarTodayTool struct {
	client *calendar.Client
}

// NewCalendarTodayTool creates a new calendar today tool.
func NewCalendarTodayTool(client *calendar.Client) *CalendarTodayTool {
	return &CalendarTodayTool{client: client}
}

func (t *CalendarTodayTool) Name() string { return "calendar_today" }

func (t *CalendarTodayTool) Description() string {
	return "Get all calendar events scheduled for today."
}

func (t *CalendarTodayTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       "object",
		Properties: map[string]llm.ParameterProperty{},
		Required:   []string{},
	}
}

func (t *CalendarTodayTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	events, err := t.client.GetToday(ctx)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("failed to get today's events: %v", err)), nil
	}

	if len(events) == 0 {
		return tools.NewSuccessResult("No events scheduled for today"), nil
	}

	return tools.NewSuccessResult(formatCalendarEvents(events)), nil
}

// formatCalendarEvents formats a slice of events into a readable string.
func formatCalendarEvents(events []calendar.Event) string {
	result := fmt.Sprintf("%d event(s):\n", len(events))
	for i, e := range events {
		start, err := e.Start.Time()
		if err != nil {
			result += fmt.Sprintf("%d. %s\n", i+1, e.Summary)
			continue
		}
		line := fmt.Sprintf("%d. %s - %s", i+1, start.Format("15:04"), e.Summary)
		if e.Location != "" {
			line += fmt.Sprintf(" (%s)", e.Location)
		}
		result += line + "\n"
	}
	return result
}

// calendarGetString extracts an optional string argument.
func calendarGetString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// Ensure tools implement the Tool interface
var (
	_ tools.Tool = (*CalendarListTool)(nil)
	_ tools.Tool = (*CalendarCreateTool)(nil)
	_ tools.Tool = (*CalendarQuickAddTool)(nil)
	_ tools.Tool = (*CalendarTodayTool)(nil)
)
