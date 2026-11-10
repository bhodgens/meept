# Leaf 04 - companion employee refactor (persona + read-only self-reporting)

## User directive (contract, verbatim intent)

"The companion is supposed to be someone who is helpful and knowledgeable
of what I've been working on. They start the conversation with how can I
help. They don't have access to any agent tools for the system, no ability
to read or write files, but they are able to report on the agent itself as
well as meept."

## Implementation

1. Rewrite config/employees/companion.json5 (worktree copy):
   - prompt: spoken-word desk companion. MUST open the conversation (first
     reply after the greeting) with a "How can I help?"-class offer. When
     asked what the operator has been working on / what's running, answer
     from the READ-ONLY platform tools below, in plain spoken sentences.
     No markdown, <=80 spoken words, no URLs/lists (existing rules kept).
   - tools (verified registry names, READ-ONLY subset):
     ["memory_search", "memory_recall", "memory_get_context",
      "platform_status", "platform_agents", "transcript_fetch", "task_list"]
     - NO file_*/shell/web/schedule_*/github_*/voice_call/delegate_*.
   - constitution: constraints.tools_allowed = the same list;
     tools_forbidden: ["*"] stays; NeverRules add: never read/write
     workspace files, never act on other agents' work, never promise an
     action it cannot perform (it is a reporter + conversationalist, not a
     worker); risk_ceiling low; tier_2_propose; amendment_policy stays
     requires_approval with frozen_fields [role, constraints.tools_forbidden].
   - description updated to "conversational companion with read-only
     platform visibility"; enabled stays false in template (repo ships
     disabled - consistent with secretary template).
2. Re-hire instructions for the operator install are NOT yours: report the
   `meept agents create` + update command; orchestrator applies it.
   NOTE: agents create may reject duplicate id (live install has old
   companion) - check cmd/meept/agents_cmd.go for an update path
   (agents update) and report the exact command that refreshes the live
   definition.
3. Pin: extend internal/daemon/voice_persona_test.go-style coverage OR a
   pure test that the shipped companion.json5 parses (hujson), has the
   exact tool list (no write-capable names), and its prompt contains the
   "how can I help" opener directive (table test reading the file,
   config/employees templates are already test-read somewhere? mirror that
   pattern; if none exists, add config/employees/companion_template_test.go
   in package config_test style or a small daemon test that parses the
   template file).

## Gates
go test -count=1 ./internal/daemon/ (persona tests) + new template test
green; gofmt/vet; no commits.
