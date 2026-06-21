package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/compress"
)

func TestCompressionPromptWithoutPipeline(t *testing.T) {
	loop := NewAgentLoop(
		WithAgentConfig(AgentConfig{
			Constitution: "test constitution",
		}),
	)

	for _, tc := range []struct {
		name string
		fn   func() string
	}{
		{"buildSystemPrompt", func() string { return loop.buildSystemPrompt() }},
	} {
		name := tc.name
		prompt := tc.fn()
		if prompt == "" {
			t.Errorf("%s returned empty", name)
		}
		if strings.Contains(prompt, "CONTEXT COMPRESSION ACTIVE") {
			t.Errorf("%s: should NOT contain compression prompt when no pipeline", name)
		}
	}
}

func TestCompressionPromptWithContextPipeline(t *testing.T) {
	store := &memCCRStore{data: make(map[string]*compress.CCREntry)}
	pipeline := compress.NewPipelineWithConfig(store, compress.PipelineConfig{
		MinTokensToCompress: 500,
		TTL:                 time.Hour,
		EnableCCR:           true,
	})
	defer pipeline.Close()

	loop := NewAgentLoop(
		WithAgentConfig(AgentConfig{
			Constitution: "test constitution",
		}),
		WithCompressionPipeline(pipeline),
	)

	for _, tc := range []struct {
		name string
		fn   func() string
	}{
		{"buildSystemPrompt", func() string { return loop.buildSystemPrompt() }},
	} {
		name := tc.name
		prompt := tc.fn()
		if prompt == "" {
			t.Errorf("%s returned empty", name)
		}
		if !strings.Contains(prompt, "CONTEXT COMPRESSION ACTIVE") {
			t.Errorf("%s: should contain 'CONTEXT COMPRESSION ACTIVE' when pipeline is set", name)
		}
		if !strings.Contains(prompt, "mcc_retrieve") {
			t.Errorf("%s: should contain 'mcc_retrieve' when pipeline is set", name)
		}
		if !strings.Contains(prompt, "Originals are retained for 1 hour") {
			t.Errorf("%s: should contain retention info when pipeline is set", name)
		}
		// Verify the section has a proper header
		if !strings.Contains(prompt, "Context Compression") {
			t.Errorf("%s: should have 'Context Compression' section header", name)
		}
	}
}
