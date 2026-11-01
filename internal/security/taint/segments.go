// segments.go implements per-handoff untrusted-segment tracking on the
// taint tracker (plan: docs/plans/20261031-agent-boundary-hardening/
// 01-inter-agent-taint; research decisions D1/D4 in
// docs/research/agent-boundary-taint-findings.md).
//
// Provenance is attached WHERE DATA ENTERS (D4): leaf 02 records the exact
// untrusted spans of a handoff payload at construction time via
// RecordUntrustedSegment, keyed by source ("collab:<driver>:<role>").
// Enforcement (internal/agent checkMessageTaint) then consults only
// OVERLAPPING segments for the specific outgoing message, instead of
// labeling the whole message unconditionally.
package taint

import (
	"crypto/rand"
	"encoding/hex"
)

const (
	// MaxUntrustedSegmentsPerTracker bounds the per-source segment list:
	// unbounded growth from a long-lived collaboration would otherwise
	// balloon the tracker. When the cap is hit, the OLDEST segment for
	// that source is evicted (FIFO per source).
	MaxUntrustedSegmentsPerTracker = 256

	// MaxUntrustedSegmentBytes bounds a single recorded segment value.
	// Handoff payloads can be large; only the span used for overlap
	// matching is needed, so oversized values are truncated to this cap.
	MaxUntrustedSegmentBytes = 4096
)

// untrustedSegment is one recorded untrusted span of a handoff payload.
type untrustedSegment struct {
	id     string
	value  string
	source string
}

// recordUntrustedSegment is the internal append+evict primitive shared by
// all segment-tracking entry points. Caller must hold t.mu.
func (t *Tracker) recordUntrustedSegment(source, value string) *untrustedSegment {
	if value == "" {
		return nil
	}
	if len(value) > MaxUntrustedSegmentBytes {
		value = value[:MaxUntrustedSegmentBytes]
	}

	seg := &untrustedSegment{
		id:     newSegmentID(),
		value:  value,
		source: source,
	}

	if t.segments == nil {
		t.segments = make(map[string][]*untrustedSegment)
	}

	list := t.segments[source]

	// FIFO eviction at the cap: drop oldest (front) entries first.
	for len(list) >= MaxUntrustedSegmentsPerTracker {
		list = list[1:]
	}

	t.segments[source] = append(list, seg)

	return seg
}

// RecordUntrustedSegment records an untrusted segment of a handoff payload
// under the given source key (convention: "collab:<driver>:<role>").
// Returns a unique segment id, or "" if value is empty (nothing recorded).
func (t *ExtendedTracker) RecordUntrustedSegment(source, value string) string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	seg := t.recordUntrustedSegment(source, value)
	if seg == nil {
		return ""
	}
	return seg.id
}

// UntrustedSegmentsContaining returns copies of the recorded segments whose
// value appears (exact substring) in content. No lock is held while copying
// results back to the caller.
func (t *ExtendedTracker) UntrustedSegmentsContaining(content string) []TaintedValue {
	if t == nil || content == "" {
		return nil
	}

	t.mu.RLock()
	values := t.untrustedSegmentsContainingLocked(content)
	t.mu.RUnlock()

	return values
}

// untrustedSegmentsContainingLocked builds the TaintedValue results for
// recorded segments contained in content. Caller must hold t.mu for reading.
func (t *Tracker) untrustedSegmentsContainingLocked(content string) []TaintedValue {
	var matches []TaintedValue
	for source, segs := range t.segments {
		for _, seg := range segs {
			if seg == nil || seg.value == "" {
				continue
			}
			if contains(content, seg.value) {
				matches = append(matches, TaintedValue{
					Value:  seg.value,
					Taints: []TaintLabel{TaintUntrusted},
					Source: source,
				})
				break // one match per source suffices for policy decisions
			}
		}
	}
	return matches
}

// contains reports whether substr occurs in s. Kept as a tiny local helper
// so this file stays independent of the strings import elsewhere.
func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ClearUntrustedSegments removes all segments recorded under the given
// source key. Returns true if any segments were removed.
func (t *ExtendedTracker) ClearUntrustedSegments(source string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.segments == nil {
		return false
	}
	if _, ok := t.segments[source]; !ok {
		return false
	}
	delete(t.segments, source)
	return true
}

// UntrustedSegmentCount reports the total number of recorded segments
// across all sources.
func (t *ExtendedTracker) UntrustedSegmentCount() int {
	if t == nil {
		return 0
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	n := 0
	for _, segs := range t.segments {
		n += len(segs)
	}
	return n
}

// SegmentLabelsFor returns the tracker-recorded untrusted segments that
// overlap (appear within) content, as TaintedValues labeled TaintUntrusted.
// This is the seam checkMessageTaint (internal/agent) consults for
// per-handoff decisions.
func SegmentLabelsFor(tracker *ExtendedTracker, content string) []TaintedValue {
	if tracker == nil {
		return nil
	}
	return tracker.UntrustedSegmentsContaining(content)
}

// newSegmentID generates a random segment id. On catastrophic entropy
// failure it returns a distinct zero-suffix sentinel (repo predid rule:
// never a predictable math/rand fallback).
func newSegmentID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "seg-0"
	}
	return "seg-" + hex.EncodeToString(buf[:])
}
