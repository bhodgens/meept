package voiceframe

import (
	"slices"
	"time"
)

// Cadence tracks the interval between repeated events (audio chunk
// arrivals/writes) so jitter can be MEASURED, not heard. Mark-only cost
// is two time reads plus a slice append bounded by maxSamples; Report
// sorts a copy. Stdlib only (voiceframe pin).
type Cadence struct {
	target     time.Duration
	maxSamples int
	n          int
	bytes      int64
	last       time.Time
	firstAt    time.Time
	sum        time.Duration
	max        time.Duration
	firstDelay time.Duration // gap between event 1 and event 2
	gaps       []time.Duration
	overruns   int // gaps > 2x target
}

// NewCadence starts a tracker whose expected inter-event gap is target.
// maxSamples caps retained gap samples (0 = 8192).
func NewCadence(target time.Duration, maxSamples int) *Cadence {
	if maxSamples <= 0 {
		maxSamples = 8192
	}
	return &Cadence{target: target, maxSamples: maxSamples}
}

// Mark records one event at time now and returns the gap since the
// previous event (0 for the first). Callers warn inline on an overrun.
func (c *Cadence) Mark(now time.Time) time.Duration {
	c.n++
	if c.firstAt.IsZero() {
		c.firstAt = now
		c.last = now
		return 0
	}
	g := now.Sub(c.last)
	c.last = now
	if c.n == 2 {
		c.firstDelay = g
	}
	c.sum += g
	if g > c.max {
		c.max = g
	}
	if c.target > 0 && g > 2*c.target {
		c.overruns++
	}
	if len(c.gaps) < c.maxSamples {
		c.gaps = append(c.gaps, g)
	}
	return g
}

// CadenceReport is the aggregate shape of one measured stream.
type CadenceReport struct {
	Events     int           `json:"events"`
	MeanGap    time.Duration `json:"mean_gap"`
	MedianGap  time.Duration `json:"median_gap"`
	P95Gap     time.Duration `json:"p95_gap"`
	MaxGap     time.Duration `json:"max_gap"`
	Overruns   int           `json:"overruns"` // gaps > 2x target
	SpanMS     int64         `json:"span_ms"`
	TargetMS   int64         `json:"target_ms"`
	BytesTotal int64         `json:"bytes_total,omitempty"`
	FirstDelay time.Duration `json:"first_delay,omitempty"`
}

// Report summarizes the observed event gaps.
func (c *Cadence) Report() CadenceReport {
	r := CadenceReport{Events: c.n, TargetMS: int64(c.target / time.Millisecond)}
	if c.n >= 2 {
		r.MeanGap = c.sum / time.Duration(c.n-1)
		r.MaxGap = c.max
		r.SpanMS = c.last.Sub(c.firstAt).Milliseconds()
		sorted := slices.Clone(c.gaps)
		slices.Sort(sorted)
		r.MedianGap = sorted[len(sorted)/2]
		r.P95Gap = sorted[(len(sorted)*95)/100]
		r.Overruns = c.overruns
	}
	r.BytesTotal = c.bytes
	r.FirstDelay = c.firstDelay
	return r
}

// AddBytes accumulates payload seen at the measured events.
func (c *Cadence) AddBytes(n int64) { c.bytes += n }

// FirstDelay returns the time from the tracker's first event to its
// second (the startup gap); zero when fewer than two events were marked.
func (c *Cadence) FirstDelay() time.Duration { return c.firstDelay }
