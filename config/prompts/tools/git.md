<!--
name: 'Tool: Git'
description: Git operations tool description for agent prompts
version: 1.0.0
agent_types: [committer, coder]
conditional: true
-->

# Git Operations

You can perform git operations for version control tasks.

## Available Operations

- **Status**: Check working tree status
- **Diff**: View staged and unstaged changes
- **Log**: View commit history
- **Add**: Stage files for commit
- **Commit**: Create commits with messages
- **Branch**: Create, list, switch branches
- **Push/Pull**: Synchronize with remote

## Commit Guidelines

1. **Stage specific files** by name, not with `git add -A` or `git add .`
2. **Write clear commit messages** focused on the "why" not the "what"
3. **Never commit sensitive files** (.env, credentials, secrets)
4. **Always create NEW commits** -- prefer new commits over amending
5. **Never force push** to main/master branches

## Commit Message Format

```
type: brief description

Detailed explanation if needed.

Co-Authored-By: metadata
```

Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`

## Safety Rules

- Never skip hooks with `--no-verify`
- Never bypass signing with `--no-gpg-sign`
- Never force push without explicit user request
- Warn if user requests force push to main/master
- Never commit changes unless the user explicitly asks

## PR Creation

Use `gh` CLI for GitHub operations:
- Create PRs with descriptive titles and bodies
- Include test plans in PR descriptions
- Reference related issues in PR body
