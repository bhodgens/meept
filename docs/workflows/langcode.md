---
title: Langcode
---

# Langcode

## Overview

`internal/langcode` is the single shared ISO-639 language-code vocabulary used
by config validation (`internal/config/schema.go`) and the output-filter
language detector (`internal/validator/filter_language.go`). It exposes four
pure functions/vars: `Default` ("en"), `Builtin` (the codes the detector can
name with no user data), `Normalize` (lowercase, strip region/script subtags,
ISO-639 primary-subtag shape check), `IsKnown` (vocabulary membership), and
`HasWordTable` (the data-driven tier: a word table at
`$MEEPT_HOME/validator/lang/<code>.txt`).

## Problem

The config layer needs the vocabulary to fail-fast on an unknown
`daemon.output_filters.expected_language` at load, and the validator needs the
same list for its filter. A `config -> validator` import carries the
validator's transitive dependency on `internal/task`/`internal/queue`, which
the kanban StateEvent work puts above `internal/queue` — closing an import
cycle through config. The vocabulary is pure stdlib, so it lives in its own
leaf package and both consumers import DOWN to it (the llm.TierPolicy /
FailurePolicyConfig mirror-not-cycle precedent).

## Behavior

- `Normalize` rejects empty strings, malformed subtags ("en-", "-en") and
  non-ISO shapes ("english"); it accepts well-formed region qualifiers
  ("en-US" -> "en") — deliberate call from bughunt wave M5.
- `IsKnown` = normalizes AND the code is in `Builtin`.
- `HasWordTable` = normalizes AND `<dir>/<code>.txt` exists as a regular file.
- `internal/validator` keeps same-named wrapper funcs over this package for
  its existing callers and test pins; the tables live ONLY here — never
  duplicate them.
- `TestBuiltinLangCodesCoverScriptCodes` (internal/validator) pins `Builtin`
  against the codes `scriptCodeByRune` actually returns.

## Configuration

Consumed by `daemon.output_filters.expected_language` validation
(`internal/config/schema.go`). Word-table dir is `$MEEPT_HOME/validator/lang`.

## Edge Cases

- A code whose primary subtag is not real ("xx") is NOT repaired by
  `Normalize`; it fails the vocabulary check at config load, where the mistake
  is visible, instead of silently resolving to another language.
- Directory-with-same-name as a table file is not treated as a word table
  (`HasWordTable` checks `!IsDir`).
