package daemon

// aw2 glue tests (plan 20261110-situational leaf 01-moment/02, NEW file):
// the daemon-side awareness wiring shape. The moment prompt lanes are
// gated in-package by internal/agent/moment_wiring_test.go (00T §3/§4);
// these tests pin what the daemon hands that seam:
//   - disabled [awareness] -> buildAwareness returns (nil, nil) -> no
//     renderer is ever installed anywhere (the D1 inert posture).
//   - enabled + resolvable employee -> a renderer that emits the frozen
//     "# Moment" block from the BotDefinition fields.
//   - enabled + unknown/empty id -> nil renderer (SetMomentRenderer(nil)
//     = pinned inert; zero repeated SQL).
// Helper names are aw2*-prefixed to avoid collisions with sibling
// ledger/daystate test files landing in this package.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/employee"
)

// aw2FakeResolver is the minimal EmployeeDefinitionResolver test double.
type aw2FakeResolver struct {
	employees map[string]*employee.Employee
	err       error
	calls     int
}

func (f *aw2FakeResolver) GetEmployee(ctx context.Context, id string) (*employee.Employee, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if emp, ok := f.employees[id]; ok {
		return emp, nil
	}
	return nil, errors.New("employee not found")
}

func aw2Cfg(enabled bool, schedulerTZ string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Awareness.Enabled = enabled
	cfg.Scheduler.Timezone = schedulerTZ
	return cfg
}

func aw2Employee(id, tz, label string) *employee.Employee {
	e := &employee.Employee{}
	e.ID = id
	e.Name = id
	e.Timezone = tz
	e.LocationLabel = label
	return e
}

// TestAw2DisabledReturnsNil — the D1 gate for the daemon side: disabled
// (the default) and nil-cfg both yield (nil, nil); a nil resolver yields
// (nil, nil) even when enabled.
func TestAw2DisabledReturnsNil(t *testing.T) {
	provider, resolve := buildAwareness(aw2Cfg(false, "America/Denver"), &aw2FakeResolver{}, nil)
	if provider != nil || resolve != nil {
		t.Fatalf("disabled awareness must build nothing: provider=%v resolve=%v", provider != nil, resolve != nil)
	}
	if p, r := buildAwareness(nil, &aw2FakeResolver{}, nil); p != nil || r != nil {
		t.Fatal("nil cfg must build nothing")
	}
	if p, r := buildAwareness(aw2Cfg(true, ""), nil, nil); p != nil || r != nil {
		t.Fatal("nil employee resolver must build nothing (warn path)")
	}
}

// TestAw2EnabledRendererRendersBlock — enabled + resolvable employee: the
// renderer emits the frozen time/location block (weather/daystate lines
// absent until sibling feeds land — omission rules verified here).
func TestAw2EnabledRendererRendersBlock(t *testing.T) {
	res := &aw2FakeResolver{employees: map[string]*employee.Employee{
		"ada": aw2Employee("ada", "America/Denver", "Rapid City shop"),
	}}
	provider, resolve := buildAwareness(aw2Cfg(true, "UTC"), res, testLogger(t))
	if provider == nil || resolve == nil {
		t.Fatal("enabled awareness must build provider + resolver")
	}
	render := resolve("ada")
	if render == nil {
		t.Fatal("resolvable employee must yield a non-nil renderer")
	}
	block := render()
	if !strings.HasPrefix(block, "# Moment\nIt is ") {
		t.Fatalf("block shape wrong: %q", block)
	}
	if !strings.Contains(block, ", at Rapid City shop.") {
		t.Fatalf("LocationLabel missing: %q", block)
	}
	if strings.Contains(block, "Weather:") || strings.Contains(block, "Day state:") {
		t.Fatalf("sibling feeds not yet wired; lines must be omitted: %q", block)
	}
	if strings.HasSuffix(block, "\n") {
		t.Errorf("Render must not emit a trailing newline: %q", block)
	}
}

// TestAw2UnknownAndEmptyIDInert — unknown id -> nil renderer (SetMomentRenderer
// shape: explicit nil = the pinned inert seam); empty id -> nil; the def
// cache makes the stamp path cost bounded, not per-turn.
func TestAw2UnknownAndEmptyIDInert(t *testing.T) {
	res := &aw2FakeResolver{employees: map[string]*employee.Employee{}}
	_, resolve := buildAwareness(aw2Cfg(true, ""), res, testLogger(t))
	if resolve == nil {
		t.Fatal("enabled awareness must hand a resolver")
	}
	if r := resolve("ghost"); r != nil {
		t.Fatal("unknown employee must resolve to a nil renderer (D1 inert)")
	}
	if r := resolve(""); r != nil {
		t.Fatal("empty id must resolve to a nil renderer")
	}
	before := res.calls
	for range 5 {
		if r := resolve("ghost"); r != nil {
			t.Fatal("cached-negative must stay nil")
		}
	}
	if res.calls > before+0 && res.calls-before > 0 && res.calls != before {
		// one lookup then cached-negative: at most one extra call total
		if res.calls-before > 1 {
			t.Fatalf("negative cache broken: %d lookups after first, want 0", res.calls-before-1)
		}
	}
}

// TestAw2TimezoneFallbackChain — def-level tz wins; empty def tz falls
// through to the scheduler-tz default location; a bad scheduler tz logs
// and falls back to UTC without failing construction.
func TestAw2TimezoneFallbackChain(t *testing.T) {
	res := &aw2FakeResolver{employees: map[string]*employee.Employee{
		"no-tz": aw2Employee("no-tz", "", ""),
	}}
	_, resolve := buildAwareness(aw2Cfg(true, "America/Denver"), res, testLogger(t))
	block := resolve("no-tz")()
	// Denver local time must show (not UTC) — compare against a live read
	// within the same minute to stay clock-race tolerant.
	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	want := time.Now().In(loc).Format("15:04 MST")
	if !strings.Contains(block, "It is "+want) {
		t.Fatalf("scheduler-tz fallback broken: block=%q want-time=%q", block, want)
	}
	if strings.Contains(block, ", at ") {
		t.Errorf("empty LocationLabel must omit the place clause: %q", block)
	}

	// bad scheduler tz -> UTC fallback, construction still succeeds.
	_, resolve2 := buildAwareness(aw2Cfg(true, "Not/AZone"), res, testLogger(t))
	if resolve2 == nil {
		t.Fatal("bad scheduler tz must not fail awareness construction")
	}
	b2 := resolve2("no-tz")()
	wantUTC := time.Now().UTC().Format("15:04 MST")
	if !strings.Contains(b2, "It is "+wantUTC) {
		t.Fatalf("UTC fallback broken: block=%q want=%q", b2, wantUTC)
	}
}

// TestAw2ResolverErrorInert — a failing employee store is fail-soft: the
// renderer resolves nil (inert), never panics, and turns keep going.
func TestAw2ResolverErrorInert(t *testing.T) {
	res := &aw2FakeResolver{err: errors.New("db down")}
	_, resolve := buildAwareness(aw2Cfg(true, ""), res, testLogger(t))
	if r := resolve("ada"); r != nil {
		t.Fatal("store error must resolve inert")
	}
}

// TestAw2DefFieldsRoundTripBotDefinition — the resolver consumes the
// frozen BotDefinition awareness fields (the JSON blob is gate-tested in
// 00T §5; this pins the daemon def mapping uses the SAME fields).
func TestAw2DefFieldsRoundTripBotDefinition(t *testing.T) {
	def := bot.BotDefinition{Timezone: "America/Denver", LocationLabel: "shop"}
	e := &employee.Employee{BotDefinition: def}
	res := &aw2FakeResolver{employees: map[string]*employee.Employee{"x": e}}
	_, resolve := buildAwareness(aw2Cfg(true, ""), res, testLogger(t))
	block := resolve("x")()
	if !strings.Contains(block, "It is ") || !strings.Contains(block, ", at shop.") {
		t.Fatalf("BotDefinition fields not consumed: %q", block)
	}
}
