package memory

import (
	"encoding/json"
	"time"
)

// TraceIndexRow is the sidecar index row for one trace_id.
// Modeled after HALO's TraceIndexRow (trace_index_models.py:6).
// It aggregates span-level data into a per-trace rollup for efficient seeking.
type TraceIndexRow struct {
	TraceID                   string    `json:"trace_id"`
	ByteOffsets               []int64   `json:"byte_offsets"`  // file offset per span
	ByteLengths               []int64   `json:"byte_lengths"`  // line byte length per span
	SpanCount                 int       `json:"span_count"`
	StartTime                 time.Time `json:"start_time"`
	EndTime                   time.Time `json:"end_time"`
	HasErrors                 bool      `json:"has_errors"`
	ServiceNames              []string  `json:"service_names,omitempty"`
	ModelNames                []string  `json:"model_names,omitempty"`
	TokenNames                []string  `json:"token_names,omitempty"`
	TotalInputTokens          int       `json:"total_input_tokens"`
	TotalOutputTokens         int       `json:"total_output_tokens"`
	AgentNames                []string  `json:"agent_names,omitempty"`
	AgentIDs                  []string  `json:"agent_ids,omitempty"`
	MissingParentCount        int       `json:"missing_parent_count"`
	MissingAgentIdentityCount int       `json:"missing_agent_identity_count"`
	OtelErrorSpanCount        int       `json:"otel_error_span_count"`
	ToolErrorSpanCount        int       `json:"tool_error_span_count"`
}

// Truncate truncates all string slice fields to prevent unbounded memory usage.
func (r *TraceIndexRow) Truncate(maxSliceLen int) {
	if maxSliceLen <= 0 {
		maxSliceLen = 100
	}
	truncateStringSlice(&r.ServiceNames, maxSliceLen)
	truncateStringSlice(&r.ModelNames, maxSliceLen)
	truncateStringSlice(&r.TokenNames, maxSliceLen)
	truncateStringSlice(&r.AgentNames, maxSliceLen)
	truncateStringSlice(&r.AgentIDs, maxSliceLen)
}

func truncateStringSlice(s *[]string, max int) {
	if s == nil || len(*s) <= max {
		return
	}
	*s = (*s)[:max]
}

// TraceIndexMeta is the sidecar metadata for staleness detection.
// Modeled after HALO's TraceIndexMeta (trace_index_builder.py:92-124).
type TraceIndexMeta struct {
	SchemaVersion int       `json:"schema_version"` // currently 1
	TraceCount    int       `json:"trace_count"`
	SourceSize    int64     `json:"source_size"`    // byte size of source JSONL
	SourceMtimeNs int64     `json:"source_mtime_ns"` // modification time nanoseconds
	BuiltAt       time.Time `json:"built_at"`
}

// Fingerprint returns a staleness fingerprint: size + mtime.
func (m *TraceIndexMeta) Fingerprint() string {
	return m.sourceFingerprint()
}

func (m *TraceIndexMeta) sourceFingerprint() string {
	return encodeFingerprint(m.SourceSize, m.SourceMtimeNs, m.SchemaVersion)
}

// encodeFingerprint produces a compact staleness fingerprint string.
func encodeFingerprint(size, mtimeNs int64, version int) string {
	// simple hex encoding of key components for fingerprint comparison
	return encode16(size) + "-" + encode16(mtimeNs) + "-" + encode16(int64(version))
}

// encode16 encodes an int64 as a hex string without leading zeros.
func encode16(v int64) string {
	if v == 0 {
		return "0"
	}
	// Use absolute value for encoding, negative values treated as 0 for safety.
	if v < 0 {
		return "0"
	}
	return encodeUint16(uint64(v))
}

// encodeUint16 encodes a uint64 as a compact hex string.
func encodeUint16(v uint64) string {
	if v == 0 {
		return "0"
	}
	hexChars := "0123456789abcdef"
	var buf [20]byte
	idx := len(buf)
	for v > 0 {
		idx--
		buf[idx] = hexChars[v&0xf]
		v >>= 4
	}
	return string(buf[idx:])
}

// MarshalJSON implements json.Marshaler for TraceIndexRow.
func (r TraceIndexRow) MarshalJSON() ([]byte, error) {
	type Alias TraceIndexRow
	return json.Marshal((*Alias)(&r))
}

// UnmarshalJSON implements json.Unmarshaler for TraceIndexRow.
func (r *TraceIndexRow) UnmarshalJSON(data []byte) error {
	type Alias TraceIndexRow
	aux := (*Alias)(r)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	return nil
}

// MarshalJSON implements json.Marshaler for TraceIndexMeta.
func (m TraceIndexMeta) MarshalJSON() ([]byte, error) {
	type Alias TraceIndexMeta
	return json.Marshal((*Alias)(&m))
}

// UnmarshalJSON implements json.Unmarshaler for TraceIndexMeta.
func (m *TraceIndexMeta) UnmarshalJSON(data []byte) error {
	type Alias TraceIndexMeta
	aux := (*Alias)(m)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	return nil
}
