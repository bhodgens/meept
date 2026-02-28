# OpenFang Feature Adoption Plan

## Overview

This plan documents features from OpenFang (a Rust-based Agent OS) being adopted into Meept (a Go-based multi-agent daemon). The focus is on high-impact features that complement Meept's existing strengths (MCP integration, learning pipeline, knowledge graph).

**Status:** Active implementation

**Analysis source:** `/tmp/openfang_analysis/openfang/`

---

## Features to Implement

### 1. Vector Embeddings for Semantic Memory ⚠️ HIGH PRIORITY

**Status:** Implementation started

**What OpenFang has:**
- Semantic memory with vector similarity search
- Hybrid storage: SQLite + embeddings (Qdrant optional)
- Semantic search alongside keyword search

**Why it benefits Meept:**
- Better memory recall without exact keyword matches
- Context retrieval for current tasks
- Enhanced knowledge graph with semantic linking

**Implementation plan:**

1. **Create `internal/memory/vector/` package**
   - `embedding.go`: Embedding generation interface
     - OpenAI API support (`text-embedding-3-small`)
     - Ollama local model support (`nomic-embed-text`)
   - `store.go`: Vector storage with SQLite backend
     - Cosine similarity search
     - Embedding cache
   - `hybrid.go`: Combined keyword + vector search

2. **Integrate with memory manager**
   - Add `SearchSemantic()` method to Manager
   - Add `SearchHybrid()` for blended results
   - Update config to support embedding provider settings

**Files:**
- `internal/memory/vector/embedding.go` ✓ created
- `internal/memory/vector/store.go` ✓ created
- `internal/memory/vector/hybrid.go` ✓ created
- `internal/memory/manager.go` - to be extended
- `internal/config/schema.go` - add EmbeddingConfig

**Configuration:**
```toml
[memory]
backend = "memvid"  # or "sqlite"

[memory.embeddings]
enabled = true
provider = "openai"  # or "ollama"
model = "text-embedding-3-small"
api_key = "${OPENAI_API_KEY}"  # optional, uses env var
dimension = 1536
```

---

### 2. Taint Tracking for Information Flow Security ⚠️ HIGH PRIORITY

**Status:** Pending

**What OpenFang has:**
- Taint labels propagate through execution
- Blocks secrets from exfiltration via `net_fetch`
- Blocks injection commands in `shell_exec`
- Suspicious pattern detection (e.g., `curl | bash` with tainted data)

**Why it benefits Meept:**
- Prevents data exfiltration (user secrets can't leak)
- Prevents prompt injection (user input can't trigger dangerous commands)
- Audit trail for data flow

**Implementation plan:**

1. **Create `internal/security/taint/` package**
   - `taint.go`: Taint label types
     ```go
     type TaintLabel string
     const (
         TaintNone TaintLabel = ""
         TaintUserInput TaintLabel = "user_input"
         TaintSecret TaintLabel = "secret"
         TaintUntrusted TaintLabel = "untrusted"
     )
     ```
   - `tracker.go`: Track taint propagation through tool calls
   - `patterns.go`: Suspicious pattern detection

2. **Extend executor**
   - Add taint tracking to tool inputs
   - Check taint before `shell_exec` and `web_fetch`
   - Block suspicious patterns with tainted data

**Suspicious patterns from OpenFang:**
```go
const SUSPICIOUS_PATTERNS = []string{
    "curl ", "| sh", "| bash", "base64 -d",
    "$(curl", "`curl", "eval ",
}
```

**Files to create:**
- `internal/security/taint/taint.go`
- `internal/security/taint/tracker.go`
- `internal/security/taint/patterns.go`

**Files to modify:**
- `internal/agent/executor.go`
- `internal/security/engine.go`
- `internal/security/types.go`

---

### 3. Extended Thinking Mode (Claude Native) ⚠️ MEDIUM PRIORITY

**Status:** Pending

**What OpenFang has:**
- Native Anthropic driver with extended thinking support
- ThinkingDelta stream events
- Separate thinking phase before response

**Why it benefits Meept:**
- Better reasoning through complex problems
- Transparency into model's thought process
- Reduced hallucination

**Implementation plan:**

1. **Create `internal/llm/anthropic.go`**
   - Native Anthropic API client (not OpenAI-compatible wrapper)
   - Support for extended thinking API
   - Handle thinking_delta stream events

2. **Extend interfaces**
   - Add thinking events to ProgressCallback
   - Extend Chatter interface for thinking mode

3. **Update models configuration**
   - Add Anthropic-specific models
   - Mark models with `extended_thinking` capability

**Files to create:**
- `internal/llm/anthropic.go`

**Files to modify:**
- `internal/llm/interface.go`
- `internal/llm/models.go`
- `internal/llm/provider_manager.go`
- `internal/llm/resolver.go`

---

## Features NOT Implementing (Out of Scope)

| Feature | Reason |
|---------|--------|
| **Media Tools** | Meept has no multimodal AI vision support. Low priority unless needed. |
| **Process Tools** | Synchronous shell execution sufficient for current use cases. |
| **WASM Sandbox** | Meept uses subprocess isolation; Tirith provides pre-execution scanning. |
| **Merkle Audit Trail** | Current SQLite audit log is sufficient for Meept's threat model. |
| **RBAC Per-Agent** | Current security model is user-focused. Can be added later if needed. |

---

## Features Requiring More Investigation

### Workflow Engine

**OpenFang's approach:** Declarative multi-agent pipelines with step modes (Sequential, FanOut, Conditional, Loop).

**Questions to resolve:**
1. Format preference: TOML, JSON, or YAML?
2. Storage location: `~/.meept/workflows/`?
3. Triggering mechanism: Intent-based or explicit tool?
4. Variable system complexity?

**Deferral reason:** Design depends on how agent coordination evolves after implementing core features.

### Autonomous Agents (Hands)

**OpenFang's approach:** Pre-packaged autonomous agents with HAND.toml format, multi-phase playbooks, 24/7 operation.

**Questions to resolve:**
1. Does Meept need 24/7 autonomous operation?
2. What's the use case for agent packaging vs configuration?
3. How does this differ from the current agent specification system?

**Deferral reason:** Current agent system is sufficient. Revisit after evaluating real-world usage patterns.

### SSE/WebSocket Streaming

**OpenFang's approach:** 140+ REST/WS/SSE endpoints with token-level progress reporting.

**Questions to resolve:**
1. Is this for a planned web UI?
2. Does the current ProgressCallback interface suffice for CLI usage?
3. What's the priority for web-based interaction?

**Deferral reason:** Current Unix socket JSON-RPC works for CLI. Web UI requirements unclear.

---

### 4. Knowledge Graph Tools ⚠️ LOW PRIORITY (Quick Win)

**Status:** Pending

**What OpenFang has:**
- `entity_create` - Create knowledge graph entities
- `entity_link` - Link entities with relations
- `entity_query` - Query knowledge graph

**Meept overlap:** Meept has `internal/memory/graph.go` with PageRank, community detection, and edge tracking. No agent-facing tools exist.

**Implementation:** Thin wrappers around existing graph functionality.

**Files to create:**
- `internal/tools/builtin/tool_entity_create.go`
- `internal/tools/builtin/tool_entity_link.go`
- `internal/tools/builtin/tool_entity_query.go`

---

### 5. Scheduling Tools ⚠️ LOW PRIORITY (Quick Win)

**Status:** Pending

**What OpenFang has:**
- `cron_create` - Create cron-style scheduled tasks
- `schedule_create` - Create one-time scheduled tasks
- `schedule_list` - List scheduled tasks
- `schedule_delete` - Delete scheduled tasks

**Meept overlap:** Meept has `internal/scheduler/` package with jobs and cron support. No agent-facing tools exist.

**Implementation:** Thin wrappers around existing scheduler package.

**Files to create:**
- `internal/tools/builtin/tool_schedule_create.go`
- `internal/tools/builtin/tool_schedule_list.go`
- `internal/tools/builtin/tool_schedule_delete.go`
- `internal/tools/builtin/tool_cron_create.go`

---

### 6. Web Search Tool ⚠️ LOW PRIORITY

**Status:** Pending

**What OpenFang has:**
- `search_web` - Web search via DuckDuckGo

**Meept overlap:** None. Meept has `web_fetch` for URL retrieval but no search capability.

**Implementation:** New tool using DuckDuckGo HTML API or similar.

**Files to create:**
- `internal/tools/builtin/tool_web_search.go`

---

## Implementation Order

1. **Vector Embeddings** - Foundation for improved memory
2. **Taint Tracking** - Security enhancement
3. **Extended Thinking** - Quality improvement
4. **Knowledge Graph Tools** - Quick win, expose existing functionality
5. **Scheduling Tools** - Quick win, expose existing functionality
6. **Web Search Tool** - New capability

---

## Reference: What Meept Does Better (Keep These)

| Feature | Meept Advantage |
|---------|-----------------|
| **MCP Protocol Support** | First-class integration vs secondary in OpenFang |
| **Agent Coworker Awareness** | Agents discover each other via `platform_agents` |
| **Learning Pipeline** | JUDGE/DISTILL/CONSOLIDATE trajectory learning |
| **ClawSkills Marketplace** | Third-party skill marketplace |
| **Self-Improvement System** | Automated code fixing |
| **Knowledge Graph** | PageRank + community detection |
| **Personality Memory** | Evolving personality from conversations |

---

## OpenFang Reference Files

- `/tmp/openfang_analysis/openfang/crates/openfang-memory/src/semantic.rs` (Vectors)
- `/tmp/openfang_analysis/openfang/crates/openfang-types/src/taint.rs` (Taint)
- `/tmp/openfang_analysis/openfang/crates/openfang-runtime/src/tool_runner.rs` (Taint checks)
- `/tmp/openfang_analysis/openfang/crates/openfang-runtime/src/drivers/anthropic.rs` (Extended thinking)
- `/tmp/openfang_analysis/openfang/crates/openfang-kernel/src/workflow.rs` (Workflows)
