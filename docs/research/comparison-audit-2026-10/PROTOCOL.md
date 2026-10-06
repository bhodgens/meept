# 429 / Retry-After RFC Audit Protocol

Read this whole file first. Then audit your assigned repo. Do not modify anything.

## What counts as "correct 429 RFC handling"

The header contract is RFC 9110 section 10.2.3 (`Retry-After`), whose value is
either:

- **delay-seconds** = `1*DIGIT` over the WHOLE header value (RFC 9110 section 7.1.3
  number grammar). Seconds to wait.
- **HTTP-date** = RFC 9110 section 5.6.7, whose preferred serialization is
  **IMF-fixdate**: `Fri, 31 Dec 2027 23:59:59 GMT`. A recipient is required by
  RFC 9110 section 5.6.7 to accept the obsolete **RFC850**
  (`Friday, 31-Dec-27 23:59:59 GMT`) and **asctime**
  (`Fri Dec 31 23:59:59 2027`) forms too.

**RFC3339** (`2027-12-31T23:59:59Z`, ISO 8601 profile) is NOT part of
`Retry-After`. It is common in the wild because Anthropic uses it in
`anthropic-ratelimit-*-reset`, and some senders put it in `Retry-After`
anyway. Parsing it is a compatibility win, not a requirement.

Provider-specific millisecond header: `retry-after-ms` (OpenAI-style, integer
milliseconds). This is a vendor extension, not an RFC.

Common bugs to hunt for:

- Prefix-parsing the delta (`fmt.Sscanf`, `parseFloat`, `atoi` on a substring, a
  regex with no anchor). `Retry-After: 2027-12-31T23:59:59Z` read as a huge
  integer, or `Retry-After: 30; foo` accepted as 30.
- Accepting a **negative** or zero delay and sleeping nothing, causing a hot
  retry loop that burns a multi-hour quota window.
- Parsing the header into a variable that is then **overwritten** by a computed
  exponential backoff. A parser whose result is never slept on is a fail.
- Only handling 429 via **string/regex matching on the error body** instead of
  the HTTP status code.
- Distinguishing rate limits from other failures, so a 429 rotates to a
  different model/provider instead of waiting out the window.
- A retry loop whose default attempt count is 1 (i.e. no retry at all).

## Audit axes — answer every one

1. **Consumer or producer?** Does it READ `Retry-After` from a response it
   received, or only SET it on responses it serves? A grep hit can be the mirror
   image of what was asked. Say which, explicitly.
2. **Forms parsed.** For each of: delay-seconds, IMF-fixdate, RFC850, asctime,
   RFC3339, `retry-after-ms` — yes or no. For delay-seconds, note if the parse
   is lax (prefix match, float, whitespace tolerant).
3. **Honored or ignored?** Does the actual sleep/backoff derive from the parsed
   value, or is it discarded in favor of computed backoff? `partial` = capped,
   clamped, or used only as one input among several.
4. **Past / unparseable values.** Past date, zero, negative, garbage: clamped to
   a floor, slept as negative, or does it fall through to computed backoff?
5. **Provider reset headers.** Any of: `anthropic-ratelimit-tokens-reset`,
   `anthropic-ratelimit-requests-reset`, `anthropic-ratelimit-input-tokens-reset`,
   `anthropic-ratelimit-output-tokens-reset`,
   `x-ratelimit-reset-requests`, `x-ratelimit-reset-tokens`,
   `x-ratelimit-reset`, `x-should-retry`, `retry-after-ms`.
6. **429 detection.** HTTP status code, a typed error class, or string/regex on
   the body?
7. **Default attempt count.** Report the DEFAULT, not the capability.
   "Loop exists, default maxAttempts=1" means no retry.
8. **Short-retry burn.** Can a 429 trigger an immediate or short-interval
   re-fire against the same window? (Burns the window it was told to respect.)
9. **Delegation chain — the most important axis.** If the repo does not implement
   this itself, find what it delegates to: an LLM SDK (openai, anthropic,
   google-genai, ai-sdk), a spawned CLI (claude-code, codex), a gateway
   subprocess, a reverse proxy. Read the dependency manifest — the expected SDK
   being absent from `dependencies` (vs present in `optionalDependencies` or
   `devDependencies`, or present only as a spawned CLI) is decisive evidence.
   Then **read the delegated layer's own source** and report what it does.
   That is the effective runtime behavior. Locate it in `node_modules`,
   `site-packages`, a vendor directory, or a lockfile-pinned version, then fetch
   that exact version from GitHub raw or a versioned CDN
   (`https://cdn.jsdelivr.net/npm/<pkg>@<version>/<path>`).

## Evidence standard

- Every claim needs **file:line plus a short verbatim quote you actually read in
  that file**. Never quote a search-result snippet: fetch the file and read the
  control flow.
- Absence claims must state their method: name the module set you enumerated and
  read in full, and name any enumeration gap. "Grepped the repo, nothing found"
  is not acceptable on its own.
- Pin the ref: run `git -C <repo> rev-parse HEAD` and record the SHA.
- Distinguish **default-off** features from shipped behavior. A flag-gated
  capability is `default off`, not `X`.

## Pitfalls

- Recursive GitHub trees over 1MB get truncated by web fetchers, cutting
  alphabetically. Enumerate per-directory with
  `https://api.github.com/repos/O/R/contents/<dir>?ref=<branch>` instead.
- Unauthenticated `api.github.com/search/code` returns 401. Do not rely on it.
  Use a local clone and grep, which is why the clone is on disk.
- URL-encode path specials in raw URLs: `@` becomes `%40`.
- A vendor SDK may have moved files between versions (e.g. `core.ts` became
  `client.ts`). Pin the version you read.
- Watch for **both** paths existing: a hand-rolled HTTP client AND a vendor SDK
  used elsewhere. Report which path is live for the 429 question.

## Baseline being compared against

Meept, `/Users/caimlas/git/meept/internal/llm/retry_after.go`
(read-only, do not modify). Its `ParseRetryAfter(http.Header)` returns
`(date, delta, present)`:

1. `Retry-After`, strict `strconv.Atoi` over the WHOLE value for delta-seconds.
   Strict on purpose: a prefix match would read an RFC3339 date as a huge delta.
2. If that fails, tries in spec order: `time.RFC1123` (IMF-fixdate),
   `time.RFC850`, `"Mon Jan _2 15:04:05 2006"` (asctime), `time.RFC3339`.
3. A standard `Retry-After` always beats a provider header.
4. Then `anthropic-ratelimit-{tokens,requests,concurrent}-reset` as RFC3339.
5. Then `X-Codex-Primary-Reset-At` and
   `X-Codex-Primary-Reset-After-Seconds` as delta-seconds.
6. Callers compose `retryAt = date` when non-zero, else `now+delta`; the caller
   clamps a past date's negative delta. Errors are classified as quota
   (`QuotaResetError`, hours-long) versus throttle, and quota is never
   short-retried (see `/Users/caimlas/git/meept/internal/llm/AGENTS.md`).

## Deliverable

Write the full report to `/tmp/429audit/<slug>.md` (directory exists). Include
per-axis findings with file:line and quotes, plus the delegation chain with the
delegated layer's version and evidence.

Then return a compact verdict in exactly this shape, under 900 words:

    VERDICT: <2-3 sentences: does it honor a server-sent Retry-After correctly per RFC 9110 + RFC3339 compatibility: yes / partial / no>

    reads_retry_after: yes | no | producer-only | partial
    forms_delay_seconds: yes|no  (lax: how?)
    forms_imf_fixdate: yes|no
    forms_rfc850: yes|no
    forms_asctime: yes|no
    forms_rfc3339: yes|no
    retry_after_ms: yes|no
    honored_or_ignored: honored | ignored | partial
    past_date_handling: clamp | floor | negative-sleep | fallthrough | n/a
    provider_reset_headers: <list, or none>
    detection: status | typed-error | string-regex | mixed
    default_attempts: <n> (and where the default is set)
    delegation: <layer + version it delegates to, or "none - implements directly">
    strongest_evidence: <file:line - one line>
    weakest_point: <the single biggest RFC-correctness gap>
    ref: <commit sha>
