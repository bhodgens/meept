package daemon

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/agent/contextalloc"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/working"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/id"
)

// Daemon wiring for the context assembler's TierBudgetProvider adapter
// (context-memory-engine leaf 01-context-assembler/03-integration.md,
// task T6): constructs the branch-B store bundle and registers it on the
// agent loop AFTER the memory manager (and any working store) exist --
// the wiring-ordering rule from internal/daemon/AGENTS.md (construct
// evolver-adjacent components after their dependencies).
//
// Compile independence (the leaf's CRITICAL seam rule): the adapter lives
// in internal/agent/contextalloc and is the ONLY importer of
// internal/memory here; this file merely hands over the (possibly nil)
// store fields. Every nil store degrades to an absent tier; an all-nil
// bundle degrades to today's undivided memory path without a second
// guard. The loop setter carries the typed-nil guard (SetTierBudgetProvider),
// so wiring is unconditional at this site.
//
// The working store is constructed lazily HERE (the daemon owns its
// lifecycle) with the shipped ring defaults from branch B leaf 01;
// assemblerWorkConfig is the single source for the ring shape (also
// consumed by the tierpolicy engine build, leaf 03-tierpolicy-daemon T4).
//
// Leaf T5 (wiring-gap 03-tierpolicy-daemon): the store this file builds
// previously had NO producer - WireContextAssembler built working.Store
// and nothing ever called Append, so the ring stayed empty forever
// (LiveTokens == 0, every working-tier recall returned nothing, and the
// tierpolicy working band could never trigger). WireWorkingProducer
// below closes that gap; the producer's choice is DOCUMENTED here:
//
// PRODUCER SITE DECISION (leaf T5 asks to document it): the producer is
// a turn.terminal BUS SUBSCRIBER built beside the memory-capture
// subscriber, NOT a loop-level hook and NOT an extension of
// memory_capture itself. Reasons: (1) the turn.terminal event is the
// proven turn-end seam - loop-level hooks are the retired pattern this
// codebase already migrated away from because task-dispatched and
// relayed loops never received them (docs/plans/20261013-turn-terminal-
// capture); (2) extending memory_capture would couple the capture
// pipeline (L0 recording, distill enqueue, ambient extraction) to the
// assembler's ring - a capture regression would then also silence the
// working tier; a separate subscriber keeps the failure domains apart
// while reusing the same event, the same session-resolution fallback
// idiom, and the same best-effort posture. The second-brain leaf
// 03-agent-recall-wiring (landed 4cefbfc9) touched only recall-side
// filtering in loop.go buildMemoryContext - no overlap with this
// producer.
func WireContextAssembler(c *Components, cfg *config.Config) {
	if c == nil || c.AgentLoop == nil || cfg == nil {
		return
	}
	// Opt-in gate (docs/agents/opt-in-defaults.md): config only arms the
	// seam; the zero value leaves the loop untouched.
	if !cfg.Agent.ContextAssembler.Enabled {
		return
	}

	// Branch B stores. All may be nil (typed-nil guard on the loop
	// setter): the episodic and semantic handles come from the memory
	// manager, which is nil when the manager failed to initialize
	// (non-fatal at boot) -- the adapter then simply reports fewer tiers.
	var work *working.Store
	if workCfg := assemblerWorkConfig(); workCfg != nil {
		tk := llm.HeuristicTokenizer{}
		if s, err := working.New(*workCfg, &tk); err == nil {
			work = s
		}
		// Construction failure is non-fatal: the working tier is simply
		// absent and recall degrades to the episodic/semantic stores.
	}
	c.workingStore = work

	var epi *memory.EpisodicMemory
	if c.MemoryManager != nil {
		epi = c.MemoryManager.Episodic()
	}

	prov := contextalloc.NewTierStoreProvider(
		work, 0, // working ceiling: allocator decides (R-A4)
		epi, 0, // episodic ceiling: allocator decides
		c.MemoryManager, 0, // semantic recall via Manager.SearchSemantic
		nil, // tokenizer: the loop resolves its serving model's tokenizer
	)

	// SetTierBudgetProvider carries the typed-nil guard; a provider over
	// all-nil stores normalizes to "no seam" inside the loop.
	c.AgentLoop.SetTierBudgetProvider(prov)

	// The [agent.context_assembler] block itself (loop-side arm gate).
	// Set AFTER the provider so a config flip can never observe a
	// half-wired seam (construct-after-dependencies ordering).
	c.AgentLoop.SetContextAssemblerConfig(cfg.Agent.ContextAssembler)
}

// assemblerWorkConfig returns the working-ring configuration. Returns a
// value (never nil) today; the pointer shape exists so a config merge can
// later disable the ring by returning nil.
func assemblerWorkConfig() *working.Config {
	cfg := working.Config{
		MaxTokens: 8192, // ring capacity in tokens (branch B leaf 01 default)
		MaxItems:  200,  // hard item cap
	}
	return &cfg
}

// assemblerWorkConfigValue dereferences assemblerWorkConfig for callers
// that need a value (the tierpolicy engine build site). The zero-value
// Config is harmless there: the engine's WorkingConfig only feeds the
// working-tier ceiling default, and a disabled ring (nil pointer from a
// future config merge) carrying the zero value keeps that default
// neutral.
func assemblerWorkConfigValue() working.Config {
	if cfg := assemblerWorkConfig(); cfg != nil {
		return *cfg
	}
	return working.Config{}
}

// WireWorkingProducer subscribes the leaf-T5 turn-terminal producer that
// feeds the working-tier ring built by WireContextAssembler. The ring
// store is handed in EXPLICITLY so the structural contract is: no store,
// no subscriber (a nil store returns nil and the ring stays absent
// exactly as before this leaf - the wiring-failure-proof deletion
// target).
//
// Each completed turn appends ONE ring turn holding the exchange's user
// prompt + final reply (the same exchange memory-capture records to L0;
// the working tier is the token-bounded recent-window view of that same
// content). Dedup uses its own (conversation, reply) LRU - reusing the
// capture LRU shape - so relay+direct double terminals append once.
//
// Every event is handled in a panic-recovered goroutine (same posture as
// memory_capture.go): a producer failure logs and never fails the user's
// turn, and the bus pump is never blocked.
func WireWorkingProducer(b *bus.MessageBus, store session.Store, work *working.Store, logger *slog.Logger) *bus.Subscriber {
	if b == nil || work == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "working-producer")
	wp := &workingProducer{
		store:  store,
		work:   work,
		seen:   newCaptureLRU(workingProducerLRUSize),
		logger: logger,
	}
	return bus.SubscribeT(b, "working-producer", agent.TopicTurnTerminal, wp.handleEvent)
}

// workingProducerLRUSize bounds the (conversation, reply) dedup LRU.
const workingProducerLRUSize = 512

// workingProducer holds the producer's dependencies and dedup state.
type workingProducer struct {
	store  session.Store
	work   *working.Store
	seen   *captureLRU
	logger *slog.Logger
}

// handleEvent is the typed-bus callback: synchronous, cheap, hands the
// append to a recovered goroutine (the capture subscriber's shape).
func (wp *workingProducer) handleEvent(ev agent.TurnTerminalEvent) {
	if wp == nil || wp.work == nil {
		return
	}
	if ev.Status != "completed" {
		return
	}
	if ev.ConversationID == "" {
		return
	}
	if wp.seen.contains(ev.ConversationID, ev.Reply) {
		return
	}
	wp.seen.add(ev.ConversationID, ev.Reply)
	go wp.append(ev) //nolint:context // bus pump context: fire-and-forget
}

// append resolves the turn's session (the same conversation-id fallback
// memory-capture uses - the message store keys by PRIMARY session id,
// relayed turns carry the conversation id) and appends one ring turn
// with the exchange's user message + reply. Errors are logged, never
// fatal. Skips honestly when there is nothing to append: no store, no
// user message, and an empty reply all mean "no ring turn", never a
// zero-content placeholder.
func (wp *workingProducer) append(ev agent.TurnTerminalEvent) {
	defer func() {
		if r := recover(); r != nil {
			wp.logger.Error("working producer panicked",
				"conversation_id", ev.ConversationID, "panic", r,
				"stack", string(debug.Stack()))
		}
	}()
	if wp.store == nil {
		return
	}
	captureSession := ev.SessionID
	if captureSession == "" {
		captureSession = ev.ConversationID
	}
	userMsg, ok := wp.lastUserMessage(captureSession)
	if !ok {
		if sess := wp.store.GetByConversationID(captureSession); sess != nil && sess.ID != captureSession {
			userMsg, ok = wp.lastUserMessage(sess.ID)
			if ok {
				captureSession = sess.ID
			}
		}
	}
	if !ok && ev.Reply == "" {
		return // nothing to hold in the ring
	}
	content := ev.Reply
	if userMsg != "" {
		content = userMsg + "\n" + ev.Reply
	}
	if content == "" {
		return
	}
	turnID := ev.TurnID
	if turnID == "" {
		// pkg/id only: never time/rand (predictable-ID rule).
		turnID = id.Generate("working-turn")
	}
	if err := wp.work.Append(context.Background(), working.Turn{
		ID:        turnID,
		SessionID: captureSession,
		Role:      "turn",
		Content:   content,
		At:        time.Now().UTC(),
	}); err != nil {
		wp.logger.Warn("working ring append failed",
			"session_id", captureSession, "error", err)
		return
	}
	wp.logger.Debug("working ring turn appended",
		"session_id", captureSession,
		"turn_id", turnID,
		"live_tokens", wp.work.LiveTokens(),
	)
}

// lastUserMessage is the memory-capture scan (backwards over the last
// page for the newest role==user message). Duplicated deliberately
// rather than exported from the capture type: the two subscribers fail
// independently by design (see the producer-site decision above).
func (wp *workingProducer) lastUserMessage(sessionID string) (string, bool) {
	total, err := wp.store.GetMessageCount(sessionID)
	if err != nil || total == 0 {
		return "", false
	}
	offset := total - memoryCapturePage
	if offset < 0 {
		offset = 0
	}
	msgs, err := wp.store.GetMessages(sessionID, offset, memoryCapturePage)
	if err != nil {
		wp.logger.Warn("working producer: get messages failed",
			"session_id", sessionID, "error", err)
		return "", false
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" && msgs[i].Content != "" {
			return msgs[i].Content, true
		}
	}
	return "", false
}
