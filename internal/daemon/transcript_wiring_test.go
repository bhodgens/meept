package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// transcriptWiringModelsConfig builds a minimal, hermetic models config
// (one resolvable provider/model set) so createAuxiliaryLLMClientWithResolver
// can construct auxiliary clients in these tests without touching the
// developer's real models.json5. NoAuth + loopback URL: the clients are
// constructed lazily and never Chatted with.
func transcriptWiringModelsConfig() *config.ModelsConfig {
	return &config.ModelsConfig{
		Model:          "testprov/main-model",
		SmallModel:     "testprov/summarizer-small",
		DefaultTimeout: 30,
		Providers: map[string]config.Provider{
			"testprov": {
				API: "openai",
				Options: config.ProviderOptions{
					BaseURL: "http://127.0.0.1:9/v1",
					NoAuth:  true,
				},
				Models: map[string]config.Model{
					"main-model":           {Name: "main-model"},
					"summarizer-small":     {Name: "summarizer-small"},
					"summarizer-dedicated": {Name: "summarizer-dedicated"},
				},
			},
		},
	}
}

// newTranscriptWiringComponents constructs Components with an injected
// hermetic models config (unlike the skill-tools harness, which loads the
// host's models.json5 — these tests must control which model refs resolve).
func newTranscriptWiringComponents(t *testing.T, cfg *config.Config) *Components {
	t.Helper()
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger, transcriptWiringModelsConfig())
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = comps.Stop(ctx)
	})
	return comps
}

// transcriptStubRunner returns a runner standing in for the Python
// subprocess: it never executes anything and always yields the given
// stdout (one transcript segment). Mirrors the builtin tests' injectable
// runner trick so execution can REACH the summarize branch.
func transcriptStubRunner(stdout string) func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
	return func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		return []byte(stdout), nil, nil
	}
}

const transcriptStubSegment = `{"text":"hello wired world","start":0}`

// TestTranscriptSummarizerClientConstruction verifies the dedicated
// transcript summarizer client is built EXACTLY when configured:
// summarize_enabled AND summarize_model both set -> non-nil field whose
// resolved config names the configured model; either condition absent ->
// the field stays nil (chain default SummarizerClient remains correct).
func TestTranscriptSummarizerClientConstruction(t *testing.T) {
	// Dedicated: both conditions set.
	cfg, _ := skillToolsTestConfig(t)
	cfg.Transcript = config.TranscriptConfig{
		Enabled:          true,
		SummarizeEnabled: true,
		SummarizeModel:   "testprov/summarizer-dedicated",
	}
	comps := newTranscriptWiringComponents(t, cfg)
	if comps.TranscriptSummarizerClient == nil {
		t.Fatal("TranscriptSummarizerClient nil with summarize_enabled + summarize_model set")
	}
	if got := comps.TranscriptSummarizerClient.Config().ModelID; got != "summarizer-dedicated" {
		t.Errorf("dedicated client model = %q, want %q", got, "summarizer-dedicated")
	}
	// The chain-default summarizer is still built alongside.
	if comps.SummarizerClient == nil {
		t.Error("SummarizerClient nil; chain default not built")
	}

	// Negative: summarize_model empty -> no dedicated client even when
	// summarize_enabled (chain default is the correct chatter).
	cfgChain, _ := skillToolsTestConfig(t)
	cfgChain.Transcript = config.TranscriptConfig{Enabled: true, SummarizeEnabled: true}
	compsChain := newTranscriptWiringComponents(t, cfgChain)
	if compsChain.TranscriptSummarizerClient != nil {
		t.Error("TranscriptSummarizerClient built without summarize_model")
	}
	if compsChain.SummarizerClient == nil {
		t.Error("SummarizerClient nil in chain-default case")
	}

	// Negative: summarize_enabled false -> never built, model or not.
	cfgOff, _ := skillToolsTestConfig(t)
	cfgOff.Transcript = config.TranscriptConfig{
		Enabled:        true,
		SummarizeModel: "testprov/summarizer-dedicated",
	}
	compsOff := newTranscriptWiringComponents(t, cfgOff)
	if compsOff.TranscriptSummarizerClient != nil {
		t.Error("TranscriptSummarizerClient built with summarize_enabled=false")
	}
}

// TestTranscriptFetchSummarizeWiring verifies the registration block
// injects a live chatter into transcript_fetch ONLY when summarize is
// enabled, observed FUNCTIONALLY through the tool's own error paths:
//
//   - enabled: summarize=true must skip the nil-chatter config error and
//     fail at the subprocess instead (nonexistent python path — the fetch
//     runs first). Hitting the python-path error proves SetSummarizer
//     received a non-nil chatter.
//   - disabled: a stubbed runner lets the fetch SUCCEED so execution
//     reaches the summarize branch, which must return the honest
//     "summarization not configured" error (SetSummarizer never called).
func TestTranscriptFetchSummarizeWiring(t *testing.T) {
	enabledCfg := func() *config.Config {
		cfg, _ := skillToolsTestConfig(t)
		cfg.Transcript = config.TranscriptConfig{
			Enabled:        true,
			PythonPath:     "/nonexistent/python-for-wiring-test",
			ModuleName:     "youtube-transcript-api",
			TimeoutSeconds: 5,
		}
		return cfg
	}

	t.Run("enabled_injects_chatter", func(t *testing.T) {
		cfg := enabledCfg()
		// Chain default: summarize_enabled without summarize_model -> the
		// SummarizerClient is the injected chatter.
		cfg.Transcript.SummarizeEnabled = true
		comps := newTranscriptWiringComponents(t, cfg)
		if tool := comps.ToolRegistry.Get("transcript_fetch"); tool == nil {
			t.Fatal("transcript_fetch not registered")
		}

		res, err := executeSkillTool(t, comps, "transcript_fetch", map[string]any{
			"url":       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			"summarize": true,
		})
		if err != nil {
			t.Fatalf("transcript_fetch execute: %v", err)
		}
		if res.Success {
			t.Skip("subprocess unexpectedly succeeded; environment has a real runner")
		}
		if strings.Contains(res.Error, "summarization not configured") {
			t.Errorf("summarize=true hit the nil-chatter config error; chatter was not injected: %s", res.Error)
		}
		if !strings.Contains(res.Error, "/nonexistent/python-for-wiring-test") {
			t.Errorf("error %q does not name the configured python path; fetch did not run before summarize", res.Error)
		}
	})

	t.Run("enabled_dedicated_model_injects_chatter", func(t *testing.T) {
		cfg := enabledCfg()
		// Dedicated: summarize_model set -> the TranscriptSummarizerClient
		// (non-nil per TestTranscriptSummarizerClientConstruction) is the
		// injected chatter.
		cfg.Transcript.SummarizeEnabled = true
		cfg.Transcript.SummarizeModel = "testprov/summarizer-dedicated"
		comps := newTranscriptWiringComponents(t, cfg)

		res, err := executeSkillTool(t, comps, "transcript_fetch", map[string]any{
			"url":       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			"summarize": true,
		})
		if err != nil {
			t.Fatalf("transcript_fetch execute: %v", err)
		}
		if res.Success {
			t.Skip("subprocess unexpectedly succeeded; environment has a real runner")
		}
		if strings.Contains(res.Error, "summarization not configured") {
			t.Errorf("summarize=true hit the nil-chatter config error; dedicated chatter was not injected: %s", res.Error)
		}
		if !strings.Contains(res.Error, "/nonexistent/python-for-wiring-test") {
			t.Errorf("error %q does not name the configured python path; fetch did not run before summarize", res.Error)
		}
	})

	t.Run("disabled_no_chatter", func(t *testing.T) {
		comps := newTranscriptWiringComponents(t, enabledCfg())
		tool := comps.ToolRegistry.Get("transcript_fetch")
		if tool == nil {
			t.Fatal("transcript_fetch not registered")
		}
		ft, ok := tool.(*builtin.TranscriptFetchTool)
		if !ok {
			t.Fatalf("registry tool is %T, want *builtin.TranscriptFetchTool", tool)
		}
		ft.SetTranscriptRunner(transcriptStubRunner(transcriptStubSegment))

		res, err := executeSkillTool(t, comps, "transcript_fetch", map[string]any{
			"url":       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			"summarize": true,
		})
		if err != nil {
			t.Fatalf("transcript_fetch execute: %v", err)
		}
		if res.Success {
			t.Fatal("summarize=true succeeded with summarizer disabled")
		}
		if !strings.Contains(res.Error, "summarization not configured") {
			t.Errorf("error = %q, want the summarization-not-configured contract error", res.Error)
		}
	})
}

// TestTranscriptFetchFallbackOutputDirWiring verifies FallbackOutputDir
// flows from [transcript] config through construction: with no session
// working directory in the context, a relative output_path must land
// under the CONFIGURED fallback root (not the ~/.meept/media default).
func TestTranscriptFetchFallbackOutputDirWiring(t *testing.T) {
	cfg, tmpDir := skillToolsTestConfig(t)
	fallback := filepath.Join(tmpDir, "transcript-media")
	cfg.Transcript = config.TranscriptConfig{
		Enabled:           true,
		FallbackOutputDir: fallback,
	}
	comps := newTranscriptWiringComponents(t, cfg)
	registered := comps.ToolRegistry.Get("transcript_fetch")
	if registered == nil {
		t.Fatal("transcript_fetch not registered")
	}
	ft, ok := registered.(*builtin.TranscriptFetchTool)
	if !ok {
		t.Fatalf("registry tool is %T, want *builtin.TranscriptFetchTool", registered)
	}
	ft.SetTranscriptRunner(transcriptStubRunner(transcriptStubSegment))

	res, err := executeSkillTool(t, comps, "transcript_fetch", map[string]any{
		"url":         "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"output_path": "wired/probe.md",
	})
	if err != nil {
		t.Fatalf("transcript_fetch execute: %v", err)
	}
	requireSuccess(t, res, "transcript_fetch")

	wantPath := filepath.Join(fallback, "wired", "probe.md")
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Fatalf("output file not written under configured FallbackOutputDir %s: %v", wantPath, statErr)
	}
}
