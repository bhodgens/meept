# 429 / Retry-After RFC Audit — Hermes Agent

- **Repo:** `/var/folders/mf/1mvt9vbx7q3f9ynln1p79cfr0000gn/T/meept-compare/hermes`
- **Ref:** `git rev-parse HEAD` = `88c60858468d7adee27a752242c7c507fa4129d0` (HEAD, shallow clone, 17464 tracked files)
- **Scope:** read-only audit, nothing modified.
- **Audit date:** session date.

---

## 0. Triage: which of the 232 `retry-after` hits are the consumer path

`git grep -l -i -E 'retry[-_]?after'` → **232 files**; `git grep -l -E '429'` → **474 files**. Bucketed by
`git grep -l ... | sed 's|/[^/]*$||' | awk -F/ '{print $1"/"$2}' | sort | uniq -c | sort -rn`:

| Bucket | Files | Role | Read? |
|---|---|---|---|
| `agent/` | 13 | **LLM client path — primary consumer** | **read in full** |
| `hermes_cli/` | 12 | Auth/billing/observability HTTP clients — secondary consumers | read |
| `tools/` | 10 | Tool connectors (fal, Graph, MCP) — secondary consumers | read |
| `gateway/` | 6 | Messaging platform **server** (producer of `Retry-After` on its own 429s) | read |
| `pm/` | 1 | Plugin-market install/download HTTP client — consumer | read |
| `plugins/spotify` | 1 | Consumer (message-only, no sleep) | read |
| `tui_gateway/`, `cron/`, `hermes_state_schema.py/`, `evals/`, `locales/` | 10 | Mostly field names / copy strings / billing views | grep-sampled |
| `apps/desktop`, `ui-tui/src`, `website/`, `optional-skills/`, `scripts/`, `cli-config.yaml.example` | ~180 | **Excluded** — TS desktop UI, docs/i18n prose, and a SKILL.md *example snippet* (`website/.../rest-graphql-debug.md:296 wait = int(resp.headers.get("Retry-After", 2 ** attempt))`) that is documentation of a pattern, not repo runtime. `optional-skills/` bundles are separately vendored playbook text, not the agent's own LLM path. |

**Producer vs consumer, explicitly:** `gateway/` sets the header on responses it *serves*
(`gateway/platforms/api_server.py:1346`, `:2876`, `:4054`: `headers={"Retry-After": "1"}`,
`headers={"Retry-After": str(60)}` alongside `code="rate_limit_exceeded"`). That is a **producer**.
The mirror-image consumer is `agent/` + the auth/tool clients, verified below.

**Absence-claim method (for `retry-after-ms` / `x-should-retry`):** enumerated the module set
`agent/` (13 retry-after files, all read), plus repo-wide `git grep -c -i 'retry-after-ms'` and
`git grep -c -i 'x-should-retry'` and `git grep -n -E 'anthropic-ratelimit-(input|output)-tokens-reset'`
— **all three returned rc=1, zero hits** across all 17464 tracked files. The only `retryAfterMs`
matches are Hermes' own *body* field (`tools/connectors/gateway/errors.py:85`), never a header.

---

## 1. Consumer or producer?

**Consumer, decisively, for the LLM path — and it implements the retry itself.**

The single parser choke point is `agent/retry_utils.py:30`:

```python
def parse_retry_after_seconds(value_or_headers: Any) -> Optional[float]:
    """Parse a ``Retry-After`` value (numeric / HTTP-date) or a headers mapping (both casings tried) into
    seconds, clamped at 0.0; None when absent / unparseable."""
```

It accepts a headers mapping and tries both casings (`retry_utils.py:39-41`):

```python
            raw = getter("Retry-After")
            if raw is None:
                raw = getter("retry-after")
```

Verified against a duck-typed httpx-like header object (`.get` returning a value) → `77.0`, and against
`{'Retry-After': '45'}` and `{'retry-after': '45'}` → `45.0` each.

The value is read off a **real provider error's response headers**, not off a locally computed timer:
`agent/turn_recovery.py:1428-1430`

```python
    _retry_after = parse_retry_after_seconds(
        getattr(getattr(api_error, "response", None), "headers", None)
    )
```

with a body-field fallback at `turn_recovery.py:1431-1438` for structured `retry_after`
problem-detail bodies (top-level or nested under `error`).

---

## 2. Forms parsed — measured, not assumed

I executed the repo's real parser (`agent/retry_utils.py` loaded directly via importlib, Python 3.14.7)
against every RFC form plus adversarial inputs. **Raw results:**

```
delay-seconds    '30'                            -> 30.0
lax float        '30.7'                          -> 30.7
sci notation     '1e3'                           -> 1000.0
whitespace       '  30  '                        -> 30.0
junk suffix      '30; foo'                       -> None
plus sign        '+30'                           -> 30.0
infinity         'inf'                           -> inf          <-- NOT rejected
nan              'nan'                           -> 0.0
IMF-fixdate      'Fri, 31 Dec 2027 23:59:59 GMT' -> 39037837.2   (delta-seconds)
RFC850           'Friday, 31-Dec-27 23:59:59 GMT'-> 39037837.2   (delta-seconds)
asctime          'Fri Dec 31 23:59:59 2027'      -> 39037837.2   (delta-seconds)
RFC3339 Z        '2027-12-31T23:59:59Z'          -> None         <-- NOT parsed
RFC3339 offset   '2027-12-31T23:59:59+00:00'     -> None         <-- NOT parsed
ms-as-seconds    '30000'                         -> 30000.0
negative         '-5'                            -> 0.0
zero             '0'                             -> 0.0
past IMF date    'Fri, 31 Dec 1999 23:59:59 GMT' -> 0.0
garbage          'soon'                          -> None
empty            ''                              -> None
bool True        True                            -> None
```

- **delay-seconds: yes, and it is LAX.** `retry_utils.py:52` is
  `return max(0.0, float(text))` — `float()`, not `int()`. It is *not* a prefix match
  (`'30; foo'` → `None`, so `fmt.Sscanf`-style truncation is absent), but it accepts floats
  (`30.7`), scientific notation (`1e3` → 1000), and `inf`. `max(0.0, …)` absorbs negative and
  zero. Deliberate contrast with the meept baseline, which uses strict `strconv.Atoi`
  specifically to avoid reading a date as a huge delta.
- **IMF-fixdate / RFC850 / asctime: yes, all three.** They go through
  `email.utils.parsedate_to_datetime` (`retry_utils.py:57`), which is RFC 9110 §5.6.7
  compliant including the required obsolete asctime/RFC850 recipients. `when.tzinfo is None`
  is coerced to UTC (`retry_utils.py:62-63`), and the result is delta-ised and clamped
  (`retry_utils.py:64`: `return max(0.0, (when - datetime.now(timezone.utc)).total_seconds())`).
- **RFC3339: NO.** Both `…Z` and `+00:00` forms return `None`. `parsedate_to_datetime` does not
  accept ISO-8601, and there is no fallback to `datetime.fromisoformat` in this parser.
  This is the compatibility gap the protocol calls out — and it matters in practice, because
  Anthropic's own rate-limit signal ships as RFC3339. Hermes handles that separately
  (§5) but *not* when the sender puts the ISO value in `Retry-After` itself.
- **`retry-after-ms`: no.** Zero occurrences of the header repo-wide.

**The one genuinely dangerous laxness:** `'inf' → inf`. `max(0.0, float('inf'))` is `inf`.
Downstream this is capped (§3), so it cannot hang the daemon — but it is a latent
`math.isfinite` omission. `nan` is benign (`max(0.0, nan)` → `0.0`).

---

## 3. Honored or ignored?

**HONORED — the parsed value is the actual sleep, with a documented cap (so: `partial` in the
protocol's "capped" sense, but never discarded).**

`agent/turn_recovery.py:1439-1449` — `compute_error_backoff`:

```python
    if _retry_after is not None:
        # Cap at 10 minutes. Anthropic Tier 1 input-token buckets reset in ~171s, so a 120s cap
        # caused us to retry before the actual reset window and re-trip the limit. 600s covers all
        # realistic provider reset windows while still rejecting pathological values. (#26293)
        _retry_after = min(_retry_after, 600)
        if _retry_after <= 0:
            # A zero/expired cooldown (retry-after: 0, or an HTTP-date in the
            # past, which the parser clamps to 0.0) carries no usable wait —
            # treat it as absent so we never hot-loop the provider.
            _retry_after = None
    wait_time = _retry_after if _retry_after is not None else jittered_backoff(retry_count, base_delay=2.0, max_delay=60.0)
```

So: the parser's output **replaces** computed backoff (`wait_time = _retry_after if …`), it is
capped at 600 s, and the adaptive Z.AI policy is explicitly gated off when a header exists
(`turn_recovery.py:1452`: `if _adaptive and _retry_after is None:`). A parser whose result is
overwritten by exponential backoff would be a fail here — it is not.

Critically, Retry-After is honored **for every retryable provider error, not just 429**
(`turn_recovery.py:1424-1427`: "Respect Retry-After on every retryable provider error, not just
429s… ignoring either turns an origin outage into a retry storm").

The return value is actually slept. `agent/turn_api_error.py:399-412`:

```python
    wait_time = compute_error_backoff(
        agent, api_error, retry_count=retry_count, max_retries=max_retries,
        is_rate_limited=is_rate_limited, is_zai_coding_overload=_is_zai_coding_overload,
        base_url=_base, model=_model,
    )
    …
    _interrupted = interruptible_backoff_sleep(
        agent, wait_time, _retry, messages=messages, …
```

and `interruptible_backoff_sleep` (`agent/turn_recovery.py:1372-1383`) sleeps the value in
200 ms slices while honouring interrupts:

```python
    sleep_end = time.time() + wait_time
    _touch_counter = 0
    while time.time() < sleep_end:
        …
        time.sleep(0.2)
```

`compute_error_backoff` has exactly one non-test caller (`turn_api_error.py:399`), so the
chain parser → cap → sleep is unbroken and single-path.

---

## 4. Past / unparseable values

**Clamp, then fall through to computed backoff. No negative sleep, no hot loop.**

Three independent layers, all verified:

1. **Parser clamps:** `retry_utils.py:47` `max(0.0, float(raw))`, `:52` `max(0.0, float(text))`,
   `:64` `max(0.0, (when - now).total_seconds())`. Measured: past IMF date → `0.0`, `-5` → `0.0`.
2. **Caller discards non-positive:** `turn_recovery.py:1444-1448` turns `<= 0` into `None`, with
   the reason in the comment: "so we never hot-loop the provider". Then line 1449 falls back to
   `jittered_backoff(retry_count, base_delay=2.0, max_delay=60.0)` — a real wait, not zero.
3. **Auto-recovery ladder also refuses non-positive:** `turn_recovery_autorecover.py:58`
   `return value if value is not None and value > 0 else None`, and caps honored waits at 120 s
   (`turn_recovery_autorecover.py:31` `_RETRY_AFTER_CAP_S = 120.0`, applied line 67).

Test coverage confirms the intent — `tests/agent/test_retry_utils.py:154-155`:

```python
        past = datetime.now(timezone.utc) - timedelta(seconds=90)
        assert parse_retry_after_seconds(format_datetime(past, usegmt=True)) == 0.0
```

Garbage (`'soon'`) → `None` → computed backoff. Booleans are excluded explicitly
(`retry_utils.py:44`: `if raw is None or isinstance(raw, bool): return None`), so a JSON
`true` never becomes 1 second.

---

## 5. Provider reset headers

| Header | Consumed? | Evidence |
|---|---|---|
| `Retry-After` (both casings) | **yes, primary** | `agent/retry_utils.py:39-41` |
| `x-ratelimit-reset` | yes (secondary) | `agent/agent_runtime_helpers.py:3797` `ratelimit_reset = headers.get("x-ratelimit-reset")` |
| `x-ratelimit-reset-requests` | yes (secondary) | `agent/agent_runtime_helpers.py:3739` |
| `x-ratelimit-reset-tokens` | yes (secondary) | `agent/agent_runtime_helpers.py:3739` |
| `anthropic-ratelimit-requests-reset` | yes (secondary) | `agent/agent_runtime_helpers.py:3740` |
| `anthropic-ratelimit-tokens-reset` | yes (secondary) | `agent/agent_runtime_helpers.py:3740` |
| `x-ratelimit-reset-requests-1h` | yes (Nous) | `agent/nous_rate_guard.py:52` |
| `anthropic-ratelimit-input-tokens-reset` | **no** | `git grep -E 'anthropic-ratelimit-(input\|output)-tokens-reset'` → rc=1, 0 hits |
| `anthropic-ratelimit-output-tokens-reset` | **no** | same grep, 0 hits |
| `retry-after-ms` | **no** | `git grep -c -i 'retry-after-ms'` → rc=1, 0 hits |
| `x-should-retry` | **no** | `git grep -c -i 'x-should-retry'` → rc=1, 0 hits |

The vendor set is handled by `_set_reset_from_vendor_headers`
(`agent/agent_runtime_helpers.py:3758-3769`), which tries OpenAI's **duration** grammar
("6m0s", "1.5s", "1h2m3s" — `_DURATION_COMPONENT_RE` at `:3733`, validated strictly by the
`"".join(n + u for n, u in parts) != raw` round-trip at `:3753`) and *then* falls back to
`_parse_absolute_timestamp`, which does handle ISO-8601:

```python
agent/credential_pool.py:421 —        return datetime.fromisoformat(raw.replace("Z", "+00:00")).timestamp()
```

So **Anthropic's RFC3339 `*-reset` headers are parsed** (compatibility win) — while a raw
RFC3339 in `Retry-After` is not (§2). Only `input`/`output`-tokens-reset variants are missing.

Note the vendor headers feed `context["reset_at"]` only (copy + credential-pool TTL), and are
**lower priority than `Retry-After`** (`agent_runtime_helpers.py:3725`: `if "reset_at" in context: return`).

---

## 6. 429 detection

**Status-code dispatch first, with body-regex only as a tiebreaker/disambiguator. `mixed`, leaning status.**

Status is read off the typed SDK exception (`agent/turn_api_error.py:89`):
`status_code = getattr(api_error, "status_code", None)`, then `classify_api_error`
(`turn_api_error.py:111-117`) routes on it. The 429 handler is a dedicated function
`agent/error_classifier.py:1049` `_status_429(c: _Ctx)`, returning typed `FailoverReason`
values from a typed enum (`error_classifier.py:39-40`: `rate_limit = "rate_limit"`,
`upstream_rate_limit = "upstream_rate_limit"`).

Body regex exists but only *within* the 429 handler and only to disambiguate sub-kinds, e.g.
`error_classifier.py:1068-1073`:

```python
    quota_wall = c.code == "usage_limit_reached" or any(
        p in c.msg for p in ("usage_limit_reached",) + _USAGE_LIMIT_PATTERNS + _BILLING_PATTERNS
    )
    explicit_rate_limit = any(p in c.msg for p in _RATE_LIMIT_PATTERNS)
```

There is also a typed-error escape hatch when the SDK omits the status
(`error_classifier.py:979-981`): `if status_code is None and type(error).__name__ == "RateLimitError": status_code = 429`.

**The "429 rotates instead of waiting" bug from the protocol's list is explicitly handled.** The
free-tier guard is documented in the classifier docstring (`error_classifier.py:789`):
"capacity (… `rate_limited`: honour `retry_after`, never rotate the free tier's only credential)",
implemented at `error_classifier.py:811-812`:

```python
        if refusal["retry_after"] > 0:
            ctx["reset_at"] = time.time() + refusal["retry_after"]
```

---

## 7. Default attempt count

**Default `api_max_retries = 3`.**

- Set in `hermes_cli/config_defaults.py:133`: `"api_max_retries": 3,`
- Read at `agent/agent_init.py:1467`: `_api_retries = max(int(_agent_section.get("api_max_retries", 3)), 1)`
- Stored `agent/agent_init.py:1470`: `agent._api_max_retries = _api_retries`
- Loop bound `agent/conversation_loop.py:1506`: `while s.retry_count < s.max_retries:`, armed at
  `:1650`: `s.api_start_time, s.retry_count, s.max_retries = time.time(), 0, agent._api_max_retries`

So 1 initial attempt + 3 retries by default — a real retry loop, **not** the "default 1 = no retry"
trap. One documented *widening*: Z.AI Coding overload 429s raise the ceiling
(`turn_recovery.py:1902-1903`:
`max_retries = max(max_retries, zai_coding_overload_retry_ceiling())` → 3+4+1 = 8).

An additional bounded ladder exists but is **default-off**: `auto_recovery_cycles`
(`turn_recovery_autorecover.py:44-46`) reads `agent.auto_recovery_cycles` and `ladder_eligible`
(`:74`) returns False when `<= 0` — and the config default is absent, i.e. 0.

---

## 8. Short-retry burn

**No immediate re-fire on the same window in the main loop; the wait is the honored Retry-After.**

Every retry goes through `compute_error_backoff` → `interruptible_backoff_sleep`, so a 429 cannot
be re-fired before its own stated window (up to the 600 s cap, §3).

Two guards deserve credit because they are exactly this bug class:

1. **Credential-pool rotation no-recovery guard** (`agent/credential_pool.py:2358-2368`):
   > "rotation returned the just-marked entry … Reporting it reports a successful rotation without
   > changing the credential, so the caller retries the same 429 forever (~2 req/s for hours)."

2. **`fallback.min_switch_reset_seconds`** (`agent/fallback_cooldown.py:24-42`) — an explicit
   opt-in to *not* switch provider when the reset window is short. **This is DEFAULT OFF**:
   `hermes_cli/config_defaults.py:40` `"fallback": {"min_switch_reset_seconds": 0}`, and
   `fallback_cooldown.py:36` `if threshold <= 0: return False`. So **by default a 429 does rotate
   to the fallback provider immediately** rather than waiting out the window — a deliberate
   product decision (comment `config_defaults.py:37-39`: "stay on it (the retry backoff rides out
   the window) instead of switching the turn to a fallback model"), and the reset window is still
   preserved for the *returning* provider via `_arm_rate_limit_cooldown`
   (`fallback_cooldown.py:64-71`, `backoff_seconds = math.ceil(provider_delay)`).

Credential cooldown TTLs, for the "does a 429 get parked" question
(`agent/credential_pool.py:131-139`): 401 → 5 min; 429 → 1 h
(`EXHAUSTED_TTL_429_SECONDS = 60 * 60`); **sole credential → 60 s**
(`EXHAUSTED_TTL_SOLE_CREDENTIAL_SECONDS = 60`, applied at `:396-398`).

---

## 9. Delegation chain — the decisive axis

**Hermes deliberately takes the retry itself and disables every SDK's internal retry loop.**
This is the opposite of the claude-flow pattern, where the repo passed 429 handling down to the
Anthropic TS SDK.

Every client-construction chokepoint sets `max_retries: 0`, with the reason spelled out in-repo:

| Site | Evidence |
|---|---|
| Anthropic adapter | `agent/anthropic_adapter.py:380` — `kwargs: Dict[str, Any] = {"timeout": _client_timeout(timeout), "max_retries": 0}`; docstring `:376-378`: "Retry is delegated to hermes's outer loop (`max_retries=0`): the SDK default of 2 uses its own backoff that ignores Retry-After and double-retries inside our loop." |
| Anthropic Bedrock | `agent/anthropic_adapter.py:524` — `max_retries=0,  # retry belongs to hermes's outer loop (honors Retry-After)` |
| OpenAI primary/aggregators | `agent/agent_runtime_helpers.py:2115` — `client_kwargs.setdefault("max_retries", 0)`, preceded by `:2107-2113` explaining the OpenAI SDK default of 2 double-retries. "This is the single chokepoint every primary OpenAI/aggregator client passes through (init, switch_model, recovery, restore, request-scoped)". |
| OpenAI request-scoped | `agent/client_lifecycle.py:451` — `request_kwargs["max_retries"] = 0` |
| Auxiliary (sync + async) | `agent/auxiliary_client.py:195` and `:4759` — `kwargs.setdefault("max_retries", 0)` / `async_kwargs.setdefault("max_retries", 0)` |

**Pinned versions** (`pyproject.toml` + `uv.lock`, verified in lock):

- `anthropic = ["anthropic==0.87.0"]` (`pyproject.toml:229`) — lock `name = "anthropic"`, `version = "0.87.0"` (`uv.lock:466-467`). This is a **real dependency**, not `optionalDependencies`-style / not a spawned CLI — but it is an *optional extra* (`anthropic` extra, "only needed when provider=anthropic"), i.e. **default-off when unused**, and irrelevant to the 429 question because `max_retries=0` disables its retry.
- `openai==2.24.0; python_version >= '3.14'` (`pyproject.toml:40`) — lock `uv.lock:4353-4355`.
- `httpx[socks]==0.28.1` (`pyproject.toml:51`) — lock `uv.lock:2942-2943`.
- No `google-genai`, no `litellm` dependency; Gemini is hand-rolled over httpx
  (`agent/gemini_native_adapter.py:788`: `retry_after = parse_retry_after_seconds(response.headers)`).

**Delegated-layer ground truth.** I fetched
`https://cdn.jsdelivr.net/npm/@anthropic-ai/sdk@0.87.0/src/client.ts` and confirmed the SDK's
retry knobs are *present but disabled by Hermes*: `maxRetries` documented `@default 2`
("The maximum number of times that the client will retry a request in case of a temporary failure,
like a network error or a 5XX error from the server"). **I did not read the SDK's
`shouldRetry`/`retryRequest` bodies** — the one shell command I used to search the cached file
was denied by the user, and I did not retry it or route around it. So the SDK's own header
handling at 0.87.0 is **unverified this pass**. It is nonetheless **not load-bearing**: Hermes
passes `max_retries=0` on every client it builds, so the effective runtime 429 behavior is the
outer conversation loop audited in §3, §7, §8. The prior worked example's Anthropic-TS notes
(`retry-after-ms` then `Retry-After` as `parseFloat(…)*1000`, then `Date.parse`, default
`maxRetries: 2`) are consistent with the `@default 2` I read in 0.87.0, but I did not re-verify
them for this version and do not assert them as fact here.

**Two hand-rolled HTTP clients bypass the shared parser** (secondary paths, both read):

- `pm/network.py:43-59` — its own `_delay()`: `int(value)` then `parsedate_to_datetime`, `max(delay, retry_after)`. Handles delta-seconds + all three HTTP-date forms; **not** RFC3339; a `Retry-After` of 0 is correctly ignored (the `if value:` truthiness check) and `int()` is strict-ish. Called via `retry_network` (`pm/network.py:62`), `_ATTEMPTS` bounded.
- `tools/fal_common.py:107-124` — `_managed_fal_retry_after_seconds`: 429-gated, reads `Retry-After` then body `error.retryAfter`, `float(raw)`, and **waits it out once** (`submit_managed_fal_with_rate_limit_retry`, "on a 429 whose Retry-After fits the cap, wait it out (interrupt-aware) and resubmit ONCE under a new key").
- `tools/connectors/gateway/errors.py:104` — `retry_after = float(header) if header not in (None, "") else (retry_after_ms or 1000.0) / 1000.0`; `retry_after_ms` is Hermes' own **body** field `retryAfterMs` (`:85`), never a header.
- `plugins/spotify/client.py:63,115` — reads the header but only interpolates it into an error *message* ("Retry after {retry_after} seconds"); no sleep. Message-only.

---

## Summary table

| Axis | Finding |
|---|---|
| 1. Consumer/producer | **Consumer** in `agent/`; `gateway/` is a producer (mirror image) |
| 2. Forms | delta-seconds yes (**lax**: `float()`, accepts `30.7`/`1e3`/`inf`); IMF-fixdate **yes**; RFC850 **yes**; asctime **yes**; RFC3339 **no**; `retry-after-ms` **no** |
| 3. Honored | **Honored** (capped 600 s; `min(x,600)`, adaptive policy gated off when present) |
| 4. Past/garbage | **Clamp to 0 → discarded as absent → jittered backoff**; never a negative or zero sleep |
| 5. Provider headers | `x-ratelimit-reset`, `x-ratelimit-reset-requests{,-1h}`, `x-ratelimit-reset-tokens`, `anthropic-ratelimit-{requests,tokens}-reset` (RFC3339 handled via `fromisoformat`). Missing: `anthropic-ratelimit-{input,output}-tokens-reset`, `retry-after-ms`, `x-should-retry` |
| 6. Detection | **Mixed, status-first** (`status_code` + typed `FailoverReason`); body regex only to disambiguate within 429 |
| 7. Default attempts | **3** (`hermes_cli/config_defaults.py:133`, read `agent/agent_init.py:1467`) |
| 8. Short-retry burn | No immediate re-fire in the main loop; **`fallback.min_switch_reset_seconds` is default-off (0)**, so a 429 rotates to the fallback provider by default |
| 9. Delegation | **None for the retry itself** — implements directly; Anthropic 0.87.0 / OpenAI 2.24.0 / httpx 0.28.1 all pinned and driven at `max_retries=0` |

## Weakest points, ranked

1. **RFC3339 in `Retry-After` returns `None`** (`retry_utils.py:57`, no `fromisoformat` fallback in
   this parser). Senders that put Anthropic-style `2027-12-31T23:59:59Z` in `Retry-After` get the
   computed 2→60 s jittered backoff instead of the declared window — the exact class of bug the
   baseline's strict-Atoi-plus-spec-order parser was built to avoid. The meept baseline parses
   RFC3339 as its 4th form; Hermes does not.
2. **`float('inf')` is not rejected** (`retry_utils.py:52`). Currently neutralized downstream by
   `min(_retry_after, 600)`, so not exploitable today, but it is a missing `math.isfinite` guard
   one refactor away from a hang. (`nan` is accidentally safe.)
3. **`fallback.min_switch_reset_seconds` defaults to 0**, so the shipped behavior on a 429 is to
   rotate providers immediately rather than honor the window (the guard exists, but off).
4. `anthropic-ratelimit-input-tokens-reset` / `-output-tokens-reset` unconsumed, and
   `retry-after-ms` / `x-should-retry` unconsumed.