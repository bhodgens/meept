# Dispatch Board — Design Proposal over the Employee Ledger

Date: 2026-11-10 · Repo: meept · Grounds: internal/ledger/{store,ops,transitions}.go, internal/contact/types.go, plan 20261109-employee-ledger (D1/D2/D3, Q2/#127). Status: **for owner ratification**.

## (a) Recommended shape (one paragraph)

Keep the ledger exactly as frozen and build a new `internal/dispatch` package whose tables live in the same `ledger.db` file (additive `CREATE TABLE IF NOT EXISTS` only — never touching `ledger`/`ledger_state`). A change event is filed as one normal ledger append: `kind='action'`, `SetStatus` drives the subject's truth, and the payload carries a `dispatch.v1` discriminator naming the change class, summary, and the **expanded recipient list** (audience rules resolved at file time, so history records who we decided to tell, not just the rule that was active that day). Filing the append and inserting the `dispatch_announcement` + one `dispatch_delivery` row per recipient happen in one transaction (same DB, single writer pool, so this is cheap). Delivery/ack state lives in `dispatch_delivery` as a *mutable execution projection*, but every delivered/acknowledged/failed transition **also appends a log-only ledger row** (`kind='action'`, source/channel-ref provenance) — so the immutable spine remains the authority and the integrity job can re-fold delivery state from it, exactly the same belt-and-braces relationship `ledger_state` has to `ledger`. "Who has been told what" is deterministic: fold `dispatch_delivery` keyed by `(announcement_id, recipient_id)`, each row pinning its announcement to a `ledger.id`. Escalation is not special machinery — an overdue ack materializes as a **commitment row owned by the dispatcher employee** (the ledger already models owed obligations), closed by a `resolution` when the dispatcher's voice call gets a verbal yes logged against the ccid.

## (b) Schemas

Ledger spine: **zero column changes.** The only frozen-territory touch is a Go-level closed-set addition *inside payloads*, not DDL: payload discriminators are versioned JSON (`{"dispatch":{"v":1,...}}`), so no guarded-ALTER is needed at all in v1. New tables (owned by `internal/dispatch`, same file, additive):

```sql
CREATE TABLE dispatch_announcement (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ledger_id     INTEGER NOT NULL UNIQUE,          -- the action row that filed it
    filed_by      TEXT NOT NULL,                    -- employee_id (secretary lane)
    subject       TEXT NOT NULL,                    -- canonical ops subject key, e.g. '12-oak'
    change_class  TEXT NOT NULL CHECK (change_class IN
                  ('material','staffing','supply','customer_sentiment','schedule','other')),
    summary       TEXT NOT NULL,
    severity      TEXT NOT NULL DEFAULT 'normal',   -- normal|urgent (drives due_at)
    filed_at      TEXT NOT NULL,                    -- RFC3339 UTC
    supersedes    INTEGER NOT NULL DEFAULT 0        -- prior announcement id on same subject
);
CREATE INDEX idx_disp_ann_subject ON dispatch_announcement(subject, id);

CREATE TABLE dispatch_delivery (
    announcement_id  INTEGER NOT NULL REFERENCES dispatch_announcement(id),
    recipient_id     TEXT NOT NULL,     -- contact.Record.ID (external) or employee id (internal)
    lane             TEXT NOT NULL CHECK (lane IN ('internal','external')),
    channel          TEXT NOT NULL,     -- 'agent_message'|'push'|contact.Channel vocab
    state            TEXT NOT NULL CHECK (state IN
                     ('pending','queued','delivered','acknowledged','failed','superseded')),
    ack_method       TEXT NOT NULL DEFAULT '',  -- 'verbal_ccid'|'reply_keyword'|'inbox_read'|'manual'
    ack_ledger_id    INTEGER NOT NULL DEFAULT 0,-- ledger row proving the ack (source_ref=ccid etc.)
    attempts         INTEGER NOT NULL DEFAULT 0,
    last_attempt_at  TEXT,
    due_at           TEXT,              -- ack deadline; NULL for non-ack-requiring channels
    escalation_ledger_id INTEGER NOT NULL DEFAULT 0, -- commitment row handed to dispatcher
    PRIMARY KEY (announcement_id, recipient_id)
);

-- the #127-lite registry: subject -> people, WITHOUT touching ledger_state
CREATE TABLE dispatch_audience (
    subject            TEXT NOT NULL,
    recipient_id       TEXT NOT NULL,
    role               TEXT NOT NULL,   -- 'site_lead'|'crew'|'owner'|'customer_contact'|'employee'
    must_know_classes  TEXT NOT NULL,   -- CSV allowlist: 'material,supply'
    preferred_channel  TEXT NOT NULL DEFAULT '',  -- '' = contact.Record.Policy fallback
    PRIMARY KEY (subject, recipient_id)
);

-- recomputed board projection (the only mutable denormalization; integrity-owned)
CREATE TABLE dispatch_board (
    subject             TEXT PRIMARY KEY,
    status              TEXT NOT NULL,  -- cache of ledger_state.status for the ops employee
    summary             TEXT NOT NULL DEFAULT '',
    open_announcements  INTEGER NOT NULL DEFAULT 0,
    unacked_count       INTEGER NOT NULL DEFAULT 0,
    overdue_count       INTEGER NOT NULL DEFAULT 0,
    last_change_at      TEXT NOT NULL,
    as_of_ledger_id     INTEGER NOT NULL,   -- fold watermark, same trick as ledger_state.as_of
    recomputed_at       TEXT NOT NULL
);
```

Tool surface (matching the ctx-stamped-identity precedent): `dispatch_file` (secretary lane; wraps one Append + expansion tx), `dispatch_execute` (dispatcher lane; claims pending rows, returns channel + script), `dispatch_record_ack`, `dispatch_board_query`. Read stays under the sanctioned-reader rule: the board RPC joins `dispatch_board` + `ledger_state`, never raw table access.

## (c) Flow: supply delay → field informed → ack tracked → escalation

```
Supplier texts delay (customer_sentiment/supply, inbound)
  │ secretary lane
  ▼
dispatch_file(subject=12-oak, class=supply, "lumber slips 2 days")
  ├─ ledger.Append(kind=action, SetStatus=waiting_on_them, payload={dispatch.v1,...})
  ├─ audience expand: dispatch_audience WHERE subject=12-oak AND class IN must_know
  │    → {Mike (site_lead, telnyx_voice), Ana (crew, signal_msg), foreman-employee (agent_message)}
  ├─ INSERT dispatch_announcement(ledger_id=<rowid>)
  └─ INSERT 3 × dispatch_delivery(state=pending, due_at=+2h)
  │ after commit: S4 storage.record.changed (Store="ledger") → board recompute wake
  ▼ dispatcher lane
dispatch_execute() → Ana: signal_msg SENT → provider receipt → state=delivered
  + log-only ledger row (source=tool, source_ref=signal id)
  foreman: agent_message inbox write → delivered; read+reply → acknowledged
  Mike: voice_call ccid=X2 … answered; verbal "got it"
  ▼
Mike answers the call — ack is NOT assumed from pickup:
  dispatcher records dispatch_record_ack(Mike, verbal_ccid=X2)
  → ledger.Append(kind=action, "Mike acknowledged supply delay", source=voice, source_ref=X2)
  → delivery.state=acknowledged, ack_ledger_id=<rowid>          ┌─ Ana never replies
  ▼                                                             │ past due_at
board: unacked_count=0…1 → integrity job scans due_at < now     ▼
  → ledger.Append(kind=commitment, employee=DISPATCHER,         "call Ana re lumber")
     "call Ana to confirm supply delay"  (escalation_ledger_id)
  → dispatcher voice_call Ana, new ccid → verbal ack → resolution row closes commitment
```

## (d) Reuse vs. new

**Reuse (don't rebuild):** Append as the single write path and provenance stamp; `ledger_state` for subject truth; `commitment`/`resolution` + `Resolves` for obligations and escalations (this is what they were *for*); the `payloadEnvelope` fold idiom and `VerifyState`/`RepairState` pattern (extend the integrity job with a `dispatch_delivery` re-fold); `Tail` + S4 `storage.record.changed` (D3 — no new topic); `internal/contact` Record + `Policy` fallback chain as the recipient/channel source of truth (don't reinvent a phone registry); voice brief → `source_ref=ccid` ack provenance; daybrief seam already shows the read-projection pattern. **Build new:** the four `dispatch_*` tables, audience expansion at file time, the escalation scheduler rule, `dispatch_execute`/`record_ack` tools, board RPC.

## (e) Open questions for the owner

1. **Subject key authority** — is `12-oak` one shared key owned by an ops-level pseudo-employee, or per-real-employee with a `dispatch_audience` alias join? (This is the #127 collision in miniature; I recommend the shared ops key below.)
2. Do **customers** ever land in `dispatch_audience` (customer_sentiment out to field is stated; field→customer pushes are not) — need an explicit disclosure policy per class?
3. `due_at` SLAs: fixed 2h, or per `change_class`/severity config?
4. Auto-ack for `agent_message` after *read* without reply — allowed, or is silence always escalation fodder?
5. Is the board **per-employee or operation-wide**? (I designed operation-wide; per-employee falls out of the same fold if wanted.)

## (f) Risks

- **Subject fragmentation**: two spellings of a jobsite split the audience join silently → mitigation: audience table is the only sanctioned name resolver; integrity job flags deliveries on subjects with no registry entry.
- **Escalation loops** (a call to get an ack spawns calls) → cap: one escalation commitment per (announcement, recipient).
- **PII in board summaries** → reuse the `+digit`-run scrub already in `ledger_brief_source.go` on the board render path.
- **Two schemas, one file**: dispatch DDL must stay strictly additive and never ALTER frozen tables, or the frozen-schema review checklist breaks.
- **Mutable `dispatch_delivery` drifts from the spine** → the ledger ack-log rows are the fold authority; delivery table is repaired like `ledger_state`.

## Rejected alternatives

- **`kind='dispatch'` in the ledger**: frozen `CHECK (kind IN ...)` can't be relaxed by guarded-ALTER — needs table rebuild of the append-only spine. Payload discriminator gets the same queryability for free.
- **Notifications as free-text subjects**: no deterministic join; "who was told what" becomes string matching.
- **Delivery state only in ledger payloads (no projection)**: every board render is an O(all-history) fold and escalation timers can't query "due_at < now" without parsing BLOBs.
- **Recompute board on read (no `dispatch_board` table)**: fine at 20 subjects, indefensible at 500 with three lanes polling; the recomputed projection matches the ratified `ledger_state` precedent.
- **New standalone dispatch store replacing the ledger**: forks the single sanctioned spine and re-solves provenance twice.
- **Treating call *pickup* as 'told'**: answered ≠ understood; ack requires verbal yes / reply keyword / recorded manual confirmation.
- **Bus announcement as the delivery record** (re-litigating D2): drop-on-full, no history, no fold.

## (5) Two-lane permission model (why this shape makes it natural)

The secretary lane **files facts**: `dispatch_file` is a thin wrapper over one Append — everything lands in the immutable log with `employee_id`=secretary, exactly the ctx-stamping precedent. The dispatcher lane **executes and confirms**: `dispatch_execute`/`record_ack` only ever mutate `dispatch_delivery` and append voice/tool-source rows with `employee_id`=dispatcher. The constitution's per-employee `tools_allowed` grants this with zero new machinery (secretary never gets `voice_call`; dispatcher's calls still ride Dialer policy gates). Because every row is attributed, the board answers *"was Mike told, by whom, when, through what, and who confirmed it"* as a single deterministic fold — which is precisely what a one-employee ledger could not say and what makes the lanes auditable instead of tribal.


---

## Owner Ratifications (2026-11-10)

- **Q1 subject authority: jobs registry with hard FK, PLUS fuzzy
  resolution.** The curated subjects/jobs table is authoritative; filing
  against an unknown or near-miss name must return a "do you mean X?"
  resolution (fuzzy match over the registry) instead of silently minting
  a new key. The canonical key exists before a dispatch is filed.
- **Q2 ack policy: EVERYTHING is relayed.** No delivered-is-enough tier
  in v1: every dispatch delivery requires acknowledged state, so every
  delivery row eventually resolves by ack or escalates to the dispatcher
  lane. The per-class/per-severity ack knob the proposal sketched is
  explicitly NOT taken; revisit only if escalation cost demands it.
- **Q3 supersession: superseded events are DISTINCT events.** A
  superseded announcement's follow-up information/confirmation obligation
  survives supersession; the successor announcement re-notifies prior
  recipients rather than silently replacing the obligation.
- **Future extension (tracked as a GitHub issue):** brief outgoing ack
  confirmations ride SMS (Telnyx Messaging), not voice calls — cheap
  per-recipient confirmation texts with reply-keyword ack; voice
  escalation remains the fallback.
- **Ledger -> dialing:** ledger-stored numbers MAY flow into the outbound
  dispatcher dial; the contacts registry serves BOTH the inbound caller
  gate and outbound recipient resolution (one system of record, two
  gates).
