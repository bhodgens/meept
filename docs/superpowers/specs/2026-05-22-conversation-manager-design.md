# Conversation Manager Design

Date: 2026-05-22
Status: Approved

## Problem

Three related issues with Meept's current agent dispatch:

1. **Agent definitions are static** — 13 hardcoded specs in `internal/agent/spec.go`. No UX for users (or the Q agent) to create new agents interactively.
2. **Classifier-only routing is fragile** — A single LLM classification determines intent, with no conversational fallback. Wrong classification sends the user to the wrong specialist.
3. **No conversational continuity** — The Dispatcher is a stateless service struct, not an agent. It can't maintain conversation context, ask clarifying questions, or track multiple active tasks.

The user should be able to chat with a fast local dispatcher that maintains conversational context, correctly routes to specialists (even across multiple simultaneous tasks), and presents synthesized results with keyboard-selectable options.

## Design

### Architecture

```
User Input (CLI/Telegram/Web/MenuBar)
    -> ChatHandler
    -> ConversationManager (NEW)
        ├── Classifier (existing, fast local model)
        │   └── classifyIntent() -> Intent + confidence
        ├── Mediator (NEW, smarter model, on-demand)
        │   └── complex routing, multi-agent synthesis, clarification
        └── Synthesizer (NEW)
            └── applies synthesis level -> A/B/C options or raw response
    -> MessageBus
    -> Worker Pool -> Specialist AgentLoop
    -> MessageBus (task.completed/failed/progress)
    -> ConversationManager
        └── Synthesizer -> user response
```

### New Components

| Component | Package | Purpose |
|-----------|---------|---------|
| `ConversationManager` | `internal/conversation/manager.go` | Stateful session coordinator, bus subscriber, routing orchestrator |
| `Mediator` | `internal/conversation/mediator.go` | Structured LLM calls for complex routing and multi-agent weaving |
| `Synthesizer` | `internal/conversation/synthesizer.go` | Response formatting -- none/terse/plain + A/B/C option generation |
| `ConversationState` | `internal/conversation/state.go` | Per-session state: active specialists, context, pending options, synthesis level |

### Modified Components

| Component | Change |
|-----------|--------|
| `ChatHandler` | Publishes to `conversation.incoming` / subscribes to `conversation.outgoing` instead of calling Dispatcher directly |
| `Dispatcher` | Becomes a specialist agent for complex multi-turn mediation, registered in AgentRegistry |
| `AgentRegistry` | Hot-reloads AGENT.md files on interval, exposes agent list to ConversationManager |
| CLI (`cmd/meept`) | New `agents` subcommand (create, list, show, edit, validate, reload) |

### Removed Components

| Component | Why |
|-----------|-----|
| Sync `RunOnce()` path in ChatHandler | Replaced by unified bus-based delegation |
| Direct `Dispatcher.ClassifyAndRoute()` call | Replaced by ConversationManager flow |

## ConversationManager

Long-running daemon component started alongside the Orchestrator and WorkerPool. Subscribes to bus topics and maintains per-session state.

**Session model**: Conversations are 1:1 with sessions. Each client connection (CLI invocation, Telegram chat, HTTP connection) creates a session with exactly one ConversationState. There is no concept of multiple conversations within a session. If the user starts a planning task and then says "also debug X", the ConversationManager handles both within the same session's state by hot-switching or adding specialists.

### Struct

```go
type ConversationManager struct {
    bus           *bus.MessageBus
    classifier    *Dispatcher          // existing classifier pipeline
    mediator      *Mediator            // new: structured LLM calls
    synthesizer   *Synthesizer         // new: response formatting
    registry      *agent.AgentRegistry // agent discovery
    queue         queue.Queue          // job enqueue
    sessions      map[string]*ConversationState
    mu            sync.RWMutex
    logger        *slog.Logger
}
```

### Message Flow (per user message)

**Step 1 -- Intake:**
- ChatHandler receives user message from RPC/HTTP
- Publishes `conversation.incoming` on bus with `{session_id, message, metadata}`
- ConversationManager picks it up

**Step 2 -- Classify (revised pipeline):**
1. Short-message guard -- skip everything for brief inputs ("hi", "thanks")
2. Keyword classifier -- deterministic, instant, no LLM
3. Capability matcher -- fast skill/capability lookup
4. LLM classifier -- receives keyword + capability results as priors
5. Semantic index -- embedding similarity
6. Heuristic fallback

**Step 3 -- Decision (3 tiers):**

| Confidence | Action | Model Used |
|------------|--------|------------|
| High (>=0.8) + simple intent | Delegate directly | Classifier (local) |
| Medium (0.5-0.8) or ambiguous | Invoke mediator for routing | Mediator (smarter) |
| Low (<0.5) or multi-intent | Invoke mediator for clarification | Mediator (smarter) |

**Step 4 -- Delegate:**
- Create `queue.Job` with `{agent_id, payload, session_id, task_id}`
- Enqueue via existing queue
- Worker pool claims and runs the specialist's `AgentLoop.RunOnce()`

**Step 5 -- Receive result:**
- ConversationManager subscribes to `task.completed`, `task.failed`, `task.progress`
- On completion: run result through Synthesizer
- Publish `conversation.outgoing` on bus with synthesized response

**Step 6 -- Present:**
- ChatHandler subscribes to `conversation.outgoing`
- Sends to user (TUI, HTTP, Telegram, etc.)

### Revised Classification Pipeline

```
User Message
    |
1. Short-message guard (brief inputs -> IntentChat)
    |
2. Keyword classifier -- deterministic, instant, no LLM
    |
3. Capability matcher -- fast skill/capability lookup
    |
4. LLM classifier -- receives keyword + capability results as priors
    |
5. Semantic index -- embedding similarity
    |
6. Heuristic fallback
```

The keyword classifier runs first because:
- It is deterministic -- "debug" always maps to IntentDebug
- Its output is free (no LLM call, no latency)
- It provides priors that ground the LLM classifier

The LLM classifier prompt includes pre-analysis results:
```
Pre-analysis:
  Keyword match: {intent} (confidence: {score})
  Capability match: {intent} (confidence: {score})
  Available agents: {list of registered agents}

If the pre-analysis is high-confidence (>=0.7), agree unless you
have strong evidence to override.
```

Decision tree after pre-analysis:
- High confidence (>=0.8): skip LLM classifier entirely, route directly
- Medium confidence (0.5-0.8): run LLM classifier with pre-analysis as context
- Low confidence (<0.5): run LLM classifier + mediator if still unclear

### ConversationState

```go
type ConversationState struct {
    SessionID         string
    ActiveSpecialists map[string]*SpecialistState  // agent_id -> state
    IntentHistory     []IntentRecord
    PendingOptions    []Option                     // A/B/C choices
    SynthesisLevel    SynthesisLevel               // none/terse/plain
    LastActivity      time.Time
    ContextBuffer     []ContextEntry               // rolling context window
}

type SpecialistState struct {
    AgentID    string
    TaskID     string
    Status     string   // pending, running, completed, failed
    Result     string
}
```

### Hot-Switching Specialists

When the user says "now also do Y" mid-conversation:

1. Classifier detects new intent while ConversationState has an active specialist
2. Mediator evaluates: spawn a second specialist, or replace the current one?
3. New job enqueued for second specialist
4. Synthesizer weaves both results together

## Synthesizer

Transforms specialist output into user-facing response, controlled by `SynthesisLevel`.

### Synthesis Levels

| Level | Single Specialist | Multiple Specialists / Complex |
|-------|-------------------|-------------------------------|
| `none` | Pass through raw specialist response verbatim | Concatenate specialist responses with separator |
| `terse` | 2-3 sentence summary + key action taken | 2-3 sentence synthesis + A/B/C options for next action |
| `plain` | Plain english explanation, no jargon | Plain english summary + A/B/C options in simple terms |

### Synthesized Response Structure

```go
type SynthesizedResponse struct {
    Body       string     // The main response text
    Options    []Option   // Keyboard-selectable choices (can be empty)
    Specialist string     // Which agent produced the underlying result
    Metadata   map[string]any
}

type Option struct {
    Key         string  // "a", "b", "c", etc.
    Label       string  // Short label for TUI display
    Description string  // One-line explanation of what this choice does
    Intent      string  // Pre-classified intent if user picks this
    AgentID     string  // Target agent if user picks this
}
```

### TUI Presentation

When a response contains options, the TUI renders:

```
The planner suggests 3 approaches for your migration:

  [a] incremental migration   -- move modules one at a time, low risk
  [b] big bang rewrite        -- rewrite everything in one pass
  [c] hybrid approach         -- core rewrite + incremental wrappers
  [other] type your own approach

>
```

Single keypresses a/b/c select an option. The client sends the option's Intent and AgentID back to the ConversationManager, which skips classification and delegates directly. Typing anything else falls through to normal classification.

### When Options Appear

- Multiple specialists return results for the same conversation
- The mediator detects ambiguity (more than one reasonable path)
- The specialist's response contains questions or decision points
- The user explicitly asks for options

Options are NOT generated for:
- Simple factual responses
- Straightforward code generation
- `none` synthesis level

## Mediator

Structured LLM caller (not a full agent loop). Invoked on-demand when the classifier can't make a confident routing decision or when synthesis of multiple specialist outputs is needed.

### Interface

```go
type Mediator struct {
    client    llm.Client
    logger    *slog.Logger
}

type MediatorInput struct {
    UserMessage       string
    Classification    ClassificationResult
    SessionContext    *ConversationState
    SpecialistResults []SpecialistResult
}

type MediatorDecision struct {
    Action        MediatorAction  // route, clarify, weave, escalate
    AgentID       string          // target agent (if route)
    Clarification string          // question for user (if clarify)
    Synthesis     string          // woven response (if weave)
    Options       []Option        // A/B/C choices
}
```

### Mediator Actions

| Action | Trigger | Output |
|--------|---------|--------|
| `route` | Classifier confidence < 0.8 | Target agent ID + routing metadata |
| `clarify` | Ambiguous intent, multiple valid interpretations | Clarifying question for user |
| `weave` | 2+ specialists returned results | Synthesized narrative combining outputs |
| `escalate` | Specialist failed or stalled | Recovery plan or re-route |

### Model Selection

- **Classifier**: Fast local model (existing, configured via `classifier_model`)
- **Mediator**: Configured via `mediator_model` -- can be a smarter cloud model or larger local model. Falls back to default if not specified

The cheap/fast model handles ~80% of messages (high-confidence routing). The expensive/smart model only activates for the ~20% that need it.

## Dispatcher as Specialist Agent

The existing Dispatcher struct is refactored: classification and routing logic moves to ConversationManager. The dispatcher becomes a specialist agent registered in the AgentRegistry.

Invoked by the ConversationManager when:
- Multi-turn clarification dialogs where the mediator can't resolve in one shot
- Weaving results from 3+ specialists into a coherent narrative
- User explicitly requests to talk to the dispatcher
- Recovery from specialist failures needing conversational debugging

Gets a `conversation_state` tool returning the current ConversationState for reasoning about active specialists and history.

## Agent Discovery & CLI

### Hot Reload

Watch agent directories for changes. When a new AGENT.md appears or changes, reload and merge without daemon restart.

Discovery sources (priority order):
1. `.meept/agents/<id>/AGENT.md` -- project-local
2. `~/.meept/agents/<id>/AGENT.md` -- user-global
3. `~/.config/meept/agents/<id>/AGENT.md` -- system-wide
4. `config/agents/<id>/AGENT.md` -- bundled defaults

New AGENT.md files that define agent IDs not in programmatic defaults are auto-registered in the AgentRegistry, added to the CapabilitiesMap, and logged at startup.

### CLI Commands

```
meept agents list                         # List all registered agents with status
meept agents show <id>                    # Show full agent spec + AGENT.md content
meept agents create [id]                  # Interactive agent creation wizard
meept agents create [id] --from <file>    # Create from existing AGENT.md
meept agents edit <id>                    # Open AGENT.md in $EDITOR
meept agents validate <file>              # Validate an AGENT.md without registering
meept agents reload                       # Force hot-reload of all agent definitions
```

### Creation Wizard Flow

1. Agent ID (required, validated for uniqueness)
2. Name (display name)
3. Role: executor / reviewer / dispatcher
4. Purpose description (opens $EDITOR for multiline)
5. Tools: present available tools as multi-select
6. Skills: present discovered skills as multi-select
7. Temperature (default: 0.5)
8. Max iterations (default: 10)
9. Timeout (default: 300s)
10. Model override (optional, press enter to skip)
11. Capabilities tags (comma-separated)
12. Review and confirm -- writes AGENT.md to `~/.meept/agents/<id>/AGENT.md`

### Q Agent Integration

- Q agent writes to `~/.meept/agents/<id>/AGENT.md` (same location as manual creates)
- Hot-reload picks it up automatically
- Q agent can suggest capability tags in generated AGENT.md files
- `meept q analyze` output includes a `created_agents` section

## Configuration

New section in `~/.meept/meept.json5`:

```json5
{
  conversation: {
    // Synthesis level controls how specialist responses are presented to the user.
    //   "none"  -- raw specialist output, no mediation. Good for advanced users
    //             who want unfiltered agent responses.
    //   "terse" -- (default) compress to 2-3 sentences + A/B/C options when
    //             multiple specialists are involved or the response contains
    //             decision points. Options are keyboard-selectable in the TUI.
    //   "plain" -- like terse but in plain english without technical jargon.
    //             Good for non-technical users or when specialists return
    //             dense output.
    synthesis_level: "terse",

    // Maximum A/B/C options to present per response. Set to 0 to disable
    // option generation entirely (synthesis still applies to the body text).
    max_options: 5,

    // Model used for the mediator (complex routing, multi-agent weaving).
    // When empty, falls back to the default model from the resolver.
    // Use a model ref from config/models.json5 (e.g., "gpt-4o", "claude-sonnet").
    // The mediator only activates when the classifier confidence is below
    // threshold, so this model sees ~20% of messages.
    mediator_model: "",

    // Confidence thresholds for the classification pipeline.
    // >= high_threshold: route directly, skip LLM classifier
    // >= medium_threshold: run LLM classifier with keyword priors
    // < medium_threshold: invoke mediator
    classification: {
      high_threshold: 0.8,
      medium_threshold: 0.5,
    },

    // Hot-reload interval for agent definitions. Set to 0 to disable
    // (agents only load at startup). Agents are discovered from:
    //   .meept/agents/<id>/AGENT.md (project-local, highest priority)
    //   ~/.meept/agents/<id>/AGENT.md (user-global)
    //   ~/.config/meept/agents/<id>/AGENT.md (system-wide)
    agent_reload_interval_seconds: 30,
  },
}
```

No breaking changes -- existing configs work as-is. The `conversation` section is optional with sensible defaults.

### ChatHandler Simplification

```go
// Before:
result, err := h.dispatcher.ClassifyAndRoute(ctx, message, sessionID)

// After:
h.bus.Publish("conversation.incoming", incomingMsg)
// ... response arrives via "conversation.outgoing" subscription
```

ChatHandler becomes a thin adapter: bus publish on input, bus subscribe for output.

## Bus Topics (New)

| Topic | Direction | Purpose |
|-------|-----------|---------|
| `conversation.incoming` | ChatHandler -> ConversationManager | User messages |
| `conversation.outgoing` | ConversationManager -> ChatHandler | Synthesized responses |
| `conversation.option.selected` | ChatHandler -> ConversationManager | User selected an A/B/C option |

Existing topics (`orchestrator.plan`, `orchestrator.schedule`, `queue.job.completed`, etc.) remain unchanged. The ConversationManager subscribes to task completion topics for specialist result handling.

## Files to Create

| File | Purpose |
|------|---------|
| `internal/conversation/manager.go` | ConversationManager struct and lifecycle |
| `internal/conversation/mediator.go` | Mediator -- structured LLM calls |
| `internal/conversation/synthesizer.go` | Synthesizer -- response formatting + options |
| `internal/conversation/state.go` | ConversationState, Option types |
| `internal/conversation/manager_test.go` | Tests |
| `internal/conversation/mediator_test.go` | Tests |
| `internal/conversation/synthesizer_test.go` | Tests |

## Files to Modify

| File | Change |
|------|--------|
| `internal/agent/handler.go` | Replace Dispatcher calls with bus publish/subscribe |
| `internal/agent/dispatcher.go` | Extract routing; dispatcher becomes specialist |
| `internal/agent/registry.go` | Add hot-reload for AGENT.md files |
| `internal/daemon/components.go` | Wire ConversationManager |
| `internal/config/schema.go` | Add ConversationConfig struct |
| `cmd/meept/main.go` | Add `agents` subcommand |
| `config/agents/dispatcher/AGENT.md` | Update dispatcher agent definition |
| `config/meept.json5` | Add conversation config template with comments |
