package memory

import "time"

// SpanRecord represents a single span record from trace JSONL.
// Fields correspond to the typical OTEL/halo trace schema.
type SpanRecord struct {
	TraceID   string    `json:"trace_id"`
	SpanID    string    `json:"span_id"`
	ParentID  string    `json:"parent_id,omitempty"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Service   string    `json:"service,omitempty"`
	Model     string    `json:"model,omitempty"`

	// Agent attribution
	AgentName string `json:"agent_name,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`

	// Token accounting
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`

	// Flags
	HasError  bool   `json:"has_error,omitempty"`
	ErrorType string `json:"error_type,omitempty"`

	// Raw payload fields (kept for surgical queries)
	Input      string            `json:"input,omitempty"`
	Output     string            `json:"output,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	ToolName   string            `json:"tool_name,omitempty"`
	ToolError  bool              `json:"tool_error,omitempty"`

	// RawLine preserves the original JSON string for regex search.
	RawLine string `json:"-"`
}

// IsAgentSpan returns true if the span has agent attribution.
func (s *SpanRecord) IsAgentSpan() bool {
	return s.AgentName != "" || s.AgentID != ""
}

// SpanRecordSlice is a sortable slice of SpanRecord.
type SpanRecordSlice []SpanRecord

func (s SpanRecordSlice) Len() int           { return len(s) }
func (s SpanRecordSlice) Less(i, j int) bool { return s[i].StartTime.Before(s[j].StartTime) }
func (s SpanRecordSlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
