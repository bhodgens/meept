package memory

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTraceIndexRowSerialization(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)

	row := TraceIndexRow{
		TraceID:                   "abc123",
		ByteOffsets:               []int64{0, 150, 300},
		ByteLengths:               []int64{150, 150, 120},
		SpanCount:                 3,
		StartTime:                 start,
		EndTime:                   end,
		HasErrors:                 true,
		ServiceNames:              []string{"agent", "llm"},
		ModelNames:                []string{"claude-3.5-sonnet"},
		TokenNames:                []string{"tokens", "cache_reads"},
		TotalInputTokens:          1024,
		TotalOutputTokens:         512,
		AgentNames:                []string{"writer"},
		AgentIDs:                  []string{"agent-1"},
		MissingParentCount:        0,
		MissingAgentIdentityCount: 1,
		OtelErrorSpanCount:        1,
		ToolErrorSpanCount:        2,
	}

	blob, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded TraceIndexRow
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.TraceID != row.TraceID {
		t.Errorf("TraceID mismatch: got %s, want %s", decoded.TraceID, row.TraceID)
	}
	if decoded.SpanCount != row.SpanCount {
		t.Errorf("SpanCount mismatch: got %d, want %d", decoded.SpanCount, row.SpanCount)
	}
	if len(decoded.ByteOffsets) != 3 {
		t.Errorf("ByteOffsets length: got %d, want 3", len(decoded.ByteOffsets))
	}
	if decoded.HasErrors != row.HasErrors {
		t.Errorf("HasErrors mismatch: got %v, want %v", decoded.HasErrors, row.HasErrors)
	}
	if decoded.TotalInputTokens != row.TotalInputTokens {
		t.Errorf("TotalInputTokens mismatch: got %d, want %d", decoded.TotalInputTokens, row.TotalInputTokens)
	}
	if len(decoded.ServiceNames) != 2 {
		t.Errorf("ServiceNames length: got %d, want 2", len(decoded.ServiceNames))
	}
	if decoded.MissingAgentIdentityCount != row.MissingAgentIdentityCount {
		t.Errorf("MissingAgentIdentityCount mismatch: got %d, want %d", decoded.MissingAgentIdentityCount, row.MissingAgentIdentityCount)
	}
}

func TestTraceIndexMetaSerialization(t *testing.T) {
	built := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	meta := TraceIndexMeta{
		SchemaVersion: CurrentSchemaVersion,
		TraceCount:    42,
		SourceSize:    1024000,
		SourceMtimeNs: 1721112000000000000,
		BuiltAt:       built,
	}

	blob, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded TraceIndexMeta
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("SchemaVersion mismatch: got %d, want %d", decoded.SchemaVersion, CurrentSchemaVersion)
	}
	if decoded.TraceCount != meta.TraceCount {
		t.Errorf("TraceCount mismatch: got %d, want %d", decoded.TraceCount, meta.TraceCount)
	}
	if decoded.SourceSize != meta.SourceSize {
		t.Errorf("SourceSize mismatch: got %d, want %d", decoded.SourceSize, meta.SourceSize)
	}
	if decoded.SourceMtimeNs != meta.SourceMtimeNs {
		t.Errorf("SourceMtimeNs mismatch: got %d, want %d", decoded.SourceMtimeNs, meta.SourceMtimeNs)
	}
}

func TestTraceIndexMetaFingerprint(t *testing.T) {
	meta := TraceIndexMeta{
		SchemaVersion: 1,
		SourceSize:    1024,
		SourceMtimeNs: 999000000,
	}

	fp := meta.Fingerprint()
	if fp == "" {
		t.Fatal("fingerprint should not be empty")
	}

	// Same inputs should produce the same fingerprint.
	meta2 := TraceIndexMeta{
		SchemaVersion: 1,
		SourceSize:    1024,
		SourceMtimeNs: 999000000,
	}
	if meta2.Fingerprint() != fp {
		t.Errorf("same inputs produced different fingerprints: %s vs %s", fp, meta2.Fingerprint())
	}

	// Different inputs should produce a different fingerprint.
	meta3 := TraceIndexMeta{
		SchemaVersion: 1,
		SourceSize:    2048,
		SourceMtimeNs: 999000000,
	}
	if meta3.Fingerprint() == fp {
		t.Error("different inputs produced the same fingerprint")
	}
}

func TestTraceIndexRowTruncate(t *testing.T) {
	row := TraceIndexRow{
		ServiceNames: []string{"a", "b", "c", "d", "e"},
		ModelNames:   nil,
		AgentNames:   []string{"x", "y", "z"},
	}

	row.Truncate(3)

	if len(row.ServiceNames) != 3 {
		t.Errorf("ServiceNames: got %d, want 3", len(row.ServiceNames))
	}
	if row.ModelNames != nil {
		t.Errorf("ModelNames should be nil, got %v", row.ModelNames)
	}
	if len(row.AgentNames) != 3 {
		t.Errorf("AgentNames: got %d, want 3", len(row.AgentNames))
	}

	// Should not truncate if already within limit.
	row2 := TraceIndexRow{
		ServiceNames: []string{"a", "b"},
	}
	row2.Truncate(3)
	if len(row2.ServiceNames) != 2 {
		t.Errorf("Should not have truncated: got %d", len(row2.ServiceNames))
	}
}

func TestEncode16(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{16, "10"},
		{255, "ff"},
		{4096, "1000"},
	}

	for _, tt := range tests {
		got := encode16(tt.in)
		if got != tt.want {
			t.Errorf("encode16(%d) = %s, want %s", tt.in, got, tt.want)
		}
	}

	// Negative values should encode as "0".
	if got := encode16(-1); got != "0" {
		t.Errorf("encode16(-1) = %s, want 0", got)
	}
}

func TestSpanRecordIsAgentSpan(t *testing.T) {
	r := SpanRecord{AgentName: "writer"}
	if !r.IsAgentSpan() {
		t.Error("expected IsAgentSpan to be true with AgentName set")
	}

	r2 := SpanRecord{AgentID: "agent-1"}
	if !r2.IsAgentSpan() {
		t.Error("expected IsAgentSpan to be true with AgentID set")
	}

	r3 := SpanRecord{}
	if r3.IsAgentSpan() {
		t.Error("expected IsAgentSpan to be false with no agent fields")
	}
}
