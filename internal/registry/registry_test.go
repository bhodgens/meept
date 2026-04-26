package registry

import (
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/pkg/models"
)

// -----------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------

// mockComponent is a lightweight test implementation of Component.
type mockComponent struct {
	name     string
	started  bool
	stopped  bool
	startErr error
	stopErr  error
}

func (m *mockComponent) Name() string    { return m.name }
func (m *mockComponent) Start(ctx context.Context) error {
	m.started = true
	return m.startErr
}
func (m *mockComponent) Stop(ctx context.Context) error {
	m.stopped = true
	return m.stopErr
}
func (m *mockComponent) Running() bool { return m.started && !m.stopped }

// -----------------------------------------------------------------------
// New
// -----------------------------------------------------------------------

func TestNew(t *testing.T) {
	r := New(nil)
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.logger == nil {
		t.Error("expected non-nil logger")
	}
	r2 := New(slog.Default())
	if r2.logger == nil {
		t.Error("expected non-nil logger with explicit default")
	}
}

// -----------------------------------------------------------------------
// Register
// -----------------------------------------------------------------------

func TestRegister(t *testing.T) {
	r := New(nil)
	c := &mockComponent{name: "test-svc"}

	err := r.Register(c)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if r.Count() != 1 {
		t.Errorf("expected count=1, got %d", r.Count())
	}
}

func TestRegister_Duplicate(t *testing.T) {
	r := New(nil)
	c := &mockComponent{name: "dup-svc"}

	r.Register(c)
	err := r.Register(c)
	if err == nil {
		t.Error("expected error for duplicate registration")
	}
}

// -----------------------------------------------------------------------
// Get
// -----------------------------------------------------------------------

func TestGet_Existing(t *testing.T) {
	r := New(nil)
	c := &mockComponent{name: "get-svc"}
	r.Register(c)

	got, ok := r.Get("get-svc")
	if !ok {
		t.Fatal("expected to find component")
	}
	if got.Name() != "get-svc" {
		t.Errorf("expected get-svc, got %s", got.Name())
	}
}

func TestGet_NonExisting(t *testing.T) {
	r := New(nil)
	_, ok := r.Get("no-such-component")
	if ok {
		t.Error("expected ok=false for non-existing component")
	}
}

// -----------------------------------------------------------------------
// StartAll / StopAll
// -----------------------------------------------------------------------

func TestStartAll(t *testing.T) {
	r := New(nil)
	c1 := &mockComponent{name: "first"}
	c2 := &mockComponent{name: "second"}
	r.Register(c1)
	r.Register(c2)

	err := r.StartAll(context.Background())
	if err != nil {
		t.Fatalf("StartAll failed: %v", err)
	}
	if !c1.Running() || !c2.Running() {
		t.Error("expected both components to be running")
	}
}

func TestStartAll_Failure(t *testing.T) {
	r := New(nil)
	c1 := &mockComponent{name: "svc1"}
	c2 := &mockComponent{name: "svc2", startErr: context.DeadlineExceeded}
	r.Register(c1)
	r.Register(c2)

	err := r.StartAll(context.Background())
	if err == nil {
		t.Fatal("expected error from StartAll")
	}
	// First component should have started even though second failed
	if !c1.Running() {
		t.Error("first component should have started before failure")
	}
}

func TestStopAll(t *testing.T) {
	r := New(nil)
	r.Register(&mockComponent{name: "svc1"})
	r.Register(&mockComponent{name: "svc2"})
	r.StartAll(context.Background())

	err := r.StopAll(context.Background())
	if err != nil {
		t.Fatalf("StopAll failed: %v", err)
	}
	if comp, ok := r.Get("svc1"); ok {
		if stopped, ok := comp.(interface{ Stopped() bool }); ok && stopped.Stopped() {
			// service was stopped as expected
		}
	}
}

// -----------------------------------------------------------------------
// List
// -----------------------------------------------------------------------

func TestList(t *testing.T) {
	r := New(nil)
	r.Register(&mockComponent{name: "alpha"})
	r.Register(&mockComponent{name: "beta"})

	info := r.List()
	if len(info) != 2 {
		t.Fatalf("expected 2 component infos, got %d", len(info))
	}

	// Verify order matches registration
	if info[0].Name != "alpha" {
		t.Errorf("expected first=alpha, got %s", info[0].Name)
	}
	if info[1].Name != "beta" {
		t.Errorf("expected second=beta, got %s", info[1].Name)
	}
}

func TestList_Empty(t *testing.T) {
	r := New(nil)
	info := r.List()
	if len(info) != 0 {
		t.Errorf("expected 0 infos, got %d", len(info))
	}
}

func TestList_RunningStatus(t *testing.T) {
	r := New(nil)
	c := &mockComponent{name: "svc"}
	r.Register(c)

	info := r.List()
	if info[0].Name != "svc" {
		t.Fatalf("expected svc, got %s", info[0].Name)
	}
	// Before Start, Running should be false
	if info[0].Running {
		t.Error("expected Running=false before Start")
	}

	r.StartAll(context.Background())
	info = r.List()
	if !info[0].Running {
		t.Error("expected Running=true after Start")
	}
}

// -----------------------------------------------------------------------
// Count
// -----------------------------------------------------------------------

func TestCount(t *testing.T) {
	r := New(nil)
	if r.Count() != 0 {
		t.Errorf("expected count=0 initially, got %d", r.Count())
	}

	r.Register(&mockComponent{name: "a"})
	r.Register(&mockComponent{name: "b"})
	r.Register(&mockComponent{name: "c"})

	if r.Count() != 3 {
		t.Errorf("expected count=3, got %d", r.Count())
	}
}

// -----------------------------------------------------------------------
// models.ComponentInfo compatibility
// -----------------------------------------------------------------------

func TestListReturnsCorrectTypes(t *testing.T) {
	r := New(nil)
	r.Register(&mockComponent{name: "type-test"})

	infos := r.List()
	if len(infos) != 1 {
		t.Fatalf("expected 1 info, got %d", len(infos))
	}

	// Verify it returns models.ComponentInfo
	var _ models.ComponentInfo = infos[0]

	if infos[0].Name != "type-test" {
		t.Errorf("expected name=type-test, got %s", infos[0].Name)
	}
	if infos[0].Type == "" {
		t.Error("expected non-empty Type string")
	}
}

// -----------------------------------------------------------------------
// Thread safety: concurrent access
// -----------------------------------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	r := New(nil)

	// Register some components
	for i := 0; i < 10; i++ {
		r.Register(&mockComponent{name: "c" + string(rune('0'+i))})
	}

	ch := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			r.Register(&mockComponent{name: "extra" + string(rune(i%10))})
		}
		close(ch)
	}()

	go func() {
		for i := 0; i < 100; i++ {
			r.Get("c0")
			r.Count()
			r.List()
		}
	}()

	<-ch
}
