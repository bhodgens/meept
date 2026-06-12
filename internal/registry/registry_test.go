package registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockComponent is a test implementation of Component.
type mockComponent struct {
	name     string
	running  bool
	startErr error
	stopErr  error
	started  atomic.Int32
	stopped  atomic.Int32
	onStart  func(ctx context.Context) error
	onStop   func(ctx context.Context) error
}

func newMockComponent(name string) *mockComponent {
	return &mockComponent{name: name}
}

func (m *mockComponent) Name() string {
	return m.name
}

func (m *mockComponent) Start(ctx context.Context) error {
	if m.onStart != nil {
		return m.onStart(ctx)
	}
	if m.startErr != nil {
		return m.startErr
	}
	m.started.Add(1)
	m.running = true
	return nil
}

func (m *mockComponent) Stop(ctx context.Context) error {
	if m.onStop != nil {
		return m.onStop(ctx)
	}
	if m.stopErr != nil {
		return m.stopErr
	}
	m.stopped.Add(1)
	m.running = false
	return nil
}

func (m *mockComponent) Running() bool {
	return m.running
}

func (m *mockComponent) withStartErr(err error) *mockComponent {
	m.startErr = err
	return m
}

func (m *mockComponent) withStopErr(err error) *mockComponent {
	m.stopErr = err
	return m
}

func TestNew(t *testing.T) {
	// With nil logger
	r := New(nil)
	if r == nil {
		t.Fatal("New() returned nil")
	}
	if r.components == nil {
		t.Error("components map should be initialized")
	}
	if r.order == nil {
		t.Error("order slice should be initialized")
	}
	if r.logger == nil {
		t.Error("logger should default to slog.Default()")
	}

	// With custom logger
	logger := slog.Default()
	r2 := New(logger)
	if r2.logger != logger {
		t.Error("custom logger should be used")
	}
}

func TestRegister(t *testing.T) {
	r := New(nil)

	// Register a component
	c1 := newMockComponent("component1")
	err := r.Register(c1)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1", r.Count())
	}

	// Register another component
	c2 := newMockComponent("component2")
	err = r.Register(c2)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if r.Count() != 2 {
		t.Errorf("Count() = %d, want 2", r.Count())
	}

	// Try to register duplicate name
	c3 := newMockComponent("component1")
	err = r.Register(c3)
	if err == nil {
		t.Error("expected error when registering duplicate name")
	}
}

func TestGet(t *testing.T) {
	r := New(nil)

	// Get non-existent component
	c, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get() should return false for non-existent component")
	}
	if c != nil {
		t.Error("Get() should return nil for non-existent component")
	}

	// Register and get
	original := newMockComponent("test")
	_ = r.Register(original)

	c, ok = r.Get("test")
	if !ok {
		t.Error("Get() should return true for existing component")
	}
	if c != original {
		t.Error("Get() should return the registered component")
	}
}

func TestStartAll(t *testing.T) {
	r := New(nil)

	c1 := newMockComponent("c1")
	c2 := newMockComponent("c2")
	c3 := newMockComponent("c3")

	_ = r.Register(c1)
	_ = r.Register(c2)
	_ = r.Register(c3)

	ctx := context.Background()
	err := r.StartAll(ctx)
	if err != nil {
		t.Fatalf("StartAll() error = %v", err)
	}

	// Verify all components started
	if c1.started.Load() != 1 {
		t.Error("c1 should have been started")
	}
	if c2.started.Load() != 1 {
		t.Error("c2 should have been started")
	}
	if c3.started.Load() != 1 {
		t.Error("c3 should have been started")
	}

	// Verify running state
	if !c1.Running() || !c2.Running() || !c3.Running() {
		t.Error("all components should be running")
	}
}

// TestStartAll_ContinuesOnError verifies that StartAll continues starting
// remaining components even when one fails (matches StopAll behavior).
func TestStartAll_ContinuesOnError(t *testing.T) {
	r := New(nil)

	c1 := newMockComponent("c1")
	c2 := newMockComponent("c2").withStartErr(errors.New("start failed"))
	c3 := newMockComponent("c3")

	_ = r.Register(c1)
	_ = r.Register(c2)
	_ = r.Register(c3)

	ctx := context.Background()
	err := r.StartAll(ctx)
	if err == nil {
		t.Fatal("StartAll() should return error when component fails to start")
	}

	// c1 should have started, c2 should have been attempted, c3 should also start
	// (new behavior: continue on error like StopAll)
	if c1.started.Load() != 1 {
		t.Error("c1 should have been started")
	}
	if c3.started.Load() != 1 {
		t.Error("c3 should have been started (continue on error)")
	}
}

func TestStopAll(t *testing.T) {
	r := New(nil)

	c1 := newMockComponent("c1")
	c2 := newMockComponent("c2")
	c3 := newMockComponent("c3")

	_ = r.Register(c1)
	_ = r.Register(c2)
	_ = r.Register(c3)

	ctx := context.Background()
	_ = r.StartAll(ctx)

	// Stop all
	err := r.StopAll(ctx)
	if err != nil {
		t.Fatalf("StopAll() error = %v", err)
	}

	// Verify all components stopped
	if c1.stopped.Load() != 1 {
		t.Error("c1 should have been stopped")
	}
	if c2.stopped.Load() != 1 {
		t.Error("c2 should have been stopped")
	}
	if c3.stopped.Load() != 1 {
		t.Error("c3 should have been stopped")
	}

	// Verify not running
	if c1.Running() || c2.Running() || c3.Running() {
		t.Error("all components should be stopped")
	}
}

func TestStopAll_ReverseOrder(t *testing.T) {
	r := New(nil)

	var stopOrder []string
	var mu sync.Mutex

	createTracking := func(name string) *mockComponent {
		c := newMockComponent(name)
		// Use onStop to track order
		c.onStop = func(ctx context.Context) error {
			mu.Lock()
			stopOrder = append(stopOrder, name)
			mu.Unlock()
			c.running = false
			return nil
		}
		return c
	}

	c1 := createTracking("c1")
	c2 := createTracking("c2")
	c3 := createTracking("c3")

	_ = r.Register(c1)
	_ = r.Register(c2)
	_ = r.Register(c3)

	ctx := context.Background()
	_ = r.StartAll(ctx)
	_ = r.StopAll(ctx)

	// Should be stopped in reverse order: c3, c2, c1
	if len(stopOrder) != 3 {
		t.Fatalf("expected 3 stops, got %d", len(stopOrder))
	}
	if stopOrder[0] != "c3" || stopOrder[1] != "c2" || stopOrder[2] != "c1" {
		t.Errorf("stop order = %v, want [c3 c2 c1]", stopOrder)
	}
}

func TestStopAll_SkipsNotRunning(t *testing.T) {
	r := New(nil)

	c1 := newMockComponent("c1")
	c2 := newMockComponent("c2")

	_ = r.Register(c1)
	_ = r.Register(c2)

	// Only start c1
	_ = c1.Start(context.Background())

	ctx := context.Background()
	err := r.StopAll(ctx)
	if err != nil {
		t.Fatalf("StopAll() error = %v", err)
	}

	// c1 should have been stopped, c2 should not
	if c1.stopped.Load() != 1 {
		t.Error("c1 should have been stopped")
	}
	if c2.stopped.Load() != 0 {
		t.Error("c2 should NOT have been stopped (was not running)")
	}
}

func TestStopAll_ContinuesOnError(t *testing.T) {
	r := New(nil)

	c1 := newMockComponent("c1")
	c2 := newMockComponent("c2").withStopErr(errors.New("stop failed"))
	c3 := newMockComponent("c3")

	_ = r.Register(c1)
	_ = r.Register(c2)
	_ = r.Register(c3)

	ctx := context.Background()
	_ = r.StartAll(ctx)

	err := r.StopAll(ctx)
	// Should return the last error
	if err == nil {
		t.Error("StopAll() should return error when component fails to stop")
	}

	// All running components should have been attempted to stop
	// Note: c3 is stopped first (reverse order), then c2 fails, then c1
	if c3.stopped.Load() != 1 {
		t.Error("c3 should have been stopped")
	}
	if c1.stopped.Load() != 1 {
		t.Error("c1 should have been stopped despite c2 failing")
	}
}

func TestList(t *testing.T) {
	r := New(nil)

	c1 := newMockComponent("c1")
	c2 := newMockComponent("c2")

	_ = r.Register(c1)
	_ = r.Register(c2)

	// Start only c1
	_ = c1.Start(context.Background())

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("List() length = %d, want 2", len(list))
	}

	// Check order matches registration order
	if list[0].Name != "c1" {
		t.Errorf("list[0].Name = %s, want c1", list[0].Name)
	}
	if list[1].Name != "c2" {
		t.Errorf("list[1].Name = %s, want c2", list[1].Name)
	}

	// Check running state
	if !list[0].Running {
		t.Error("c1 should be running")
	}
	if list[1].Running {
		t.Error("c2 should not be running")
	}

	// Check type contains the actual type name
	if list[0].Type == "" {
		t.Error("Type should not be empty")
	}
}

func TestCount(t *testing.T) {
	r := New(nil)

	if r.Count() != 0 {
		t.Errorf("empty registry Count() = %d, want 0", r.Count())
	}

	_ = r.Register(newMockComponent("c1"))
	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1", r.Count())
	}

	_ = r.Register(newMockComponent("c2"))
	_ = r.Register(newMockComponent("c3"))
	if r.Count() != 3 {
		t.Errorf("Count() = %d, want 3", r.Count())
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := New(nil)

	// Pre-register some components
	for i := range 10 {
		_ = r.Register(newMockComponent(fmt.Sprintf("pre-%d", i)))
	}

	var wg sync.WaitGroup
	const goroutines = 20
	const opsPerGoroutine = 100

	// Concurrent reads
	for i := range goroutines {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			for j := range opsPerGoroutine {
				r.Get(fmt.Sprintf("pre-%d", j%10))
				r.Count()
				r.List()
			}
		}(i)
	}

	// Concurrent writes (registrations)
	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range opsPerGoroutine / 10 {
				name := fmt.Sprintf("concurrent-%d-%d", id, j)
				_ = r.Register(newMockComponent(name))
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("test timed out - possible deadlock")
	}
}

func TestStartAll_WithContext(t *testing.T) {
	r := New(nil)

	started := make(chan struct{})
	c := &mockComponent{
		name: "blocking",
	}
	c.onStart = func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	_ = r.Register(c)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- r.StartAll(ctx)
	}()

	select {
	case <-started:
		// Component started
	case <-time.After(time.Second):
		t.Fatal("component should have started")
	}

	// Wait for timeout
	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected DeadlineExceeded, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("StartAll should have returned after context timeout")
	}
}
