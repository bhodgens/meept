# 429 / Retry-After RFC Audit — prime-agent

**Ref:** `7a52276cb17310f331f1075f28fa5cf9c4ae0a0a` (2026-10-05, "publish install.ps1 on every beta cut...", #3364)
**Repo:** `/var/folders/mf/1mvt9vbx7q3f9ynln1p79cfr0000gn/T/meept-compare/prime-agent` (shallow, 1463 tracked files, clean tree)
**Language:** Rust workspace (9 crates under `crates/`). No JS/TS runtime in the product; `crates/pa-ai/tests/differential/ts_driver.ts` is a test-only differential harness, `crates/pa-core/assets/export-html/*.js` are vendored browser bundles.

---

## Summary

prime-agent **reads** `Retry-After` from responses it received and **honors** the parsed value as a lower bound on the retry wait. It is a genuine consumer, and it has a real quota-park mechanism that feeds the daemon scheduler rather than hot-looping.

It is nonetheless `partial` against RFC 9110 §10.2.3 because of four gaps, in descending severity:

1. **The jitter is applied AFTER the server wait, so it can sleep 20% LESS than the server asked** (`provider_retry.rs:304` / `auto_retry.rs:189` / `provider_failover.rs:321`).
2. **Only IMF-fixdate is parsed.** RFC850 and asctime are rejected, both of which RFC 9110 §5.6.7 requires a recipient to accept. RFC3339 is not parsed either.
3. **Zero knowledge of `anthropic-ratelimit-*-reset`** — confirmed asymmetry against the initial recon signal. An Anthropic 429 carrying no `Retry-After` yields `retry_after_ms: None`, so the park never engages and the retry falls to a 2s computed backoff.
4. **`Retry-After` is only consulted on the 503 trace-upload path**, never on a 429 there, because 429 is deliberately excluded from `RETRIABLE_HTTP_STATUSES`.

Against the Meept baseline, prime-agent is *more* RFC-complete on delay-seconds strictness (whole-value `f64` parse, no prefix match) but *less* on date-form coverage (Meept tries RFC1123/RFC850/asctime/RFC3339; prime-agent tries IMF-fixdate only).

---

## Axis 1 — Consumer or producer?

**Consumer.** Every `Retry-After` occurrence in the tree is a *read* off an inbound response.

Two independent read sites, and **no producer site**:

| Site | Reads | Serves responses? |
|---|---|---|
| `pa-ai` provider path | inbound provider HTTP responses | no |
| `pa-core` trace upload | inbound trace-server HTTP responses | no |

Absence of a producer: `git grep -In -iE 'Retry-After' -- '*.rs'` returns 34 lines; every one is a header *lookup*, a doc comment naming the TS original, or a test. Not one is an `insert`/`append` onto an outbound response. The HTTP server in this repo is the daemon RPC/WS surface, which never sets `Retry-After`.

---

## Axis 2 — Forms parsed

There are **two separate, independently-written parsers**. Neither tries the other's date layouts.

### 2a. `pa-ai` — `crates/pa-ai/src/utils_inner/stream_failure/http_retry.rs:17-49`

```rust
pub fn parse_retry_after_ms<S: std::hash::BuildHasher>(
    headers: &std::collections::HashMap<String, String, S>,
) -> Option<u64> {
    if let Some(value) = header_value(headers, "retry-after-ms") {
        if let Ok(ms) = value.parse::<f64>() { ... return Some(ms as u64); }
    }
    let raw = header_value(headers, "retry-after")?;
    if let Ok(seconds) = raw.parse::<f64>() { ... return Some((seconds * 1000.0) as u64); }
    match parse_http_date(&raw) { ... }
}
```

`parse_http_date` (`http_retry.rs:51-85`) is a hand-rolled positional tokenizer: `parts[1]`=day, `parts[2]`=month name, `parts[3]`=year, `parts[4]`=time. That layout only matches IMF-fixdate.

### 2b. `pa-core` — `crates/pa-core/src/agent_traces/http.rs:180-194`

```rust
pub fn retry_after_delay(retry_after: Option<&str>, cap_ms: u64) -> Option<u64> {
    let value = retry_after?.trim();
    if value.is_empty() { return None; }
    if let Ok(seconds) = value.parse::<f64>() {
        if seconds.is_finite() && seconds >= 0.0 {
            let capped = (seconds * 1000.0).ceil().min(cap_ms as f64);
            return Some(capped as u64);
        }
    }
    let retry_at = parse_http_date(value)?;
    let delta = retry_at.saturating_sub(now_ms());
    Some(delta.min(cap_ms))
}
```

Its `parse_http_date` (`http.rs:198-236`) splits on the first `,` then reads day/month/year/time — also IMF-fixdate only, and it uses `parts[1].starts_with(name)` for the month (so `Nov`/`November` both match, but that is not the axis that matters here).

### Empirically verified (I extracted both functions verbatim and ran them)

| Input | `pa-ai` parse | `pa-core` (cap 2^31−1) |
|---|---|---|
| `120` | `Some(120000)` | `Some(1000)`* |
| `0` | `Some(0)` | `Some(0)` |
| `-5` | `None` | `None` |
| `1.5` | `Some(1500)` | `Some(1500)` |
| `1e3` | `Some(1000000)` | `Some(1000000)` |
| `30; foo` | `None` | `None` |
| `120 abc` | `None` | `None` |
| `Fri, 31 Dec 2027 23:59:59 GMT` | `Some(...)` ✓ | `Some(2147483647)` ✓ |
| `Friday, 31-Dec-27 23:59:59 GMT` | **`None`** | **`None`** |
| `Fri Dec 31 23:59:59 2027` | **`None`** | **`None`** |
| `2027-12-31T23:59:59Z` | **`None`** | **`None`** |
| `Sun, 06 Nov 1994 08:49:37 GMT` | `Some(0)` (past→0) | `Some(0)` |

<sub>*`pa-core` multiplies by 1000 internally; the unit is ms in both.</sub>

**Form scorecard**

| Form | Result |
|---|---|
| delay-seconds | **yes**, but parsed as `f64`, not `1*DIGIT`. **Not lax in the prefix sense** — Rust's `f64::from_str` requires the whole value, so `30; foo` and `120 abc` both fail (verified). But it over-accepts beyond RFC 9110 §7.1.3: `1.5`, `1e3`, `+30` all parse. `NaN`/`inf` parse too, but are caught by `is_finite()`. |
| IMF-fixdate | **yes**, both parsers |
| RFC850 | **no** — required by RFC 9110 §5.6.7 |
| asctime | **no** — required by RFC 9110 §5.6.7 |
| RFC3339 | **no** — compatibility miss, not a requirement |
| `retry-after-ms` | **yes**, `pa-ai` only, and it takes **precedence over** a standard `Retry-After` (checked first, `http_retry.rs:20`). Inverted vs the Meept baseline rule "a standard `Retry-After` always beats a provider header." |

---

## Axis 3 — Honored or ignored?

**`partial` — honored, but then partially undone.** The parsed value is genuinely carried end-to-end and *is* the value slept on; it is not overwritten by computed backoff. The problem is downstream.

The chain: provider response headers → `ProviderHttpError.headers` → `extract_parts_from_http` → `StreamFailureInfo.retry_after_ms` → serialized as `retryAfterMs` on the diagnostic → `provider_stream_failure_retry_after_ms` → `provider_retry_delay`.

`stream_failure.rs:780-782`:
```rust
    // An error-resolved wait (Retry-After vs resets_at maximum) overrides the
    // raw header, like the TS `err.retryAfterMs` field takes precedence.
    let retry_after_ms = error
        .retry_after_ms
        .or_else(|| parse_retry_after_ms(&error.headers));
```

`provider_retry.rs:227-239` — the server wait wins as a floor:
```rust
    if let Some(retry_after_ms) = retry_after_ms {
        if policy.max_retry_delay_ms > 0 && retry_after_ms > policy.max_retry_delay_ms {
            return ProviderRetryDelay::ExceedsCap { retry_after_ms };
        }
    }
    let exponential = policy.base_delay_ms
        .saturating_mul(2u64.saturating_pow(attempt.saturating_sub(1)))
        .min(policy.max_delay_ms);
    let delay_ms = exponential
        .max(retry_after_ms.unwrap_or(0))
        .min(MAX_TIMER_DELAY_MS);
    ProviderRetryDelay::Wait { delay_ms }
```

**Then `provider_retry.rs:304` shaves up to 20% off it:**
```rust
        let delay_ms = jittered_delay_ms(delay_ms, retry_jitter_rand01());
```
with `RETRY_JITTER_FRACTION: f64 = 0.2` (`provider_retry.rs:189`) and `jittered_delay_ms` = `((delay_ms as f64) * factor).round()` over `factor ∈ [0.8, 1.2]`.

Verified by porting the exact arithmetic:

```
Retry-After: 60000 (== cap): base_wait=60000ms  jittered(waited)=[48000,60000,72000]ms  undercut=true
Retry-After: 30000:          base_wait=30000ms  jittered(waited)=[24000,30000,36000]ms  undercut=true
```

Same defect in all three retry loops: `auto_retry.rs:189`, `provider_failover.rs:321`, and `provider_retry.rs:304` (`complete_with_provider_retry`).

This is a real RFC violation, not pedantry: a server sending `Retry-After: 60` is asserting a 60-second quota window, and the client sleeps 48 seconds and re-fires inside it. The `max()` floor is undone after the fact. The repo's own e2e test *encodes* this behavior as correct — `provider_failure_e2e.rs:672-678`:

```rust
        // The server-requested wait (Retry-After: 1s) wins over the 50ms
        // base delay; the jittered wait stays in [800, 1400]ms.
        let delay = start["delayMs"].as_u64().expect("delayMs");
        assert!(
            (800..=1400).contains(&delay),
            "Retry-After jittered delay {delay} outside [800, 1400]"
```

`[800, 1400]` is `1000ms ± 20%` — the test asserts the undercut as intended behavior. The stated rationale (`provider_retry.rs:181-188`) is fleet de-synchronization, which is legitimate, but it should jitter *upward only* on a server-directed wait, or clamp to the floor after jittering.

The repo is otherwise honest about the wait: `auto_retry.rs:230-237` emits `AutoRetryEvent::Start { delay_ms, ... }` with the **jittered** value, and the comment at `provider_retry.rs:186-188` says "The jittered value is what the caller waits AND what `auto_retry_start` reports, so the live countdown stays honest." The user-facing countdown is honest; it is just honest about a wait that is short.

---

## Axis 4 — Past / unparseable values

**Past date → clamped to 0, then floored by the base delay. No negative sleep. No hot loop.** Confirmed in both parsers:

`http_retry.rs:40-45`:
```rust
            // Epoch millis fit i64; the max(0) floor keeps the delta non-negative for the u64 wait.
            let now = now_ms() as i64;
            let wait = (date_ms - now).max(0) as u64;
```
`http.rs:192-193`:
```rust
    let delta = retry_at.saturating_sub(now_ms());
    Some(delta.min(cap_ms))
```
(`saturating_sub` on `u64` cannot go negative; a pre-1970 date is rejected outright by `http.rs:232-234`.)

- **Negative delay-seconds** (`-5`): rejected by `seconds >= 0.0` → falls through to `parse_http_date` → `None` → **fallthrough to computed backoff**. Correct.
- **Zero** (`Retry-After: 0`): accepted as `Some(0)`, then `exponential.max(0)` = the base delay (2000ms default). Not a hot loop.
- **Unparseable garbage**: `None` → falls through to computed backoff. Correct.
- **Overflow guard**: `delay_ms.min(MAX_TIMER_DELAY_MS)` where `MAX_TIMER_DELAY_MS = 2_147_483_647` (`provider_retry.rs:56`), with the comment "Node caps timers at 2^31-1 ms; longer delays overflow setTimeout and fire after ~1ms." Good — this specifically avoids the "huge delta fires after 1ms" failure mode.

This axis is the repo's strongest.

---

## Axis 5 — Provider reset headers

| Header | Supported |
|---|---|
| `retry-after-ms` | **yes** — `http_retry.rs:20`, highest precedence |
| `anthropic-ratelimit-tokens-reset` | **no** |
| `anthropic-ratelimit-requests-reset` | **no** |
| `anthropic-ratelimit-input-tokens-reset` | **no** |
| `anthropic-ratelimit-output-tokens-reset` | **no** |
| `x-ratelimit-reset-requests` | **no** |
| `x-ratelimit-reset-tokens` | **no** |
| `x-ratelimit-reset` | **no** |
| `x-should-retry` | **no** |
| Codex `resets_at` (body) | **yes** — `errors.rs:259`, a *body* field not a header |

**The asymmetry is confirmed.** Method: `git grep -In -i -F '<header>'` with **no pathspec**, i.e. all 1463 tracked files, all file types (1224 `.rs`, 85 `.md`, 51 `.py`, 29 `.jsonl`, 23 `.toml`, 19 `.json`, 8 `.yml`, 3 `.sh`, 3 `.ps1`, 3 `.js`, 1 `.ts`, plus licenses). Line counts:

```
'anthropic-ratelimit': 0 lines
'x-ratelimit':         0 lines
'x-should-retry':      0 lines
'retry-after-ms':      4 lines
'Retry-After':         34 lines
```

The repo knows the OpenAI-family millisecond extension but has zero knowledge of Anthropic's reset headers — despite shipping a hand-written Anthropic provider (`crates/pa-ai/src/providers/anthropic/`). Anthropic's 429s carry `anthropic-ratelimit-*-reset` and normally no `Retry-After`, so this is the highest-impact gap in the audit: it is the difference between parking a session for hours and re-firing a 2s ladder against a multi-hour window.

The Codex `resets_at` body path is a genuine strength and does the right precedence (`errors.rs:349-357`):
```rust
            if let Some((friendly, body_retry_after_ms)) =
                codex_usage_limit_message(&payload, Some(status))
            {
                message = friendly;
                // Neither server delay (Retry-After header, resets_at body)
                // may undercut the other.
                if let Some(body_retry_after_ms) = body_retry_after_ms {
                    retry_after_ms = Some(retry_after_ms.unwrap_or(0).max(body_retry_after_ms));
                }
```

---

## Axis 6 — 429 detection

**`status` — HTTP status code drives it.** `stream_failure.rs:604-608`:
```rust
    let rate_limit = regex::Regex::new(r"rate_limit|usage_limit|usage_not_included|throttl")
        .expect("static regex");
    if rate_limit.is_match(&type_lower) || status == Some(429) {
        return StreamFailureKind::RateLimit;
    }
```
The regex is on the provider's structured `error.type` field (a typed signal), not on free body prose, and `status == Some(429)` is an independent sufficient condition. `rate_limit` is **not** in the permanent-failure list (`provider_retry.rs:169-178`), so it retries. This is correct per the protocol's "distinguishing rate limits from other failures" test.

Note `classify_stream_failure` is fed `provider_error_type.or(Some(error.message))` at `stream_failure.rs:790-795` — so for a 429 the regex can fire on message text. Harmless: 429 alone already classifies as `RateLimit`.

---

## Axis 7 — Default attempt count

**`max_retries: 3`, and `enabled: true` — the default is a real retry loop, not 1.**

`provider_retry.rs:37-43`:
```rust
pub const DEFAULT_PROVIDER_RETRY_POLICY: ProviderRetryPolicy = ProviderRetryPolicy {
    enabled: true,
    max_retries: 3,
    base_delay_ms: 2000,
    max_retry_delay_ms: 60000,
    max_delay_ms: UNBOUNDED_BACKOFF_MS,
};
```
with the comment "TS `DEFAULT_PROVIDER_RETRY_POLICY`; also the settings defaults: `retry.enabled` true, `maxRetries` 3, `baseDelayMs` 2000, `provider.maxRetryDelayMs` 60000".

Settings resolution `settings/manager.rs:763-800` layers `.and_then(...).unwrap_or(DEFAULT_...)` over `retry.enabled` / `retry.maxRetries` / `retry.baseDelayMs` / `retry.provider.maxRetryDelayMs`, so the shipped default is 3.

Related defaults:
- Failover: `enabled: true, max_retries: 3, base_delay_ms: 1000, max_delay_ms: 30000` (`provider_failover.rs:77-82`), whole-episode ceiling `MAX_TOTAL_PROVIDER_RETRIES: u32 = 8` (`provider_failover.rs:90`) — an explicit fix for the observed "~30 attempts" complaint.
- Park: `pause_until_reset: true, max_pause_ms: 86_400_000, max_parks: 8` (`provider_park.rs:77-81`).
- Trace upload: `TRACE_UPLOAD_MAX_RETRIES: u32 = 3` (`agent_traces.rs:81`).

All default-on, all > 1. No default-off feature is load-bearing here.

---

## Axis 8 — Short-retry burn  ← the flagged axis; **no hot loop found**

This repo has the scheduler-side structure the concern predicted: a durable quota park, coalesced goal continuations, and an explicit refusal window. Provider 429 signals **feed the scheduler** rather than bypassing it.

**The park path.** When a 429's wait exceeds `max_retry_delay_ms` (default 60s), `provider_retry_delay` returns `ExceedsCap` (`provider_retry.rs:228-230`), and `auto_retry.rs:191-209` converts that into a park:
```rust
            ProviderRetryDelay::ExceedsCap { retry_after_ms } => {
                ...
                let parked = if is_quota_block_failure(&message) {
                    match park.as_deref_mut() {
                        Some(park) => park(message.clone(), &abort).await,
```
The daemon arm (`crates/pa-daemon/src/agent_engine/turn/quota.rs:34-73`) creates a **durable one-shot cron job** scheduled at the reset:
```rust
        let schedule_text = format!(
            "at {}",
            pa_core::session::manager::format_iso(resume_at_ms as i64)
        );
```
so the wake survives worker restarts and passivated sessions. Park → `resume_after_ms = reset + PROVIDER_RESUME_GRACE_MS(30_000)`, clamped to `max_pause_ms`, budgeted by `max_parks: 8`.

**The hot-loop guard that closes axis 8.** The scheduler has a goal-continuation loop with a doubling backoff, and it explicitly refuses to schedule a probe into a parked session. `goal_driver/progress.rs:148-160`:
```rust
            if provider_stream_failure_kind(turn).as_deref() == Some("rate_limit") {
                self.counted_no_progress_turn_ms = Some(turn.timestamp);
                // An earlier no-progress strike's window is CLEARED here:
                // the parked session owns the retry cadence now, and a
                // live backoff window would expose `backoff_wake_at` —
                // the daemon would schedule a 10s marker probe into the
                // parked session. The durable streak carries the strike;
                self.no_progress_backoff_until_ms = 0;
                self.parked_refusal_until_ms =
                    now_millis() + CONTINUATION_NO_PROGRESS_BACKOFF_BASE_MS;
                return Ok(false);
            }
```
reinforced at `goal_driver.rs:655-660`: "The parked window never reaches `backoff_wake_at` — no probe wake is ever scheduled for a parked session."

The comment at `progress.rs:143` names the exact failure mode this prevents: "the daemon would schedule a 10s marker probe into the parked session and keep 429ing a still-limited wallet". This is a deliberate, documented fix for short-retry burn.

**Bounded wake recovery.** If a park's wake fires and the probe reports no reset, `quota.rs:278-296` re-arms at `QUOTA_WAKE_RETRY_DELAY_MS: u64 = 60_000` bounded by `QUOTA_WAKE_MAX_RETRIES: u32 = 3` (`agent_engine.rs:132,136`) — not an unbounded 10s probe loop.

**Residual axis-8 risk.** The park requires a **reported reset**. `provider_park_decision` returns `NoParkReason::NoReset` when `reset_ms` is `None` (`provider_park.rs:146-150`) — and `quota_failure_reset_ms` is just the same `retryAfterMs` field. Combined with the axis-5 gap, an **Anthropic 429 with no `Retry-After` and no reset header produces `retry_after_ms: None` → no park → a 2s/4s/8s retry ladder and terminal give-up.** The short burn there is bounded (3 attempts, 14s total, then `is_quota_block_failure` yields no park and the turn ends) rather than a true hot loop, but it burns the start of the window instead of respecting it.

One more non-loop defect: `RETRY_UPLOAD_MAX_RETRIES` retries 429 at the trace endpoint **without** reading `Retry-After`, because 429 is excluded from the retriable set (`http.rs:265-267`):
```rust
/// The retriable HTTP statuses (TS `RETRIABLE_HTTP_STATUSES`; 429 is
/// deliberately absent — the caller reschedules instead).
pub(super) const RETRIABLE_HTTP_STATUSES: [u16; 6] = [408, 425, 500, 502, 503, 504];
```
and `upload.rs:236-241` gates the header read on `status == 503` only. So `Retry-After` is honored *only* for 503, never for 429, on the trace lane. The comment's premise (a 429 trace upload reschedules) is not implemented in this lane — `TraceUploadResult::Failed { retry_after_ms }` is produced at `upload.rs:164` and then **dropped** by the only consumer, `client_traces.rs:91-98` (`..` discards it). For a provider 429 this is moot (the provider lane reads the header); for the trace endpoint it means a 429 gets 3 immediate requests at 500ms/1s/2s.

---

## Axis 9 — Delegation chain

**None — implements directly. No LLM SDK, no spawned CLI, no gateway subprocess, no reverse proxy in the 429 path.**

Evidence, from the manifests:

`crates/pa-ai/Cargo.toml` dependency list is `pa-types`, `serde`, `serde_json`, `anyhow`, `thiserror`, `futures`, `tokio`, `tokio-util`, `reqwest 0.12` (rustls), `h2 0.4`, `bytes`, `regex`, `rand`, `base64`, `sha2`, `hmac`, `hex`, `rsa`, `url`, `tokio-tungstenite 0.28`, `http 1`. **No `openai`, `anthropic`, `async-openai`, `async-anthropic`, `google-genai`, or `genai` crate.**

`Cargo.lock` confirmation:
```
$ grep -inE '^name = "(openai|anthropic|async-openai|async-anthropic|google-genai|genai|cohere|mistralai|tower|reqwest-retry|backoff|governor)"' Cargo.lock
2337:name = "tower"
$ grep -inE 'retry|backoff|governor|circuit' Cargo.lock
(no output)
```
`tower 0.5.3` is present in the lock but is a transitive dep of reqwest/hyper and is **not referenced by a single line of repo source** (`git grep -In 'tower' -- '*.rs'` → zero hits). No `reqwest-retry`, no `backoff`, no `governor`, no circuit-breaker crate, and `git grep -iE 'tenacity|circuit.?breaker' -- '*.rs'` returns **zero hits**.

`vendor/` contains only `crossterm` (a TUI terminal library, patched for its kitty-support probe per root `Cargo.toml:15-19`).

The comment at `pa-ai/src/utils_inner/http.rs:1-7` documents the hand-rolled design explicitly:
> "Providers in the TS reference go through SDK clients configured with `maxRetries: 0`; here each provider issues one streaming HTTP request through a shared `reqwest` client."

This is why the audit is tractable: **every line of 429 behavior in this repo is first-party Rust that I read directly.** There is no vendored retry layer whose version I would need to pin. (The Rust `chrono`/`httpdate` crates are absent too — hence the two hand-written date parsers, and hence the missing RFC850/asctime support: a stock `httpdate` crate would have handled both.)

Both parsers are reached for Anthropic as well — `anthropic/stream.rs:267-274` funnels a 4xx/5xx into `ProviderError::from_http_status_body(response.status, &body, response.headers.clone())`, which carries `headers` (`stream_failure.rs:458-478`) into `extract_parts_from_http` → `parse_retry_after_ms`. So the Anthropic provider *does* read `Retry-After`; it just doesn't know the reset headers.

---

## Findings, ranked

**F1 (High) — Jitter sleeps less than the server asked.** `provider_retry.rs:304`, `auto_retry.rs:189`, `provider_failover.rs:321`. A `Retry-After: 60` can be slept as 48s. The `max()` floor at `provider_retry.rs:236-237` is correct; the jitter after it is not. The e2e test at `provider_failure_e2e.rs:672-678` asserts the undercut. Fix: clamp after jitter, or jitter upward-only when the wait came from a server header.

**F2 (High) — No `anthropic-ratelimit-*-reset`.** 0 hits across all 1463 tracked files. An Anthropic 429 without `Retry-After` gets no park and a 2s/4s/8s ladder against a multi-hour window. Highest real-world impact given the repo ships its own Anthropic provider.

**F3 (Medium) — RFC850 and asctime not parsed.** RFC 9110 §5.6.7 requires recipients to accept both. `http_retry.rs:51-85` and `http.rs:198-236` are IMF-fixdate-only. Meept's baseline tries all four. RFC3339 also unsupported (a compat miss, per the protocol).

**F4 (Medium) — Trace lane reads `Retry-After` on 503 only, and drops the parsed value.** `http.rs:267` excludes 429; `upload.rs:236-241` gates on 503; `upload.rs:164` computes `retry_after_ms` into `TraceUploadResult::Failed` and `client_traces.rs:91-98` discards it with `..`.

**F5 (Low) — delay-seconds parsed as `f64`, over-accepting.** `1.5`, `1e3`, `+30` all accepted where RFC 9110 §7.1.3 specifies `1*DIGIT`. Not a prefix-match bug (whole-value parse, verified), and negative/garbage correctly rejected.

**F6 (Low) — `retry-after-ms` outranks standard `Retry-After`.** `http_retry.rs:20` checks it first and returns early. Inverted vs the "standard header always wins" rule.

---

## Enumeration gaps

- `git grep` covers tracked files only; the tree is clean (`git status --porcelain` empty), so no untracked source was missed.
- I read the two `Retry-After` parsers, their HTTP-response plumbing, and every consumer of `retry_after_ms` in full. I did **not** read the bodies of `provider_failover.rs` beyond the regions quoted (lines 1-365 of ~700) or `provider_retry.rs` beyond line 310 of 977; both are non-test control flow in the regions that set the defaults and the delay decision, which are the parts this audit turns on.
- Absence claims for `anthropic-ratelimit-*`, `x-ratelimit-*`, `x-should-retry`, `tenacity`, and `circuit.?breaker` are exhaustive over all tracked files and all file types, as enumerated above.
- No live provider was contacted; all behavior claims come from source plus two verbatim-extracted parser harnesses I compiled and ran.