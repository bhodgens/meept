package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"time"

	"sync"
)

// -----------------------------------------------------------------------
// TraceStoreReader interface
// -----------------------------------------------------------------------

// TraceStoreReader abstracts trace store operations for the RLM analyzer.
// This interface allows the analyzer to work with any trace store
// implementation (Phase 1 sidecar index or in-memory mocks).
type TraceStoreReader interface {
	ListTraceIDs() ([]string, error)
	GetSpansForTrace(traceID string) ([]string, error)
	ListSpans(spanIDs []string) ([]spanViewData, error)
}

// spanViewData is the simplified span view used by the RLM analyzer.
type spanViewData struct {
	spanID       string
	spanName     string
	service      string
	model        string
	inputTokens  int
	outputTokens int
	hasError     bool
	rawJSON      []byte
	//lint:ignore U1000 reserved for future time-based trace filtering
	startTime    time.Time
	//lint:ignore U1000 reserved for future time-based trace filtering
	endTime      time.Time
}

// traceSpan is a type alias for backward compatibility with existing code.
type traceSpan = spanViewData

// spanSliceToSpanViewData converts a slice of memory.SpanView to []spanViewData.
// This is used by the TraceStoreAdapter when proxying ListSpans calls.
//lint:ignore U1000 reserved for future TraceStoreAdapter
func spanSliceToSpanViewData(src []memorySpanView) []spanViewData {
	out := make([]spanViewData, len(src))
	for i, s := range src {
		out[i] = spanViewData{
			spanID:       s.SpanID,
			spanName:     s.SpanName,
			service:      s.Service,
			model:        s.Model,
			inputTokens:  s.InputTokens,
			outputTokens: s.OutputTokens,
			hasError:     s.HasError,
		}
	}
	return out
}

// memorySpanView mirrors memory.SpanView to avoid a circular import.
// The fields must match memory.SpanView exactly.
//lint:ignore U1000 reserved for future TraceStoreAdapter
type memorySpanView struct {
	SpanID       string
	SpanName     string
	Service      string
	Model        string
	InputTokens  int
	OutputTokens int
	HasError     bool
}

// -----------------------------------------------------------------------
// InMemoryTraceStore - a minimal TraceStoreReader for the RLM analyzer
// -----------------------------------------------------------------------

// NewInMemoryTraceStore creates a new in-memory trace store.
func NewInMemoryTraceStore() *InMemoryTraceStore {
	return &InMemoryTraceStore{
		traceIDs: make(map[string]struct{}),
		spanIDs:  make(map[string][]string), // traceID -> spanIDs
		spans:    make(map[string]spanViewData),
	}
}

// InMemoryTraceStore is a lightweight TraceStoreReader backed by in-memory maps.
type InMemoryTraceStore struct {
	mu       sync.RWMutex
	traceIDs map[string]struct{}
	spanIDs  map[string][]string // traceID -> spanIDs
	spans    map[string]spanViewData
}

// AddTraceSpan adds a span and records its trace ID.
func (s *InMemoryTraceStore) AddTraceSpan(traceID string, span spanViewData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spans[span.spanID] = span
	s.spanIDs[traceID] = append(s.spanIDs[traceID], span.spanID)
	s.traceIDs[traceID] = struct{}{}
}

// ListTraceIDs returns all trace IDs in the store.
func (s *InMemoryTraceStore) ListTraceIDs() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.traceIDs))
	for id := range s.traceIDs {
		ids = append(ids, id)
	}
	return ids, nil
}

// GetSpansForTrace returns all span IDs for a trace.
func (s *InMemoryTraceStore) GetSpansForTrace(traceID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sids, ok := s.spanIDs[traceID]
	if !ok {
		return nil, nil // returns empty, not error
	}
	return sids, nil
}

// ListSpans returns spans for a set of span IDs.
func (s *InMemoryTraceStore) ListSpans(spanIDs []string) ([]spanViewData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []spanViewData
	for _, sid := range spanIDs {
		if sp, ok := s.spans[sid]; ok {
			results = append(results, sp)
		}
	}
	return results, nil
}

// LoadJSONLTraces reads spans from a JSONL file and adds them to the store.
// Each line is expected to be a JSON object with at minimum a "span_id" field.
// Optional fields: "trace_id" (defaults to "unknown"), "span_name", "service", "model",
// "input_tokens", "output_tokens", "has_error".
func LoadJSONLTraces(store *InMemoryTraceStore, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase buffer for large spans.
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			continue // skip malformed lines
		}

		spanID, _ := raw["span_id"].(string)
		if spanID == "" {
			continue
		}

		traceID, _ := raw["trace_id"].(string)
		if traceID == "" {
			traceID = "unknown"
		}

		spanName, _ := raw["span_name"].(string)
		if spanName == "" {
			if n, ok := raw["name"].(string); ok {
				spanName = n
			}
		}

		service, _ := raw["service"].(string)
		model, _ := raw["model"].(string)

		inputTokens := 0
		if v, ok := raw["input_tokens"].(float64); ok {
			inputTokens = int(v)
		}
		outputTokens := 0
		if v, ok := raw["output_tokens"].(float64); ok {
			outputTokens = int(v)
		}

		hasError := false
		if v, ok := raw["has_error"].(bool); ok {
			hasError = v
		} else if v, ok := raw["status"].(string); ok {
			hasError = v == "error"
		}

		store.AddTraceSpan(traceID, spanViewData{
			spanID:       spanID,
			spanName:     spanName,
			service:      service,
			model:        model,
			inputTokens:  inputTokens,
			outputTokens: outputTokens,
			hasError:     hasError,
		})
	}

	return scanner.Err()
}
