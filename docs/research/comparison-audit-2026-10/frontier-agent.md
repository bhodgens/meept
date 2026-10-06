# 429 / Retry-After RFC Audit — FrontierAgent

- **Repo:** `/var/folders/mf/1mvt9vbx7q3f9ynln1p79cfr0000gn/T/meept-compare/frontier-agent`
- **Ref:** `179709fee18ae8506ac77bc724b3cea0ac1e0dbf` (770 tracked files, shallow clone)
- **Verdict:** **partial** — a `Retry-After` parser exists one layer down in the pinned
  engine dependency and its delay-seconds value is genuinely slept on, but it parses
  only `float()`-able values. Every HTTP-date form, RFC3339, and `retry-after-ms`
  falls through to computed backoff, and a **negative** header value is slept verbatim
  as a negative (i.e. no sleep at all).

---

## Headline: this repo is a pure delegator for the 429 path

The recon hint ("zero `Retry-After` hits") is **confirmed and is not decisive on its
own** — the repo's own `frontier_agent/infra/*` modules are module-rebinding facades
that hand every LLM concern to the pinned engine package.

**Absence-claim method.** I enumerated and read in full: all 17 files of
`frontier_agent/infra/` (including every `*_client.py` facade), `frontier_agent/core/llm.py`,
`frontier_agent/core/errors.py`, `frontier_agent/core/runtime/loop/` (18 files),
`frontier_agent/infra/config.py`, `frontier_agent/infra/llm_adapter.py` (693 lines),
`deploy/huggingface/errors.py`, and the four `plugins/tools/` HTTP modules that
touch 429 (`web_search.py`, `web_fetch.py`, `web_fetch_aligned.py`, `view_image.py`).
Repo-wide case-insensitive `retryafter|retry_after|retry-after` over all 770 tracked
files returns **zero** hits (exit 1); the only `retry after` phrase in the tree is prose
(`frontier_agent/components/observers/duplicate_query_rollback.py:30`). Zero hits for
`retry-after-ms`, `anthropic-ratelimit`, `x-ratelimit`, `x-should-retry`.
**Enumeration gap:** the `agent_core` package is **not in the tree at all** (see
Delegation) so its source was read from GitHub at the pinned tag, not from disk; and
`benchmarks/frontierchallenge/` (a separate pinned sub-project, excluded from ruff per
`pyproject.toml:135`) was grepped but not read line-by-line.

### The facades

`frontier_agent/infra/openai_client.py` (10 lines total):

```python
import agent_core.providers.openai_chat as _implementation
from agent_core.providers.openai_chat import *  # noqa: F403
sys.modules[__name__] = _implementation
```

Same shape for `anthropic_client.py` → `agent_core.providers.anthropic`,
`protocol_client.py` → `agent_core.providers.protocol_client`,
`retriable.py` → `agent_core.runtime.retriable`, `llm/fallback.py` →
`agent_core.providers.fallback`, and `core/runtime/loop/_call.py` →
`agent_core.runtime.loop._call`.

### Delegation chain

| Layer | Version | Source |
|---|---|---|
| **Engine (decisive)** | `apodex-agent-core==0.12.2` (import namespace `agent_core`) | `github.com/ApodexAI/AgentCore` @ tag `v0.12.2` |
| LLM SDK (OpenAI) | `openai==2.54.0` | `openai/openai-python` @ `v2.54.0` |
| LLM SDK (Anthropic) | `anthropic==1.0.0` | via `uv.lock` |

Evidence for the pin — `pyproject.toml:11`:
```toml
    "apodex-agent-core==0.12.2",
```
and `uv.lock:210-211`:
```
name = "apodex-agent-core"
version = "0.12.2"
```
`uv.lock:1016` confirms it is a **runtime `dependencies` entry, not optional/dev** —
`{ name = "apodex-agent-core", specifier = "==0.12.2" }`. It is therefore the live path,
not an unused or test-only package. No CLI is spawned (no `claude`/`codex` subprocess);
the chain is repo → `agent_core` → vendor SDK.

---

## Axis-by-axis

### 1. Consumer or producer? — **Consumer**

The delegate *reads* the header off an exception it received. It never sets one on a
response it serves. `agent_core/runtime/loop/_call.py:60-61`:

```python
def _get_retry_after(exc: Exception) -> float | None:
    """Extract a Retry-After header value (seconds) from a 429 exception."""
```

Nothing in the repo sets `Retry-After` on outbound responses either (zero grep hits).

### 2. Forms parsed

The entire parser is `agent_core/runtime/loop/_call.py:60-75`:

```python
def _get_retry_after(exc: Exception) -> float | None:
    for attr in ("response", "headers"):
        obj = getattr(exc, attr, None)
        if obj is None:
            continue
        headers = getattr(obj, "headers", obj) if attr == "response" else obj
        if not hasattr(headers, "get"):
            continue
        val = headers.get("retry-after") or headers.get("Retry-After")
        if val:
            try:
                return float(val)
            except (ValueError, TypeError):
                pass
    return None
```

It is a bare `float(val)` — **no date parsing, no `retry-after-ms`, no provider
headers**. Traced by executing the exact expression against each RFC form:

| Form | Example | Parsed | Resulting backoff |
|---|---|---|---|
| delay-seconds | `30` | `30.0` | **30.0 s (honored)** |
| delay-seconds (lax) | `30.5` | `30.5` | 30.5 s — floats accepted, spec says `1*DIGIT` |
| IMF-fixdate | `Wed, 31 Dec 2027 23:59:59 GMT` | `None` | falls to computed backoff |
| RFC850 | `Friday, 31-Dec-27 23:59:59 GMT` | `None` | falls to computed backoff |
| asctime | `Fri Dec 31 23:59:59 2027` | `None` | falls to computed backoff |
| RFC3339 | `2027-12-31T23:59:59Z` | `None` | falls to computed backoff |
| `retry-after-ms` | `5000` | `None` (never looked up) | falls to computed backoff |
| `0` | `0` | `0.0` → falsy | falls to computed backoff (safe) |
| garbage | `30; foo` | `None` | falls to computed backoff |

So: **delay-seconds yes (lax: `float()`, accepts floats, no `strconv`-strict
whole-value discipline)**; IMF-fixdate / RFC850 / asctime / RFC3339 / `retry-after-ms`
all **no**.

Notably the repo's own pinned `openai==2.54.0` SDK **does** implement all of this
correctly — `_base_client.py:759-795` tries `retry-after-ms` first, then float
seconds, then `email.utils.parsedate_tz` (which accepts all three date forms), and
`_should_retry` honors `x-should-retry` and a max-retry-timeout
(`_base_client.py:821-839`). **That correct implementation is disabled** — every
client is constructed `max_retries=0`:

`agent_core/providers/openai_chat.py:196`
```python
            max_retries=0,           # retries are owned by the runtime loop
```
`agent_core/providers/anthropic.py:82` and `:373`
```python
                api_key=api_key, base_url=base_url, timeout=timeout, max_retries=0,
```
`agent_core/providers/openai_responses.py:79` — same. So the weaker hand-rolled
parser *replaces* a stronger vendor one. This is the single most consequential finding.

### 3. Honored or ignored? — **honored (for delay-seconds), partial overall**

`agent_core/runtime/loop/_call.py:1112-1133`:
```python
            elif status == 429:
                retry_reason = "rate_limited"
                # 429 honours ``Retry-After`` (clamped at 300s ceiling so a
                # buggy upstream returning ``Retry-After: 86400`` cannot
                # silently stall the loop for a day); ...
                if retry_wait_fixed is not None:
                    backoff = retry_wait_fixed
                else:
                    retry_after = _get_retry_after(exc)
                    backoff = (
                        min(retry_after, 300)
                        if retry_after
                        else _default_rate_limit_backoff(attempt)
                    )
```
and the value is genuinely slept at `_call.py:1184`:
```python
            await asyncio.sleep(backoff)
```
This is a **real** honor, not a parsed-then-discarded variable — the protocol's
"parser whose result is never slept on is a fail" does not apply. It is `partial`
because (a) only the seconds form survives, (b) it is **capped at 300 s**, so a server
saying `Retry-After: 900` is under-waited 3×, and (c) `retry_wait_fixed` — when set —
**discards the server value entirely** (`:1121-1122`). That override defaults to
`None` (`agent_core/loop_types.py:166`) and the repo never sets it (grep for
`retry_wait_fixed` in the repo returns only the facade pass-through at
`frontier_agent/core/runtime/loop/_call.py:23,42`), so in shipped behavior the server
value wins. Cap makes it `partial`, not `honored`.

### 4. Past / unparseable / negative values — **negative-sleep (the bug)**

`_call.py:1125-1129` guards only truthiness and an upper cap. Traced values:

- `Retry-After: -5` → `float` gives `-5.0` → truthy → `min(-5.0, 300)` = `-5.0` →
  `asyncio.sleep(-5.0)` returns immediately → **the loop re-fires instantly against
  the same window**. This is exactly the "hot retry loop that burns a quota window"
  bug the protocol names. There is **no lower clamp anywhere** in the file.
- `Retry-After: 0` → `0.0` is falsy → `if retry_after` is False → falls through to
  `_default_rate_limit_backoff` (safe by accident).
- Past IMF-fixdate / unparseable → `None` → falls through to computed backoff
  (`_call.py:96-97`: `30/60/120/240/300s` base, ±25% jitter). Safe, but it *ignores*
  a server that correctly said "retry at 23:59:59 GMT".

The only floor on the path is the deadline guard at `_call.py:1146-1149`, which
abandons retries when `backoff + _WALL_DEADLINE_FLOOR_S > deadline_remaining` — a
deadline budget, not a rate-limit floor, and it fires for a negative `backoff` too
only because `-5 + 20 > remaining` is usually false (i.e. it does not protect).

### 5. Provider reset headers — **none**

Grepped `anthropic-ratelimit-tokens-reset`, `-requests-reset`,
`-input-tokens-reset`, `-output-tokens-reset`, `x-ratelimit-reset-requests`,
`x-ratelimit-reset-tokens`, `x-ratelimit-reset`, `x-should-retry`, `retry-after-ms`
across the repo **and** the delegate's 12 enumerated modules
(`runtime/retriable.py`, `providers/{openai_chat,anthropic,fallback,protocol_client,openai_responses,aux_builder}.py`,
`runtime/loop/agent_loop.py`, `retry_policy.py`, `errors.py`, `loop_types.py`,
`runtime/llm_request_overrides.py`): **zero hits** for every one. `x-should-retry`
exists only in the disabled `openai` SDK path (`_base_client.py:832-839`).

### 6. 429 detection — **mixed (status first, string-regex as fallback)**

The retry branch keys on a **status code**, not the body:
`_call.py:1083-1112`
```python
            status = _get_status_code(exc)
            if status and status in (400, 401, 403, 404):
                ...
            elif status == 429:
```
with `get_status_code` (`agent_core/runtime/retriable.py:280-291`) doing typed
attribute lookup on `("status_code", "status", "code")`:
```python
    for attr in ("status_code", "status", "code"):
        val = getattr(err, attr, None)
        if isinstance(val, int):
            return val
```
So a real 429 does **not** match a "retry against the body" failure mode. However a
string/regex path also exists in the same package — `retriable.py:317-321`:
```python
def is_rate_limited(err: BaseException) -> bool:
    """True if ``err`` is a per-key rate-limit (429). Rotating keys
    likely helps; backing off also helps."""
    blob = _stringify(err)
    return any(p.search(blob) for p in _RATE_LIMIT_PATTERNS)
```
with `_RATE_LIMIT_PATTERNS = (re.compile(r"rate[_\s]*limit", ...), re.compile(r"\b429\b"))`
(`retriable.py:121-124`) — used for chain-advancement, not the sleep decision. The
repo itself also carries a body-string classifier at
`frontier_agent/infra/llm_adapter.py:42-49`:
```python
_RETRYABLE_KEYWORDS = frozenset({
    "timeout", "timed out", "429", "500", "502", "503", "504", "529",
    "overloaded", "rate limit", "rate_limit", "server error",
```
— that one *is* pure string matching, and it drives a second, independent retry layer
(`llm_adapter.py:216`: `delay = min(0.5 * (2 ** attempt), 8) + random.random() * 0.25`).

### 7. Default attempt count — **5**

`agent_core/loop_types.py:164`:
```python
    max_llm_retries: int = 5
```
consumed at `agent_core/runtime/loop/agent_loop.py:847-848`:
```python
    response = await call_llm(
        llm_for_turn, messages_for_call, cfg.llm_timeout, cfg.max_llm_retries, turn,
```
The repo's only production `LoopConfig(...)` construction —
`apodex/task_runner.py:283-301` — sets `max_turns`, `llm_timeout=180`,
`tool_result_max_chars`, compactor, etc. and **does not set `max_llm_retries`**, so the
default 5 applies. A second layer sits above it: `FallbackLLM` defaults to
`max_retries: int = 2` (`frontier_agent/infra/llm_adapter.py:77`) fed from
`llm_fallback_max_retries: int = 2` (`frontier_agent/infra/config.py:156`) — but that
wrapper is **default-off** (`llm_fallback_model: str = ""  # empty = disabled`,
`config.py:155`), so shipped default is 5 attempts from the engine loop only.

### 8. Short-retry burn — **yes, via the negative path**

Two routes re-fire against the same window:
1. **Negative `Retry-After`** (§4) sleeps ~0 s and immediately re-fires, up to 5
   attempts, inside the window the server asked to respect.
2. A **date-form `Retry-After`** is discarded for `_default_rate_limit_backoff(attempt)`
   = `min(30 * 2**attempt, 300)` jittered — attempt 1 waits ~30 s regardless of the
   server's actual reset instant.

Mitigating: the engine deliberately keeps 429 **off** the chain-escalation list
(`agent_core/runtime/retriable.py:33-34`, and `is_retriable_with_fallback` excludes
rate limits) so a 429 waits out the window on the same key rather than rotating
provider — the correct policy distinction, and better than the repo's own
`FallbackLLM`, whose `rate_limit`-triggered degrade is opt-in.

### 9. Repo-local 429 handlers (secondary, all computed-backoff)

These never read a header either:

- `plugins/tools/web_search.py:326-331` (Serper):
  ```python
                if resp.status_code == 429:
                    wait = 2 ** attempt
                    logger.warning("Serper 429 rate limited, retrying in %ds (attempt %d)", wait, attempt + 1)
  ```
  `_MAX_RETRIES = 3` (`web_search.py:26`).
- `plugins/tools/web_fetch.py:540-544` (Jina): `if status == 429: wait = 2 ** attempt`;
  `_MAX_RETRIES = 3` (`web_fetch.py:46`).
- `plugins/tools/web_fetch_aligned.py:191-195`: retries on `sc in [408, 409, 425, 429]`
  with a precomputed `retry_delays` list, no header read.
- `plugins/tools/view_image.py:75-79`: `wait = 2 ** attempt`, 3 attempts.
- `deploy/huggingface/errors.py:49-52` is **producer-side only** — it maps 429 to a
  slug for responses it serves (`"RateLimitError": 429` at `:80`), never reads one.

---

## Comparison to the Meept baseline

`/Users/caimlas/git/meept/internal/llm/retry_after.go` wins on every axis: strict
whole-value `strconv.Atoi` for delay-seconds (so an RFC3339 date can't be misread as a
huge delta), then `time.RFC1123` → `RFC850` → asctime → `RFC3339` in spec order, plus
`anthropic-ratelimit-*-reset` and `X-Codex-*` fallbacks, with a standard `Retry-After`
always beating a provider header and a past-date clamp. FrontierAgent's delegate
implements the first clause only.

---

## Ranked findings

1. **Negative `Retry-After` is slept as a negative** — `agent_core/runtime/loop/_call.py:1126`
   + `:1184`. `min(retry_after, 300)` has no lower bound; `asyncio.sleep(-5.0)` returns
   immediately, so up to 5 attempts burn the window instantly. Fix: clamp to
   `max(retry_after, floor)` (or reject `<= 0` into the computed-backoff branch).
2. **All HTTP-date forms and `retry-after-ms` are unparsed** —
   `_call.py:60-75`. A server saying "retry at 23:59:59 GMT" is ignored. Fix: mirror
   the pinned SDK's already-present `_parse_retry_after_header`
   (`openai/_base_client.py:759-795`): `retry-after-ms`, float seconds, then
   `email.utils.parsedate_tz`.
3. **The correct vendor implementation is switched off** — `max_retries=0` at
   `openai_chat.py:196`, `anthropic.py:82` and `:373`, `openai_responses.py:79`.
   The hand-rolled parser is strictly weaker than what it replaced.
4. **300 s hard cap under-waits long windows** — `_call.py:1126`. A legitimate
   `Retry-After: 3600` is retried after 300 s.
5. **`float()` laxness** — `_call.py:72`. `30.5` is accepted (spec allows `1*DIGIT`
   only). Low harm, but it means a whitespace/prefix-ish sender can steer the delay.
6. **Second, redundant retry layer** — `frontier_agent/infra/llm_adapter.py:200-231`
   re-matches `429` as a body substring and applies its own 0.5–8 s backoff. Default
   off, but when enabled it stacks on top of the engine loop.

## Verdict

**partial.** A `Retry-After` read exists in the pinned engine dependency
(`apodex-agent-core==0.12.2`), is on the consumer side, keys 429 off the HTTP status
code, and the parsed delay is genuinely slept (`await asyncio.sleep(backoff)`) rather
than discarded — so the delay-seconds form is honored. It is not RFC 9110-conformant:
IMF-fixdate, RFC850, asctime, RFC3339 and `retry-after-ms` are all unparsed and fall
through to computed backoff, the honor is capped at 300 s, and a negative value is
slept as a negative, producing a hot retry loop against the very window the server
asked to protect. The repo itself contains zero header reads.