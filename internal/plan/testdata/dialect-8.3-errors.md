# Plan: Billing CSV export

## Meta

- task_id: t-20260906-billing
- version: 1
- status: draft
- updated: 2026-09-06

## Goal

Export monthly billing summaries as CSV for finance review.

## Decisions

- Decision: CSV over the wire, not XLSX — Rationale: finance tooling ingests CSV directly.

## Open Questions

- Do we include credits as negative line items?

## Phases

### Phase 1: Extract billing rows

Pull the billing-period rows from the ledger store.

**Produces:**

- `billing-rows` (schema) — normalized ledger rows for one billing period

**Consumes:** none

**Steps:**

1. Query the ledger store for the billing period [code]
2. Normalize currency and tax fields [code] (needs: Phase1.S1)

### Phase 2: Render CSV

Turn normalized rows into the finance CSV.

**Produces:**

- `billing-csv` (file) — the monthly CSV export

**Consumes:**

- `payment-gateway` (interface) — charge records folded into line items

**Steps:**

1. Fold payment records into the normalized rows [code] (needs: billing-rows)
2. Write the CSV with finance header conventions [code] (needs: Phase2.S1)

## Notes

CSV encoding is UTF-8; currency is USD cents.
