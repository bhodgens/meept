# Leaf p2-01 — client REST extensions + auto_approve knob

Contract P2-A in p2-master.md. OWN: internal/hermes/client.go (+test),
internal/config/schema.go HermesConfig region, config/meept.json5 [hermes]
block. Do NOT create sse.go (p2-02 owns it, running concurrently in the
same package — if you need a helper, put it in client.go with a `client`
prefix).

Tasks:
1. CreateSession; SessionKey on ChatOptions/RunOptions (X-Hermes-Session-Key
   header when non-empty); RunApproval; Jobs CRUD per contract.
2. PIN upstream shapes before coding: fetch
   https://raw.githubusercontent.com/NousResearch/hermes-agent/main/gateway/platforms/api_server.py
   (and _api_runs.py if present) via web_extract; grep the approval +
   jobs + create-session handlers for request/response field names.
   If unreachable, implement the contract's fallback shape, add
   TODO(upstream-shape) comments, and REPORT which shapes are unpinned.
3. HermesConfig.AutoApprove bool default false + DefaultConfig + template
   comment + config round-trip test.
4. Tests: httptest stubs for every new method incl. error mapping
   (429/409/401 reuse existing sentinels), SessionKey header presence/
   absence, jobs list pagination if upstream supports it.
5. Sweep: go test -p 2 ./internal/hermes/ ./internal/config/ ; both
   binaries build. Explicit-path commit:
   'feat(hermes): create-session, approval, jobs, session-key (hermes-interop p2-01)'.

Report: RED/GREEN, pinned-vs-TODO shapes, method list, files.
