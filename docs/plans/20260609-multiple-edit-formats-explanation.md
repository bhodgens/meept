# Multiple Edit Format Strategies - Explanation

**Created:** 2026-06-09
**Updated:** 2026-06-09 (v2 - corrected for Meept's existing patch system)
**Type:** Conceptual Explanation
**Related Plan:** `20260609-multiple-edit-formats-implementation.md`

## What Are Edit Formats?

Edit formats define **how the LLM communicates code changes** back to the system. Different formats have different trade-offs in terms of:
- **Precision**: How targeted the change is
- **Token efficiency**: How many tokens required
- **Parsing reliability**: How easily the system can extract and apply changes
- **Model compatibility**: Some models perform better with specific formats

## Why Multiple Formats Matter

Different scenarios call for different edit strategies:

| Scenario | Best Format | Why |
|----------|-------------|-----|
| Small fix in 1000-line file | Search/Replace | Only touch changed lines |
| Complete rewrite of small file | Whole file | Simpler for LLM to reason about |
| Structured API integration | Function call | Machine-parseable, reliable |
| Plan-then-execute workflow | Architect | Separate design from implementation |

---

## Important: Meept's Existing Patch System

**Meept already has a sophisticated internal patch system** via the `file_edit` tool (`internal/tools/builtin/file_edit.go`):

| Feature | Implementation |
|---------|----------------|
| **Anchored patches** | `LINE:HASH` format (e.g., `42:a3\|content`) |
| **Snapshot tags** | 4-char hex tags for cache versioning |
| **Block operations** | `replace_block`, `delete_block` resolve AST boundaries |
| **Stale anchor recovery** | 3-tier: exact → hash-only → fuzzy (Levenshtein) |
| **Session chain recovery** | Walks edit history to find matching snapshot |
| **Boundary absorption** | Auto-trims duplicate context lines |
| **Preview/accept workflow** | PendingChangesRegistry for review before apply |

**Example internal format:**
```json
{
  "path": "/app/handler.go",
  "edits": [
    {
      "op": "replace",
      "anchor": "234:f7\|var h *Handler",
      "content": "var h = &Handler{}"
    }
  ]
}
```

### The Real Gap: LLM Input Format Adapters

The issue isn't Meept's **internal** patch format (it's excellent). The gap is in **how different LLMs are prompted to produce edits**.

Currently, LLMs must output Meept's structured JSON format. But different models perform better with different prompt styles:
- **Claude**: Trained on SEARCH/REPLACE blocks in markdown
- **Local models (llama.cpp)**: Simpler with whole-file output
- **Code models**: May understand unified diff format
- **Function-calling models**: Can emit structured JSON directly

**Solution**: Build **input format adapters** that parse various LLM output styles and convert them to Meept's internal `editOp` format.

## Input Format Adapters (LLM Output Styles)

These are the **LLM output formats** we need to parse and convert to Meept's internal patch format:

### 1. **SEARCH/REPLACE Blocks** (Claude-style)

**How it works:** LLM outputs markdown blocks with SEARCH/REPLACE delimiters:
````markdown
<<<<<<< SEARCH
def calculate_total(items):
    total = 0
    for item in items:
        total += item
    return total
=======
def calculate_total(items):
    total = sum(items)
    return total
>>>>>>> REPLACE
````

**Adapter responsibility:**
1. Parse SEARCH/REPLACE blocks from markdown
2. Look up each SEARCH block's line number and hash in the current file
3. Convert to Meept's `LINE:HASH` anchor format
4. Generate internal editOp with proper snapshot tagging

**Pros:**
- Token-efficient (only changed content)
- Works with Claude models (trained on this format)
- Easy to review (diff-like)

**Cons:**
- Requires accurate text matching to find line numbers
- SEARCH block must match file content (recovery handles mismatches)

**Best for:** Claude 3.7 Sonnet, Claude 4 models, general refactoring

---

### 2. **Fenced SEARCH/REPLACE** (Markdown blocks)

**How it works:** Same as #1, but explicitly wrapped in fenced code blocks with language hint:

````markdown
```python
<<<<<<< SEARCH
def old_func():
    pass
=======
def new_func():
    pass
>>>>>>> REPLACE
```
````

**Adapter responsibility:** Same as #1, but parser first extracts fenced blocks before processing SEARCH/REPLACE delimiters.

**Pros:**
- Clearer boundaries for parsing
- Works with models trained on markdown code blocks
- Reduces ambiguity about code vs. explanation

**Cons:**
- Slightly more tokens for fencing
- Some models struggle with nested code blocks

**Best for:** Claude models, general-purpose editing

---

### 3. **Structured JSON** (Function calling / Native tool use)

**How it works:** LLM uses Meept's native tool schema directly:

```json
{
  "tool": "file_edit",
  "arguments": {
    "path": "/app/handler.go",
    "edits": [
      {
        "op": "replace",
        "anchor": "234:f7|var h *Handler",
        "content": "var h = &Handler{}"
      }
    ]
  }
}
```

**Adapter responsibility:** None - this is Meept's native format. Just validate and pass through.

**Pros:**
- Machine-parseable (no regex extraction)
- Supports full Meept features (snapshot tags, block ops)
- Type validation via JSON Schema

**Cons:**
- Requires model with function calling support
- LLM must understand `LINE:HASH` anchor format
- Token overhead for JSON structure

**Best for:** GPT-4o, GPT-5, Claude with tool use, production workflows

---

### 4. **Whole File** (Complete replacement)

**How it works:** LLM outputs the complete new file content:

````markdown
Here's the updated file:

```python
def calculate_total(items):
    total = sum(items)
    return total
```
````

**Adapter responsibility:**
1. Parse the code block from markdown
2. Read current file to get original content
3. Compute line-level diff to identify changes
4. Generate `editOp` entries for each changed section (or use single `replace` from BOF to EOF)

**Pros:**
- Simplest for LLM to understand (no anchors needed)
- No context matching required from LLM perspective
- Works with any model (no special training needed)

**Cons:**
- Token-expensive for large files
- Adapter must compute diff to generate patches
- Loses Meept's fine-grained anchor tracking

**Best for:** Small files (<100 lines), major rewrites, new files, local models (llama.cpp)

---

### 5. **Unified Diff** (Standard diff format)

**How it works:** LLM outputs standard unified diff:

```diff
--- a/src/calculator.py
+++ b/src/calculator.py
@@ -1,5 +1,4 @@
 def calculate_total(items):
-    total = 0
-    for item in items:
-        total += item
+    total = sum(items)
     return total
```

**Adapter responsibility:**
1. Parse unified diff format (---, +++, @@ hunks)
2. Map hunk line numbers to current file content
3. Compute `LINE:HASH` anchors for each change
4. Generate `editOp` entries with proper anchors

**Pros:**
- Standard format (familiar to code-trained models)
- Token-efficient
- Clear line-level changes

**Cons:**
- Some models struggle with diff syntax (`+`/`-`/`@@`)
- Line numbers may drift if file changed since context read
- Adapter must resolve anchors from line numbers

**Best for:** Code-specific models (DeepSeek Coder, StarCoder), incremental changes

---

### 6. **Architect Mode** (Plan-then-execute)

**How it works:** Two-phase approach:

**Phase 1 - Planning:**
```
Here's my plan to add OAuth2 support:

1. Create `src/validators.py` with `validate_input()` function
2. Modify `src/handler.py` to import and call `validate_input()`
3. Add tests in `tests/test_validators.py`

Files to change:
- src/validators.py (NEW)
- src/handler.py (MODIFY lines 15-30)
- tests/test_validators.py (NEW)
```

**Phase 2 - Implementation:**
LLM implements each file using any of the above formats (SEARCH/REPLACE, whole file, etc.)

**Adapter responsibility:**
1. Parse planning phase to extract file list and change summary
2. Route each implementation block to appropriate adapter
3. Coordinate multi-file edit application

**Pros:**
- Separates design from implementation
- Allows user review before changes
- Better for complex multi-file changes
- Reduces hallucination risk

**Cons:**
- Two LLM calls (higher latency, more tokens)
- Requires coordination between phases
- Plan may drift from implementation

**Best for:** Complex features, major refactoring, safety-critical changes

---

## Format Selection Matrix

| Model Type | Recommended Format | Alternative |
|------------|-------------------|-------------|
| Claude 3.7/4 Sonnet | SEARCH/REPLACE blocks | Fenced blocks |
| GPT-4o/GPT-5 | Structured JSON (native) | SEARCH/REPLACE |
| o1/o3 (reasoning) | Architect mode | SEARCH/REPLACE |
| Local (llama.cpp) | Whole file | Unified diff |
| DeepSeek Coder | Unified diff | SEARCH/REPLACE |
| Gemini | Structured JSON | Whole file |

## Implementation Architecture for Meept

### Current State

Meept has a **sophisticated internal patch system** via `file_edit` tool:
- Internal format: `editOp` with `LINE:HASH` anchors
- Operations: `replace`, `replace_block`, `insert_before`, `insert_after`, `delete`, `delete_block`
- Recovery: 3-tier stale anchor recovery (exact → hash → fuzzy)
- Preview/accept: `PendingChangesRegistry` for review workflow

**Gap:** LLMs must output Meept's structured JSON format directly. Models like Claude perform better with SEARCH/REPLACE blocks they were trained on.

### Proposed Architecture: Input Format Adapters

```
┌─────────────────────────────────────────────────────────────┐
│                    LLM Response                              │
│  (Claude outputs SEARCH/REPLACE, local model outputs file)   │
└───────────────────────┬─────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│              Format Adapter Router                           │
│   (selects adapter based on model config / user preference) │
└───────────────────────┬─────────────────────────────────────┘
                        │
        ┌───────────────┼───────────────┐
        │               │               │
        ▼               ▼               ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│ SEARCH/     │ │ Unified     │ │ Whole       │
│ REPLACE     │ │ Diff        │ │ File        │
│ Parser      │ │ Parser      │ │ Parser      │
└──────┬──────┘ └──────┬──────┘ └──────┬──────┘
       │               │               │
       └───────────────┼───────────────┘
                       │
                       ▼
       ┌───────────────────────────────┐
       │  Convert to Meept editOp[]    │
       │  - Resolve LINE:HASH anchors  │
       │  - Add snapshot tags          │
       │  - Validate patch grammar     │
       └───────────────┬───────────────┘
                       │
                       ▼
       ┌───────────────────────────────┐
       │  file_edit.Execute()          │
       │  (existing implementation)    │
       └───────────────────────────────┘
```

### Adapter Interface

```go
// EditAdapter converts LLM output to Meept's internal editOp format
type EditAdapter interface {
    // Parse extracts edits from model-specific output format
    Parse(response string, filePath string) ([]editOp, error)

    // FormatPrompt returns the prompt template for this format
    FormatPrompt() string
}

// Adapters to implement:
// - SearchReplaceAdapter (Claude-style)
// - UnifiedDiffAdapter (code models)
// - WholeFileAdapter (local models)
// - JSONAdapter (native, pass-through)
```

### Selection Logic

```go
func selectAdapter(model *ModelConfig, userPref string) EditAdapter {
    // 1. User preference always wins
    if userPref != "" {
        return parseAdapterType(userPref)
    }

    // 2. Model-based selection
    switch {
    case model.SupportsToolUse && model.ProviderID == "anthropic":
        return &JSONAdapter{}  // Claude with tool use
    case model.ProviderID == "anthropic":
        return &SearchReplaceAdapter{}  // Claude default
    case model.ProviderID == "openai":
        return &JSONAdapter{}  // GPT with function calling
    case model.IsLocal:
        return &WholeFileAdapter{}  # Simpler for local models
    case strings.Contains(model.ModelID, "coder"):
        return &UnifiedDiffAdapter{}  // Code-trained models
    default:
        return &SearchReplaceAdapter{}
    }
}
```

### Integration Points

1. **New package** (`internal/tools/builtin/adapters/`): Format adapter implementations
2. **LLM Client** (`internal/llm/client.go`): Route response through adapter before parsing
3. **Agent Orchestrator** (`internal/agent/orchestrator.go`): Adapter selection per task
4. **Config Schema** (`internal/config/schema.go`): Add `edit_format` preference per model
5. **Prompt templates** (`internal/llm/prompts/`): Format-specific prompts for each adapter

---

## Benefits for Meept

1. **Better Model Compatibility**: Let Claude use SEARCH/REPLACE, local models use whole file, GPT use JSON
2. **Improved Reliability**: Parse what each model does best, convert to Meept's robust internal format
3. **Token Efficiency**: SEARCH/REPLACE for small changes, whole file only when appropriate
4. **Leverages Existing Investment**: Adapters convert TO Meept's sophisticated `file_edit` system
5. **Flexibility**: Users can select format per model or task type

---

## Example User Workflows

### Workflow 1: Quick Bug Fix (Claude)
```bash
# User wants a quick fix
meept chat --model claude-sonnet "Fix the nil pointer in handler.go"
# System selects: SEARCH/REPLACE adapter (Claude's native format)
# Adapter parses blocks, converts to Meept editOp with LINE:HASH anchors
```

### Workflow 2: New Feature (Complex)
```bash
# User wants to add OAuth2 support
meept chat "Add OAuth2 authentication support"
# System selects: Architect mode (planning phase first)
# Then routes implementation blocks to appropriate adapters per file
```

### Workflow 3: Local Model Development
```bash
# Using local llama.cpp model
meept chat --model local/llama-3.1-8b "Refactor the database layer"
# System selects: Whole file adapter (simpler cognitive load for local models)
# Adapter computes diff, generates Meept editOps
```

### Workflow 4: Production Deployment
```bash
# High reliability required
meept chat --edit-format=json "Update the payment processor"
# System selects: JSON adapter (native Meept format, full validation)
# Direct pass-through to file_edit.Execute()
```

---

## Implementation Phases

| Phase | Description | Adapters |
|-------|-------------|----------|
| 1 | Core adapter interface + JSON pass-through | `JSONAdapter` |
| 2 | Claude-optimized SEARCH/REPLACE | `SearchReplaceAdapter` |
| 3 | Local model support | `WholeFileAdapter` |
| 4 | Code model support | `UnifiedDiffAdapter` |
| 5 | Architect mode coordination | `ArchitectAdapter` |

---

## Next Steps

See `20260609-multiple-edit-formats-implementation.md` for the detailed implementation plan.
