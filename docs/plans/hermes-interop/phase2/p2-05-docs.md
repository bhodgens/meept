# Leaf p2-05 — docs

Contract P2-E in p2-master.md. OWN: docs/workflows/hermes.md,
docs/feature-comparison-matrix.md (one-line touch-ups only). AFTER all
code leaves — document the LANDED behavior (read the tool schema +
client).

Tasks:
1. Extend hermes.md: approve verb + auto_approve security model (default
   false, confirmation flow), streaming (wait/collect_events, stream),
   jobs verbs, capability gating, session continuity (session_key,
   create-on-demand, 409 rule). REMOVE the v1 session_id limitation note.
2. Matrix row touch-up only if now wrong.
3. go build both binaries. Explicit-path commit:
   'docs: hermes interop phase 2 (hermes-interop p2-05)'.

Report: doc sections added, files.
