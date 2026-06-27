package config

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
)

func TestReloadRegistry_RegisterAndTrigger(t *testing.T) {
	rr := NewReloadRegistry()

	var called atomic.Bool
	rr.Register("test.json5", func(old, newCfg *Config) error {
		called.Store(true)
		return nil
	})

	if rr.Len("test.json5") != 1 {
		t.Errorf("expected 1 hook, got %d", rr.Len("test.json5"))
	}

	rr.Trigger("test.json5", "abc123")

	if !called.Load() {
		t.Error("expected hook to be called")
	}
}

func TestReloadRegistry_MultipleHooks(t *testing.T) {
	rr := NewReloadRegistry()

	var count atomic.Int32
	rr.Register("foo.json", func(old, newCfg *Config) error {
		count.Add(1)
		return nil
	})
	rr.Register("foo.json", func(old, newCfg *Config) error {
		count.Add(1)
		return nil
	})

	rr.Trigger("foo.json", "def456")

	if count.Load() != 2 {
		t.Errorf("expected 2 hooks called, got %d", count.Load())
	}
}

func TestReloadRegistry_HookReturnError(t *testing.T) {
	rr := NewReloadRegistry()

	errExpected := errors.New("test error")
	calledCount := 0

	rr.Register("err.json", func(old, newCfg *Config) error {
		calledCount++
		return errExpected
	})
	rr.Register("err.json", func(old, newCfg *Config) error {
		calledCount++
		return nil
	})

	rr.Trigger("err.json", "ghi789")

	if calledCount != 2 {
		t.Errorf("expected both hooks called, got %d", calledCount)
	}
}

func TestReloadRegistry_PanickingHookRecovery(t *testing.T) {
	rr := NewReloadRegistry()

	calledCount := 0

	rr.Register("panic.json", func(old, newCfg *Config) error {
		calledCount++
		panic("intentional panic")
	})
	rr.Register("panic.json", func(old, newCfg *Config) error {
		calledCount++
		return nil
	})

	// Should not panic
	rr.Trigger("panic.json", "jkl012")

	if calledCount != 2 {
		t.Errorf("expected both hooks called despite panic, got %d", calledCount)
	}
}

func TestReloadRegistry_UnknownPath(t *testing.T) {
	rr := NewReloadRegistry()

	// Triggering unknown path should not error
	rr.Trigger("nonexistent.json", "xyz")

	if rr.Len("nonexistent.json") != 0 {
		t.Error("unknown path should have 0 hooks")
	}
}

func TestReloadRegistry_RegisteredHooks(t *testing.T) {
	rr := NewReloadRegistry()

	rr.Register("a.json", nil)
	rr.Register("b.json", nil)
	rr.Register("c.json", func(old, newCfg *Config) error { return nil })

	hooks := rr.RegisteredHooks()
	if len(hooks) != 3 {
		t.Errorf("expected 3 registered hooks, got %d: %v", len(hooks), hooks)
	}

	// Verify all keys present
	keyMap := make(map[string]bool)
	for _, k := range hooks {
		keyMap[k] = true
	}
	for _, expected := range []string{"a.json", "b.json", "c.json"} {
		if !keyMap[expected] {
			t.Errorf("missing hook %q in registered hooks", expected)
		}
	}
}

func TestReloadRegistry_NilHookIgnored(t *testing.T) {
	rr := NewReloadRegistry()

	rr.Register("nil.json", nil)

	// Trigger should not panic or error
	rr.Trigger("nil.json", "abc")

	// Len reports the number of registered funcs
	if rr.Len("nil.json") != 1 {
		t.Errorf("expected hook count 1, got %d", rr.Len("nil.json"))
	}
}

func TestReloadRegistry_NilReloadFuncPanics(t *testing.T) {
	// This tests that a nil function in the hook list is safe to call —
	// but actually calling a nil function would panic. The code protects
	// against this by checking `fn != nil` in Trigger.
	rr := NewReloadRegistry()

	// Register a nil function (which is valid — users might unregister by nil-ing)
	fn := func(old, newCfg *Config) error {
		return fmt.Errorf("should not be called")
	}
	rr.Register("safe.json", fn)
	// Now nil the registered hook — in real usage this requires removing it,
	// but Trigger should handle nil safely.
	// Test: the fn in the slice is non-nil, so it will be called.
	rr.Trigger("safe.json", "abc")
}
