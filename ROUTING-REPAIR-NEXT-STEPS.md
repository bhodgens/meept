# Routing repair handoff

Review `docs/plans/20260917-routing-repair/master.md` before authorizing implementation.

Status: repair plan prepared; five follow-up decisions recorded in master.md. The user separately approves removal of the unused veto fixture. The fixture is now removed, with focused tests and agent-package build passing. No commits occur.

The tree contains five leaves: privacy prevention, scoring integrity, corpus/cache provenance, routing guards, and acceptance.

Next action: independent review of finding coverage, file ownership, score definitions, and approval gates. Execution starts only after approval.

Historical privacy removal, remote history changes, installed artifact replacement, and commits still need separate approval. The user approves scratch-rig local model verification, null/n/a for undefined metrics, local model-content hashing, and deletion of the unused veto fixture. The requested exposure inventory is `/tmp/meept-private-exposure-inventory-20260917.md`. Blanket repair execution remains pending.

Estimated repair effort: 12-24 engineering hours, excluding live model evaluation, history repair, and independent review cycles.

The source branch changes under parallel sessions. Reconcile the current commit and working tree before dispatching any worker.
