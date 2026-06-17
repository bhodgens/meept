# predid

A `go vet`-style analyzer that detects use of **pred**ictable time-based
values as **ID**entifiers.

## What it flags

The analyzer reports any of these patterns when used in a context that
suggests ID generation:

| Pattern | Why it's bad |
|---------|-------------|
| `time.Now().UnixNano()` | Predictable, can collide under concurrency |
| `time.Now().UnixMicro()` | Same — microseconds since epoch |
| `time.Now().Unix()` | Same — seconds since epoch |
| `time.Now().Format("20060102150405")` | Compact timestamp as string ID |

**Fix:** Use `pkg/id.Generate()` which provides cryptographically-secure
unique IDs.

## What triggers a flag

The analyzer only reports when the predictable call appears in a context
that looks like ID usage:

1. **Assignment to ID-like variable:** `sessionID := time.Now().UnixNano()`
2. **Struct field with ID-like name:** `Foo{ID: time.Now().UnixNano()}`
3. **ID setter method argument:** `obj.SetID(time.Now().UnixNano())`
4. **Variable declaration:** `var token = time.Now().UnixNano()`

"ID-like" means the name contains one of: `id`, `uid`, `gid`, `rid`,
`key`, `token`, `session`, `uuid`, `guid`, `nonce`, `seed`, `hash`.

## What it does NOT flag

- `time.Now().UnixNano()` assigned to `timestamp` or `createdAt` (not ID-like)
- `time.Now().Format(time.RFC3339)` (has separators, not used as ID)
- Calls on non-`time.Now()` receivers

## Usage

```bash
# Run via make
make predid

# Or directly
go run ./tools/analyzers/predid/ ./...

# On a specific package
go run ./tools/analyzers/predid/ ./internal/agent/...
```

## Known limitations

1. **Conservative name matching** — may miss ID uses in variables with
   non-standard names (e.g., `ref`, `pk`).
2. **No interprocedural analysis** — if `time.Now().UnixNano()` is passed
   to a helper function that uses it as an ID, it won't be flagged.
3. **Format string heuristic** — `isIDFormat` uses a simple heuristic
   (no separators + contains `2006`). Some non-ID formats may match.

## Structure

```
tools/analyzers/predid/
├── main.go              # entrypoint (package main)
├── README.md            # this file
└── predid/
    └── analyzer.go      # analyzer (package predid)
```
