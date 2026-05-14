---
name: convention-commit-style
description: "enforce conventional commit message format with structured body"
scope: session
---

All commit messages must follow the Conventional Commits format:

```
<type>(<scope>): <short summary>

<optional body with context>

<optional footer(s)>
```

Rules:
- **type** must be one of: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`, `build`, `ci`, `style`
- **scope** is optional but encouraged: the package or component affected (e.g., `agent`, `llm`, `memory`)
- **short summary**: lowercase, imperative mood ("add feature" not "added feature"), no period, max 72 chars
- **body** (optional): explain WHY, not WHAT. The diff shows what changed. Wrap at 72 chars.
- **footer** (optional): breaking changes (`BREAKING CHANGE: description`), issue references (`Closes #123`)

Examples:
```
feat(agent): add template injection to agent loop
fix(memory): prevent duplicate entries in FTS index
refactor(security): extract input sanitizer into standalone package
docs(config): document new template discovery directories
```

When suggesting commit messages, always provide the full message including body if the change needs context.
