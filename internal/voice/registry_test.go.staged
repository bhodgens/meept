package voice

// Gate tests for contract C8 (master.md, quoted verbatim): the voice
// instance registry caps instances at voice.max_concurrent_calls (ErrBusy
// over cap) and allows one live call per target number globally
// (ErrDuplicateTarget). Release frees both.

import (
	"errors"
	"sync"
	"testing"
)

// TestRegistryLimits pins: cap-N registry accepts N acquires, N+1st returns
// ErrBusy; a second live call to the SAME target returns ErrDuplicateTarget
// even under the cap; Release frees both dimensions (slot + target).
func TestRegistryLimits(t *testing.T) {
	reg := NewRegistry(1)

	inst, err := reg.Acquire("cc-1", "emp-1", "+15551230001")
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if inst.CallControlID != "cc-1" || inst.EmployeeID != "emp-1" || inst.Target != "+15551230001" {
		t.Errorf("Instance = %+v, want (cc-1, emp-1, +15551230001)", inst)
	}

	// Cap 1: the 2nd Acquire is ErrBusy.
	if _, err := reg.Acquire("cc-2", "emp-2", "+15551230002"); !errors.Is(err, ErrBusy) {
		t.Errorf("2nd Acquire over cap 1 = %v, want ErrBusy", err)
	}

	// Duplicate live target, under a bigger cap: ErrDuplicateTarget.
	reg2 := NewRegistry(4)
	if _, err := reg2.Acquire("cc-a", "emp-1", "+15551230009"); err != nil {
		t.Fatalf("seed Acquire: %v", err)
	}
	if _, err := reg2.Acquire("cc-b", "emp-2", "+15551230009"); !errors.Is(err, ErrDuplicateTarget) {
		t.Errorf("same-target Acquire = %v, want ErrDuplicateTarget (one live call per target globally)", err)
	}

	// Live() reports exactly the live instances.
	if got := len(reg2.Live()); got != 1 {
		t.Errorf("Live() len = %d, want 1", got)
	}

	// Release frees both the slot and the target.
	reg2.Release("cc-a")
	if _, err := reg2.Acquire("cc-c", "emp-2", "+15551230009"); err != nil {
		t.Errorf("Acquire after Release = %v, want nil (release frees the target)", err)
	}
	if got := len(reg2.Live()); got != 1 {
		t.Errorf("Live() len after release+reacquire = %d, want 1", got)
	}

	// Unknown Release is a no-op, never a panic.
	reg2.Release("cc-never-acquired")

	// Same-employee second call is refused even with capacity (C8: one
	// employee invoke mutex => one live call per employee).
	if _, err := reg2.Acquire("cc-d", "emp-2", "+15551230010"); !errors.Is(err, ErrBusy) {
		t.Errorf("second live call for one employee = %v, want ErrBusy (one live call per employee)", err)
	}
}

// TestRegistryRace exercises concurrent Acquire/Release under -race: the
// registry is mutex-guarded and the cap can never be exceeded by a race.
func TestRegistryRace(t *testing.T) {
	reg := NewRegistry(2)

	var wg sync.WaitGroup
	granted := make(chan struct{}, 100)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			inst, err := reg.Acquire(ccidFor(n), empFor(n), targetFor(n))
			if err == nil {
				granted <- struct{}{}
				reg.Release(inst.CallControlID)
			}
		}(i)
	}
	wg.Wait()
	close(granted)
	inFlight := 0
	for range granted {
		inFlight++
	}
	if inFlight == 0 {
		t.Fatalf("no concurrent Acquire ever succeeded; registry is over-locked")
	}
}

func ccidFor(n int) string { return "cc-race-" + strconvN(n) }
func empFor(n int) string  { return "emp-" + strconvN(n%3) }
func targetFor(n int) string {
	// 6 distinct targets so duplicate-target refusals and busy caps interleave.
	return "+15551230" + strconvN(n%6) + "00"
}

func strconvN(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
