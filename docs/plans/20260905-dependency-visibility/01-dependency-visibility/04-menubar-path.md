# leaf 01-dependency-visibility/04 — menubar PATH environment

## DISPATCH INSTRUCTION

You are an implementation agent. Implement the Swift change with
verification. **Do NOT commit. Do NOT run `git add`.**

- Parent: docs/plans/20260905-dependency-visibility/01-dependency-visibility/orchestrator.md
- Scope STRICTLY: menubar/MeeptMenuBar/Services/DaemonController.swift (one
  hunk). No other files.
- Dependencies: none (parallel). With leaf 03, closes issue #32.
- Estimated context: ~20K.

## Contract D (verbatim from parent master)

`menubar/MeeptMenuBar/Services/DaemonController.swift`: when spawning
`/bin/launchctl` (process at ~:123), set
`process.environment = ProcessInfo.processInfo.environment.merging(["PATH": <same dir list as Contract C>])`.

The dir list (identical order to Go's DaemonPath guaranteed suffix):
`/opt/homebrew/bin:/usr/local/bin:$HOME/.local/bin:$HOME/go/bin:$HOME/.cargo/bin:/usr/bin:/bin:/usr/sbin:/sbin`
— with $HOME expanded in Swift (`NSHomeDirectory()` or
`ProcessInfo.processInfo.homeDirectoryForCurrentUser.path`). Inherited
PATH first, then missing guaranteed dirs appended (mirror Go's dedup
semantics), with a single-line comment: keep in sync with
internal/daemon/daemonpath.go DaemonPath() (issue #32).

## Tasks

### Task 1: read DaemonController.swift

Understand runLaunchctl(:122-124) and every Process() spawn in the file.
If the daemon itself is spawned by the menubar anywhere else (search
`Process(`), that site gets the same environment.

### Task 2: implement

Small private helper in DaemonController.swift:

```swift
/// PATH for spawned processes: inherited PATH first, then guaranteed
/// dirs appended if absent. Keep in sync with internal/daemon/daemonpath.go
/// DaemonPath() (issue #32).
private static func daemonPATH() -> String
```

And set `process.environment` in runLaunchctl (and any other Process
spawn site found in Task 1). Swift env merge:
`process.environment = ProcessInfo.processInfo.environment.merging(["PATH": daemonPATH()]) { (_, new) in new }`

### Task 3: verify

```
cd menubar && swift build 2>&1 | tail -5
```

(If the package requires Xcode-only build, use `swiftc -parse
MeeptMenuBar/Services/DaemonController.swift` as the syntax gate and say
so in the report.) Also: `git diff` must show exactly one file.

## Self-Verification Checklist

- [ ] daemonPATH() mirrors Contract C order incl. inherited-first + dedup
- [ ] Every Process() spawn site in the file carries the environment
- [ ] Sync comment references daemonpath.go and issue #32
- [ ] swift build (or -parse) passes; diff confined to one file

## Review Checklist (for orchestrator)

- [ ] Dir list order identical to Contract C
- [ ] $HOME expanded at runtime (not literal "$HOME")
- [ ] No behavior change when PATH is already complete

Do NOT commit.
