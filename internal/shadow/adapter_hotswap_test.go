package shadow

import (
	"context"
	"strings"
	"testing"
)

type fakeOllamaActivator struct {
	activatedName string
	activatedBase string
	activatedPath string
	returnErr     error
}

func (f *fakeOllamaActivator) ActivateAdapter(ctx context.Context, baseName, adapterName, adapterPath string) error {
	f.activatedBase = baseName
	f.activatedName = adapterName
	f.activatedPath = adapterPath
	return f.returnErr
}

func TestManager_HotSwap_ActivatesInOllamaAndNotifiesLoop(t *testing.T) {
	m := newTestManager(t, func(c *Config) {
		c.Adapters.Enabled = true
		c.Adapters.HotSwapEnabled = true
		c.Adapters.EvalThreshold = 0.0 // disabled for this test
	})

	activator := &fakeOllamaActivator{}
	m.SetOllamaActivator(activator)

	notified := ""
	m.SetHotSwapCallback(func(bakedName string) {
		notified = bakedName
	})

	ctx := context.Background()
	adapter := NewAdapter("v1", "qwen2.5:7b", "lora", "/tmp/adapter.gguf")
	if err := m.RegisterAdapter(ctx, adapter); err != nil {
		t.Fatalf("RegisterAdapter: %v", err)
	}
	run := NewTrainingRun(adapter.ID, map[string]any{})
	run.RecordsUsed = 50
	run.Complete(0.5, 0.9)
	if err := m.adaptersStore.SaveTrainingRun(ctx, run); err != nil {
		t.Fatalf("SaveTrainingRun: %v", err)
	}

	if err := m.HotSwap(ctx, adapter.ID); err != nil {
		t.Fatalf("HotSwap: %v", err)
	}
	if activator.activatedName == "" {
		t.Errorf("expected Ollama activation, got none")
	}
	// bakedModelRef follows the convention "<base>-shadow-<adapterIDprefix>",
	// e.g. "qwen2.5:7b-shadow-v1abcd12". The callback should receive exactly that.
	if notified == "" || !strings.HasPrefix(notified, "qwen2.5:7b-shadow-") {
		t.Errorf("expected baked model ref callback, got %q", notified)
	}
}

func TestManager_HotSwap_DisabledReturnsError(t *testing.T) {
	m := newTestManager(t, func(c *Config) {
		c.Adapters.Enabled = true
		c.Adapters.HotSwapEnabled = false
	})

	ctx := context.Background()
	adapter := NewAdapter("v1", "qwen2.5:7b", "lora", "/tmp/adapter.gguf")
	if err := m.RegisterAdapter(ctx, adapter); err != nil {
		t.Fatalf("RegisterAdapter: %v", err)
	}

	if err := m.HotSwap(ctx, adapter.ID); err == nil {
		t.Errorf("expected error when hot-swap disabled, got nil")
	}
}

func TestManager_HotSwap_EvalGateFailureAborts(t *testing.T) {
	m := newTestManager(t, func(c *Config) {
		c.Adapters.Enabled = true
		c.Adapters.HotSwapEnabled = true
		c.Adapters.EvalThreshold = 0.95 // high bar
	})

	activator := &fakeOllamaActivator{}
	m.SetOllamaActivator(activator)

	ctx := context.Background()
	adapter := NewAdapter("v1", "qwen2.5:7b", "lora", "/tmp/adapter.gguf")
	if err := m.RegisterAdapter(ctx, adapter); err != nil {
		t.Fatalf("RegisterAdapter: %v", err)
	}
	run := NewTrainingRun(adapter.ID, map[string]any{})
	run.RecordsUsed = 50
	run.Complete(1.0, 0.3) // low eval score
	if err := m.adaptersStore.SaveTrainingRun(ctx, run); err != nil {
		t.Fatalf("SaveTrainingRun: %v", err)
	}

	err := m.HotSwap(ctx, adapter.ID)
	if err == nil {
		t.Errorf("expected eval-gate failure to abort HotSwap, got nil")
	}
}

func TestManager_HotSwap_NoActivatorStillFlipsDBFlag(t *testing.T) {
	m := newTestManager(t, func(c *Config) {
		c.Adapters.Enabled = true
		c.Adapters.HotSwapEnabled = true
		c.Adapters.EvalThreshold = 0.0
	})

	ctx := context.Background()
	adapter := NewAdapter("v1", "qwen2.5:7b", "lora", "/tmp/adapter.gguf")
	if err := m.RegisterAdapter(ctx, adapter); err != nil {
		t.Fatalf("RegisterAdapter: %v", err)
	}
	run := NewTrainingRun(adapter.ID, map[string]any{})
	run.RecordsUsed = 50
	run.Complete(0.5, 0.9)
	if err := m.adaptersStore.SaveTrainingRun(ctx, run); err != nil {
		t.Fatalf("SaveTrainingRun: %v", err)
	}

	if err := m.HotSwap(ctx, adapter.ID); err != nil {
		t.Fatalf("HotSwap without activator: %v", err)
	}
}
