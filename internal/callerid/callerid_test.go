package callerid

// 00T §1 gate tests (caller-authz plan, branch 01-identity): the neutral
// ctx seam between voice stamping and enforcement reading. Frozen contract
// (docs/plans/20261110-caller-authz/master.md):
//
//	type Info struct { Number, Name string; Approved, Known bool }
//	func With(ctx context.Context, info Info) context.Context
//	func From(ctx context.Context) (Info, bool)
//
// RED style: undefined symbols (Info / With / From) until leaf 01 lands
// callerid.go. This package imports stdlib only — nothing here may
// expand that surface.
//
// Phone literals are built via gateE164 (fmt.Sprintf) on purpose: agent
// tooling redacts long digit runs on display, which corrupts source
// copied from tool output.

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func gateE164(n int) string { return fmt.Sprintf("+1555%07d", n) }

// TestWithFromRoundtrip pins the core contract: an Info stored via With
// comes back byte-identically via From with ok=true.
func TestWithFromRoundtrip(t *testing.T) {
	want := Info{
		Number:   gateE164(1),
		Name:     "Ada",
		Approved: true,
		Known:    true,
	}
	ctx := With(context.Background(), want)
	got, ok := From(ctx)
	if !ok {
		t.Fatalf("From after With = ok false, want true")
	}
	if got != want {
		t.Errorf("From = %+v, want %+v", got, want)
	}
}

// TestFromAbsence pins the non-caller posture: a ctx that never passed
// through With reports ok=false (the step-1c gate must be inert for
// non-voice turns — "no caller in ctx -> 1c inert").
func TestFromAbsence(t *testing.T) {
	if _, ok := From(context.Background()); ok {
		t.Error("From(context.Background()) = ok true, want false (absence must be distinguishable)")
	}
	// A derived non-caller ctx reads the same absence.
	cancelled, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, ok := From(cancelled); ok {
		t.Error("From(cancelled non-caller ctx) = ok true, want false")
	}
}

// TestZeroInfoStampsReadsBack pins that a zero Info is still a stamp
// (ok=true, zero value) — bindingTurnRunner stamps Instance.Caller
// unconditionally, so enforcement must not confuse "unknown caller"
// (zero, ok) with "no caller" (absent, !ok).
func TestZeroInfoStampsReadsBack(t *testing.T) {
	ctx := With(context.Background(), Info{})
	got, ok := From(ctx)
	if !ok {
		t.Fatal("From after With(Info{}) = ok false, want true (zero Info is a legal stamp)")
	}
	if got != (Info{}) {
		t.Errorf("zero stamp reads back as %+v, want zero Info", got)
	}
}

// TestNestedValuesDoNotBleedAcrossGoroutines proves ctx isolation: two
// goroutines each stamp a DIFFERENT Info onto their own ctx derived from
// one shared parent; each reads back its own, never the other's. (The
// voice tree runs concurrent calls; a package-level map would fail this.)
func TestNestedValuesDoNotBleedAcrossGoroutines(t *testing.T) {
	parent := context.Background()
	infos := []Info{
		{Number: gateE164(2), Name: "one", Approved: true, Known: true},
		{Number: gateE164(3), Name: "two", Approved: false, Known: true},
	}

	var wg sync.WaitGroup
	results := make([]Info, len(infos))
	okFlags := make([]bool, len(infos))
	for i, info := range infos {
		wg.Add(1)
		go func(i int, info Info) {
			defer wg.Done()
			ctx := With(parent, info)
			results[i], okFlags[i] = From(ctx)
		}(i, info)
	}
	wg.Wait()

	for i, want := range infos {
		if !okFlags[i] {
			t.Errorf("goroutine %d: From ok=false, want true", i)
			continue
		}
		if results[i] != want {
			t.Errorf("goroutine %d: From = %+v, want %+v (cross-goroutine bleed)", i, results[i], want)
		}
	}

	// The parent itself was never stamped and reads absence.
	if _, ok := From(parent); ok {
		t.Error("parent ctx leaked a caller stamp after child With calls")
	}
}
