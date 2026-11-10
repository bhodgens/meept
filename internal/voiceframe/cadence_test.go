package voiceframe

import (
	"testing"
	"time"
)

func TestCadenceReport(t *testing.T) {
	c := NewCadence(20*time.Millisecond, 100)
	if c.Report().Events != 0 {
		t.Fatal("fresh tracker reports events")
	}
	base := time.Now()
	c.Mark(base)
	c.Mark(base.Add(20 * time.Millisecond))
	c.Mark(base.Add(45 * time.Millisecond))  // 25 ms gap
	c.Mark(base.Add(245 * time.Millisecond)) // 200 ms gap: overrun (>2x target)
	c.AddBytes(3 * 640)
	r := c.Report()
	if r.Events != 4 {
		t.Fatalf("events = %d, want 4", r.Events)
	}
	if r.MaxGap != 200*time.Millisecond {
		t.Fatalf("max gap = %v, want 200ms", r.MaxGap)
	}
	if r.Overruns != 1 {
		t.Fatalf("overruns = %d, want 1", r.Overruns)
	}
	if r.FirstDelay != 20*time.Millisecond {
		t.Fatalf("first delay = %v, want 20ms", r.FirstDelay)
	}
	if r.BytesTotal != 1920 {
		t.Fatalf("bytes = %d, want 1920", r.BytesTotal)
	}
	if r.SpanMS != 245 {
		t.Fatalf("span = %d, want 245 ms", r.SpanMS)
	}
}

func TestCadenceSampleCap(t *testing.T) {
	c := NewCadence(time.Millisecond, 4)
	base := time.Now()
	for i := 0; i < 50; i++ {
		c.Mark(base.Add(time.Duration(i) * time.Millisecond))
	}
	if len(c.gaps) != 4 {
		t.Fatalf("gaps kept %d, want cap 4", len(c.gaps))
	}
	if c.Report().Events != 50 {
		t.Fatal("event count must survive the sample cap")
	}
}
