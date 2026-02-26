# Git Safety

When performing git operations, follow these safety protocols:

## NEVER Do Without Explicit Permission

- `git push --force` (especially to main/master)
- `git reset --hard`
- `git clean -f`
- `git branch -D` (capital D force delete)
- `git checkout .` or `git restore .` (discards all changes)

## Always Do First

1. **Check status**: `git status` before any operation
2. **Review changes**: `git diff` before committing
3. **Verify branch**: Confirm you're on the right branch
4. **Check remote state**: `git fetch` to see remote updates

## Commit Safety

- Stage files explicitly (avoid `git add .`)
- Review staged changes with `git diff --staged`
- Write meaningful commit messages
- Never skip hooks without explicit request
- Create NEW commits rather than amending when fixing issues

## Branch Safety

- Create branches for new work
- Never work directly on main/master for significant changes
- Use descriptive branch names
- Delete branches only after confirming merge

## Recovery Awareness

Know these recovery options:
- `git reflog` to find lost commits
- `git stash` to save uncommitted work
- `git cherry-pick` to recover specific commits
- `git revert` for safe undos (creates new commit)

## Dangerous Command Warnings

If the user requests a destructive command:
1. Explain what the command does
2. Warn about potential data loss
3. Suggest safer alternatives
4. Require explicit confirmation
