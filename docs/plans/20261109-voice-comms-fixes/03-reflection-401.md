# Leaf 03 - reflection classifier 401 storm (diagnose + fix)

## Defect

Live voice turns log 12x in one call:
`reflection failed error="classifier call failed: HTTP 401: {\"error\":{...\"message\":\"Token not provided (request id: 20261109...)"`
(request id format = the CLOUD agnes gateway, not the loopback runtimes).
Call site: internal/agent/reflection_collector.go:121 (ClassifierClient ->
classifier alias). Consequences: wasted rotation time per turn, log spam,
reflection dead.

## Task (diagnose-first; code fix ONLY if a code bug; config fix = REPORT)

1. Reproduce deterministically: curl each classifier-alias member IN ORDER
   as the client would (read ~/.meept/models.json5 classifier alias -
   READ ONLY, never edit; the members as of today: prompt-router on
   :8082, local-classifier on :8143, local llama on :8080, agnes cloud).
   Which member answers 401? The request-id says agnes - so find WHY the
   local head members are skipped/failing (check their /health + a minimal
   chat completion; check disabled_providers; check the noAuth/apiKey
   gating in buildProviderConfigs). Also check whether the reflection
   client passes a session/extra header agnes rejects.
2. Root-cause report at file:line. If CODE bug (e.g. reflection builds its
   own client ignoring alias health, or a header/quota misclassification):
   fix with test. If CONFIG bug (missing key env, member unreachable):
   emit the EXACT minimal config/key change needed - the orchestrator
   applies it to ~/.meept (user-owned; you MUST NOT edit ~/.meept).
3. Regardless: reflection 401s must not spam per turn - if the fix is
   config-side, add ONE improvement: reflection_collector should log the
   first failure per conversation at Warn and Debug after (dedupe), or
   mirror the loop's existing cooldown so the alias head stops being
   retried per turn - pick the smallest conforming change with a test.

## Gates
go test -count=1 -timeout 300s ./internal/agent/ ./internal/llm/ green (if
code touched); gofmt/vet; report member-by-member curl results verbatim.
Sibling note: do NOT touch internal/agent/delegate*.go or gate_park files
(active sibling waves); reflection_collector.go + its tests only.
