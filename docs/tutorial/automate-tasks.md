# Automate Common Tasks with User Instructions

Tired of running the same commands after every change? User Instructions let you
teach Meept to do them for you, automatically. This walkthrough shows you how to
go from zero to fully automated in about ten minutes.

## What Are User Instructions?

User Instructions are plain-English rules that Meept converts into structured
automation: **"when X happens, do Y."** You write the rule once in natural
language, the instruction parser turns it into a trigger + action pair, and the
daemon fires the action whenever the trigger matches. You never have to remember
the exact syntax -- just type what you want in conversational English.

For the full conceptual background (triggers, actions, storage tiers, context
injection), see [Concepts: User Instructions](../concepts/instructions.md).

---

## Prerequisites

- Meept daemon running (`./bin/meept-daemon -f` or `make go-daemon`)
- CLI built (`make build`)
- You are in a project directory (instructions default to project scope)

If the daemon is not running, start it now:

```bash
make go-daemon
```

You should see `daemon ready` in the log output. Check that the CLI can reach it:

```bash
./bin/meept status
```

---

## Quick Start: Your First Instruction

Let's add the classic "always run `go test` after editing Go files" rule and
watch it fire.

### 1. Preview before you commit

`preview` parses your sentence without saving anything. Use it to verify the
parser understood you correctly:

```bash
meept instructions preview "always run go test after I edit Go files"
```

Expected output (your exact trigger pattern may vary slightly):

```
Parsed instruction preview:
  Trigger Type:    post_hook
  Trigger Pattern: write_file:*.go
  Action Tool:     shell_execute
  Command:         go test ./...
  Scope:           project
  Priority:        normal
  Confidence:      0.85
```

If the confidence is low or the trigger looks wrong, rephrase. Trigger keywords
the parser looks for include: `always`, `never`, `every time`, `whenever`,
`from now on`, `remember to`, `make sure to`, `automatically`.

### 2. Add the instruction

Once the preview looks right, add it for real:

```bash
meept instructions add "always run go test after I edit Go files"
```

Expected output:

```
Instruction created successfully!
  ID:       run-tests-after-go-changes
  Trigger:  post_hook:write_file:*.go
  Action:   shell_execute
  Scope:    project
  Priority: normal
  Enabled:  true
```

### 3. List your instructions

```bash
meept instructions list
```

Expected output:

```
ID                              Trigger                    Action          Scope      Priority
----------------------------------------------------------------------------------------------------
run-tests-after-go-changes      post_hook:write_file:*.go  shell_execute   project    normal
```

### 4. Show the full detail

```bash
meept instructions show run-tests-after-go-changes
```

### 5. Delete when you no longer need it

```bash
meept instructions delete run-tests-after-go-changes
```

```
Instruction 'run-tests-after-go-changes' deleted.
```

That is the entire lifecycle: `preview`, `add`, `list`, `show`, `delete`.

---

## Common Patterns

Below are four patterns that cover the majority of real-world automation needs.
Copy them verbatim and tweak the specifics.

### Pattern 1: Auto-Test on Save

Run your test suite the moment a Go file is written.

```bash
meept instructions add "always run go test ./... after I write Go files"
```

**Parsed as:**
- Trigger: `post_hook:write_file:*.go`
- Action: `shell_execute` with `command: go test ./...`

Variations for other ecosystems:

```bash
# Python
meept instructions add "always run pytest after I write Python files"

# Rust
meept instructions add "always run cargo test after I write Rust files"
```

Tip: narrow the pattern if your repo is huge. "after I write files in
`internal/agent/`" produces `post_hook:write_file:internal/agent/*`, which avoids
triggering on documentation edits.

### Pattern 2: Daily Standup Reminder

Fire a notification on a schedule instead of tying the rule to a tool event.

```bash
meept instructions add "every day at 9am, send me a standup reminder"
```

**Parsed as:**
- Trigger: `cron:0 9 * * *`
- Action: `notification` with `message: standup reminder`

Cron triggers use standard 5-field cron syntax. More examples:

```bash
# Every Monday at 10am
meept instructions add "every Monday at 10am, summarize last week's commits"

# Every hour on the hour
meept instructions add "every hour, check if the build is green"
```

### Pattern 3: Project Conventions via Memory Retain

Instead of running a command, inject a fact into the agent's context so every
future agent turn is aware of it.

```bash
meept instructions add "always remember that this project uses conventional commit format"
```

**Parsed as:**
- Trigger: `manual:` (only fires when you explicitly invoke it, or when the
  context injector pulls active instructions into the system prompt)
- Action: `memory_retain` with `category: preferences`

The retained memory is visible to every agent as part of the "Standing
Instructions" block in the system prompt. This is the right pattern for coding
standards, branch naming conventions, preferred libraries, and other persistent
context.

### Pattern 4: Post-Commit Code Review Reminder

Hook into Git lifecycle events.

```bash
meept instructions add "after every commit, remind me to request a code review"
```

**Parsed as:**
- Trigger: `git_post_commit`
- Action: `notification`

Meept generates a `.git/hooks/post-commit-user` shell script that dispatches to
the daemon via RPC when the hook fires. Verify it was created:

```bash
ls -la .git/hooks/post-commit-user
```

For pre-commit checks (blocking), use:

```bash
meept instructions add "before every commit, run golangci-lint"
```

This generates `.git/hooks/pre-commit-user`. If the instruction action exits
non-zero, the commit is aborted.

---

## Triggers Reference

| Trigger syntax | Fires when | Example |
|---|---|---|
| `cron:<5-field expr>` | Cron schedule matches | `cron:0 9 * * *` (daily at 9am) |
| `post_hook:<tool>:<glob>` | After a tool completes on a matching path | `post_hook:write_file:*.go` |
| `event:<bus_topic>` | A matching event fires on the message bus | `event:session.started` |
| `intent:<intent_type>` | User input is classified as the given intent | `intent:research` |
| `git_pre_commit` | Before a git commit is created | `git_pre_commit` |
| `git_post_commit` | After a git commit is created | `git_post_commit` |
| `manual:` | Only when invoked explicitly via CLI or dispatcher | `manual:` |

You rarely type the trigger syntax yourself -- the natural language parser
generates it from your description. Use `meept instructions preview` to verify
the generated syntax before saving.

### Tips for Trigger Patterns

- **Be specific about file globs.** `*.go` is better than `*`; `internal/agent/*.go`
  is better still when you only want a subsystem.
- **Use cron for time-based rules**, not "every time I start a session" -- the
  latter only fires on session start, not at a wall-clock time.
- **Git hooks are synchronous for pre-commit and asynchronous for post-commit.**
  Pre-commit hooks that fail will block the commit; use them for linting and
  tests. Post-commit hooks are non-blocking; use them for notifications.

---

## Actions Reference

| Action | What it does | Risk level |
|---|---|---|
| `shell_execute` | Run a shell command | Low for known-safe commands, medium for unknown, high for destructive patterns |
| `memory_retain` | Store information in Meept's memory system, injected into future agent context | Low |
| `notification` | Send a notification (desktop, Telegram, etc. depending on config) | Low |
| `agent_trigger` | Invoke a specialist agent (e.g., `researcher`, `analyst`) with a prompt | Medium |
| `file_write` | Write content to a file | Medium |
| `git_commit` | Create a git commit | Medium |

The parser selects the action type based on the verb you use. "Run" maps to
`shell_execute`, "remember" maps to `memory_retain`, "notify" or "remind" maps
to `notification`, "research" or "analyze" maps to `agent_trigger`.

---

## Confirmation Flow and Security

Meept validates every instruction before saving it. There are three risk tiers:

| Risk tier | Examples | Behavior |
|---|---|---|
| **Low** | `go test`, `go build`, `git status`, `ls`, `cat`, `echo` | Saved immediately, no prompt |
| **Medium** | Unknown commands, `git push`, `chmod`, `file_write` | Confirmation required before activation |
| **High** | `rm -rf`, `curl | bash`, `sudo`, `chmod 777`, `dd`, `mkfs` | Blocked by default; requires explicit `--force` override |

When you add an instruction that falls into the medium or high tier, the CLI
prints a warning:

```
  [!] This instruction requires confirmation before activation.
```

For details on risk assessment patterns and the security model behind it, see
[Security](../workflows/security.md) and
[Concepts: User Instructions - Security Model](../concepts/instructions.md#security-model).

---

## Where Instructions Live

Instructions are stored as YAML files in tiered directories with priority
shadowing. The first tier that has a given ID wins; lower tiers are ignored for
that ID.

| Tier | Path | Scope |
|---|---|---|
| Project (highest) | `.meept/instructions/` | Current repository only |
| User | `~/.meept/instructions/` | All of your projects |
| System (lowest) | `~/.config/meept/instructions/` | Machine-wide defaults |

Use the project tier for repo-specific automation (test runners, linters, git
hooks). Use the user tier for personal preferences that apply everywhere
(preferred commit format, coding style). Use the system tier for organization
policies.

You can inspect the raw YAML for any instruction:

```bash
cat .meept/instructions/run-tests-after-go-changes.yaml
```

Example file contents:

```yaml
id: run-tests-after-go-changes
trigger: post_hook:write_file:*.go
action: shell_execute
action_args:
  command: go test ./...
  timeout: 60s
enabled: true
scope: project
priority: normal
created_at: 2026-06-23T09:14:00Z
```

---

## Troubleshooting

### "Daemon not running"

Start the daemon:

```bash
make go-daemon
# or
./bin/meept-daemon -f
```

### "Parse error: invalid instruction"

The parser could not extract a trigger from your sentence. Use trigger keywords
explicitly: `always`, `never`, `every`, `whenever`, `after`, `before`. Run the
input through `preview` first to see what the parser makes of it.

### "Validation failed: tool not found"

The action you described maps to a tool that is not registered. The valid
action tools are: `shell_execute`, `memory_retain`, `notification`,
`agent_trigger`, `file_write`, `git_commit`. Rephrase to use a supported verb.

### Instruction is saved but never fires

1. Check that it is enabled: `meept instructions show <id>`.
2. Confirm the trigger pattern matches the event you expect. For `post_hook`
   triggers, the glob must match the file path the tool operated on.
3. Check the daemon log for instruction execution errors:
   ```bash
   tail -f ~/.meept/logs/daemon.log | grep -i instruction
   ```

### Two instructions with the same ID

IDs are unique within a tier but can shadow across tiers. If a project-tier
instruction has the same ID as a user-tier one, the project version wins. Delete
or rename one of them if this is unintended.

---

## Next Steps

- [Concepts: User Instructions](../concepts/instructions.md) -- deep dive into
  triggers, actions, the security model, and context injection
- [CLI Reference: instructions](../reference/cli/instructions.md) -- every flag,
  exit code, and RPC mapping
- [Feature Spec: User Instructions](../workflows/user-instructions.md) --
  architecture and implementation status
- [Security](../workflows/security.md) -- how risk assessment and validation
  gates work across the platform
- [Quick Start](../getting-started/quick-start.md) -- if you haven't yet, get
  the daemon running and try a chat first
