package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
)

// classifierAdapter wraps llm.Chatter to satisfy memory.ClassifierLLM.
type classifierAdapter struct {
	chatter llm.Chatter
	logger  *slog.Logger
}

func newClassifierAdapter(chatter llm.Chatter) *classifierAdapter {
	if chatter == nil {
		return nil
	}
	return &classifierAdapter{
		chatter: chatter,
		logger:  slog.Default().With("component", "epistemic-classifier"),
	}
}

func (a *classifierAdapter) ClassifyRelationships(ctx context.Context, newMem memory.Memory, candidates []memory.Memory) ([]memory.EdgeVerdict, error) {
	prompt := buildClassificationPrompt(newMem, candidates)
	resp, err := a.chatter.Chat(ctx, prompt, llm.WithTemperature(0.1))
	if err != nil {
		return nil, fmt.Errorf("classifier chat: %w", err)
	}
	if resp == nil || resp.Content == "" {
		return nil, fmt.Errorf("classifier returned empty response")
	}
	verdicts, err := memory.ParseClassifierJSON([]byte(resp.Content))
	if err != nil {
		return nil, fmt.Errorf("parse classifier json: %w", err)
	}
	return verdicts, nil
}

var _ memory.ClassifierLLM = (*classifierAdapter)(nil)

func buildClassificationPrompt(newMem memory.Memory, candidates []memory.Memory) []llm.ChatMessage {
	system := `You are an epistemic relationship classifier. Read the new memory and each candidate, then decide if a meaningful relationship exists.

Valid relationships:
- contradicts: the new memory asserts the opposite of the candidate
- superseded: the new memory replaces the candidate
- evidence_for: the new memory supports the candidate
- evidence_against: the new memory undermines the candidate
- derives_from: the new memory is derived from the candidate
- supports: the new memory reinforces the candidate (weaker than evidence_for)
- unrelated: no meaningful relationship

Return a JSON array of objects with keys: relation, target_id, confidence (0.0-1.0), explanation.
If no relationships, return [].`

	var candStr string
	for _, c := range candidates {
		candStr += fmt.Sprintf("- id=%s type=%s content=%q\n", c.ID, c.Type, c.Content)
	}
	user := fmt.Sprintf("New memory: id=%s type=%s content=%q\n\nCandidates:\n%s", newMem.ID, newMem.Type, newMem.Content, candStr)

	return []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: system},
		{Role: llm.RoleUser, Content: user},
	}
}

type ambientClassifierAdapter struct {
	chatter llm.Chatter
	logger  *slog.Logger
}

var _ memory.AmbientClassifierLLM = (*ambientClassifierAdapter)(nil)

func newAmbientClassifierAdapter(chatter llm.Chatter) *ambientClassifierAdapter {
	if chatter == nil {
		return nil
	}
	return &ambientClassifierAdapter{
		chatter: chatter,
		logger:  slog.Default().With("component", "ambient-extractor"),
	}
}

func (a *ambientClassifierAdapter) ExtractCandidates(ctx context.Context, prompt string) ([]byte, error) {
	// Grammar-constrained extraction: force a bare JSON array of candidate
	// objects (the shape memory.ParseAmbientCandidates accepts) directly on
	// the wire for llama.cpp-style endpoints. attachRawGrammar is nil-safe
	// and local-endpoint-gated: local models get the grammar, cloud
	// providers never see the field. DisableThinking keeps the LFM2.5
	// <think> template from fighting the grammar (mechanical extraction
	// gains nothing from reasoning); the task_summarizer uses the same
	// option for the same reason.
	resp, err := a.chatter.Chat(ctx, []llm.ChatMessage{
		{Role: llm.RoleUser, Content: prompt},
	}, llm.WithTemperature(0.2), llm.WithRawGrammar(llm.AmbientCandidateGrammar()), llm.DisableThinking())
	if err != nil {
		return nil, fmt.Errorf("ambient classifier chat: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("ambient classifier returned nil response")
	}
	return []byte(resp.Content), nil
}

func wireEpistemicDetector(memoryMgr *memory.Manager, chatter llm.Chatter, memCfg config.MemoryConfig, logger *slog.Logger) {
	if memoryMgr == nil || chatter == nil {
		return
	}
	graph := memoryMgr.Graph()
	if graph == nil {
		logger.Debug("epistemic detector skipped: no knowledge graph")
		return
	}
	detector := memory.NewEpistemicDetector(memory.EpistemicDetectorConfig{
		Graph:      graph,
		Manager:    memoryMgr,
		Classifier: newClassifierAdapter(chatter),
		Threshold:  memCfg.Epistemic.DetectionThreshold,
		AutoWeight: memory.EffectiveAutoTrustWeight(memCfg.Epistemic.AutoTrustWeight),
		Logger:     logger.With("component", "epistemic-detector"),
	})
	memoryMgr.SetEpistemicDetector(detector)
	logger.Info("epistemic detector wired")
}

func wireEpistemicHook(agentLoop *agent.AgentLoop, memoryMgr *memory.Manager, chatter llm.Chatter, memCfg config.MemoryConfig, logger *slog.Logger) {
	if agentLoop == nil || memoryMgr == nil || chatter == nil {
		return
	}
	if !memCfg.Epistemic.AmbientExtraction.Enabled {
		return
	}
	// Raw-response retention: the (prompt, rawBody) pair per classifier call,
	// written to the memory data dir (gitignored). Training/calibration data
	// for the judge lane; nil when no data dir. Raw conversation text — never
	// enters the repo.
	var rawHook func(prompt, rawBody string)
	if dataDir := memoryMgr.DataDir(); dataDir != "" {
		rw := &rawResponseWriter{path: filepath.Join(dataDir, "raw_responses.jsonl"), log: logger}
		rawHook = rw.write
	}
	extractor := memory.NewAmbientExtractor(memory.AmbientExtractorConfig{
		Manager:         memoryMgr,
		Classifier:      newAmbientClassifierAdapter(chatter),
		Logger:          logger.With("component", "ambient-extractor"),
		RawResponseHook: rawHook,
	})
	// Calibration log: rejected ambient candidates land in the memory data
	// dir. Nil (no data dir) = logging disabled; never blocks extraction.
	var rejectedLogger *agent.RejectedCandidateLogger
	if dataDir := memoryMgr.DataDir(); dataDir != "" {
		rejectedLogger = agent.NewRejectedCandidateLogger(dataDir, logger)
	}
	hook := agent.NewEpistemicHook(agent.EpistemicHookConfig{
		Cfg:            memCfg.Epistemic,
		Extractor:      extractor,
		Logger:         logger.With("component", "epistemic-hook"),
		RejectedLogger: rejectedLogger,
	})
	agentLoop.SetEpistemicHook(hook)
	logger.Info("epistemic hook wired",
		"ambient_extraction", true,
		"max_per_turn", memCfg.Epistemic.AmbientExtraction.MaxPerTurn,
	)
}

// wireFileWatcherHook creates a FileWatcherHook from the daemon config and
// attaches it to the agent loop via SetFileWatcher. No-op when agentLoop is
// nil, the hook is disabled in hooks config, or FileWatcher pattern is empty.
// When Async+AsyncRewake are enabled, the hook publishes hook.async_rewake
// signals that the loop's own consumer (armed in RunOnceWithParts) injects
// into the conversation at the next reasoning iteration.
func wireFileWatcherHook(agentLoop *agent.AgentLoop, cfg config.Config, bus *bus.MessageBus, logger *slog.Logger) {
	if agentLoop == nil {
		return
	}

	fwCfg := cfg.Hooks.FileWatcher
	if !fwCfg.Enabled || fwCfg.Pattern == "" {
		return
	}

	hook := agent.NewFileWatcherHook(
		fwCfg.Pattern,
		fwCfg.Debounce,
		fwCfg.Ignore,
		logger.With("component", "file-watcher"),
	)
	hook.Async = fwCfg.Async
	hook.AsyncRewake = fwCfg.AsyncRewake
	hook.SetBus(bus)

	agentLoop.SetFileWatcher(hook)
	logger.Info("file watcher hook wired",
		"pattern", fwCfg.Pattern,
		"debounce", fwCfg.Debounce,
		"async", fwCfg.Async,
		"async_rewake", fwCfg.AsyncRewake,
	)
}

// wireHTTPHooks converts each HTTP hook entry from the daemon config into an
// agent.HTTPHook and registers it as a session-start/session-end hook on the
// agent loop's hook registry. Each hook also gets the message bus reference
// (for async-rewake signals) when Async+AsyncRewake are enabled; those
// signals are consumed by the loop's own rewake consumer (see
// internal/agent/loop_rewake.go), which wakes the matching conversation at
// its next reasoning iteration.
//
// No-op when agentLoop is nil, the hook registry is nil, or no HTTP hooks are
// configured.
func wireHTTPHooks(agentLoop *agent.AgentLoop, cfg config.Config, bus *bus.MessageBus, logger *slog.Logger) {
	if agentLoop == nil {
		return
	}
	hr := agentLoop.HookRegistry()
	if hr == nil {
		return
	}
	if len(cfg.Hooks.HTTP) == 0 {
		return
	}

	wired := 0
	for i, hc := range cfg.Hooks.HTTP {
		// RetryCount pointer resolution: nil (key omitted in config) →
		// default 3; an explicit value (including 0 = no retries and
		// -1 = unlimited) passes through dereferenced. See
		// config.HTTPHookConfig.RetryCount for the three-way contract.
		retryCount := 3
		if hc.RetryCount != nil {
			retryCount = *hc.RetryCount
		}
		agentCfg := agent.HTTPHookConfig{
			URL:         hc.URL,
			Method:      hc.Method,
			Headers:     hc.Headers,
			Timeout:     hc.Timeout,
			RetryCount:  retryCount,
			Async:       hc.Async,
			AsyncRewake: hc.AsyncRewake,
		}
		// H9 (bughunt 2026-09-03): an empty allowlist makes every hook fail
		// "not in allowlist" before any request is sent (the old nil pass
		// made the whole HTTP-hook feature structurally dead). Default to
		// allowing the hook's OWN url; AllowedURLs widens it.
		allow := hc.AllowedURLs
		if len(allow) == 0 && hc.URL != "" {
			allow = []string{"^" + regexp.QuoteMeta(hc.URL) + "$"}
		}
		hook, err := agent.NewHTTPHook(agentCfg, allow, logger.With("hook", "http", "index", i))
		if err != nil {
			logger.Warn("failed to wire HTTP hook",
				"url", hc.URL,
				"error", err,
			)
			continue
		}
		if hc.Async && hc.AsyncRewake && bus != nil {
			hook.SetBus(bus)
		}
		// Register for both session-start and session-end events so HTTP
		// hooks fire on both boundaries. The hook type is set dynamically
		// by OnSessionStart/OnSessionEnd.
		hr.RegisterSessionStartHook("http_start_"+hc.URL, agent.HookPriorityNormal, hook)
		hr.RegisterSessionEndHook("http_end_"+hc.URL, agent.HookPriorityNormal, hook)
		wired++
	}

	logger.Info("HTTP hooks wired",
		"count", wired,
		"total_configured", len(cfg.Hooks.HTTP),
	)
}

// rawResponseWriter appends (prompt, response) pairs to a JSONL file.
// Write errors disable the writer after one warning (best-effort).
type rawResponseWriter struct {
	mu   sync.Mutex
	path string
	log  *slog.Logger
	dead bool
}

type rawResponseRecord struct {
	TS       string `json:"ts"`
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}

func (w *rawResponseWriter) write(prompt, rawBody string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dead {
		return
	}
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		w.dead = true
		w.log.Warn("raw-response logging disabled (mkdir)", "error", err)
		return
	}
	// Size-cap rotation (shared policy with the other calibration logs; the
	// helper lives in internal/memory, which this package already imports).
	if err := memory.RotateIfNeeded(w.path, memory.MaxCalibrationLogBytes); err != nil {
		w.dead = true
		w.log.Warn("raw-response logging disabled (rotate)", "error", err)
		return
	}
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		w.dead = true
		w.log.Warn("raw-response logging disabled (open)", "error", err)
		return
	}
	rec := rawResponseRecord{TS: time.Now().UTC().Format(time.RFC3339), Prompt: prompt, Response: rawBody}
	if err := json.NewEncoder(f).Encode(rec); err != nil {
		w.dead = true
		w.log.Warn("raw-response logging disabled (encode)", "error", err)
	}
	if err := f.Close(); err != nil {
		w.log.Warn("raw-response close failed", "error", err)
	}
}
