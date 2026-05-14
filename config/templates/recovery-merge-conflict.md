---
name: recovery-merge-conflict
description: "systematic procedure for resolving git merge conflicts"
scope: turn
---

There are merge conflicts to resolve. Follow this procedure.

**Step 1: Understand the conflict scope.**
- Run `git diff --name-only --diff-filter=U` to list conflicted files.
- For each file, run `git diff <file>` to see all conflict markers.
- Read the commit messages of both branches to understand intent:
  - `git log --oneline HEAD...MERGE_HEAD` for the incoming changes.
  - `git log --oneline MERGE_HEAD...HEAD` for our changes.

**Step 2: Resolve each conflict.**
- Open the conflicted file. Each conflict looks like:
  ```
  <<<<<<< HEAD
  our version
  =======
  their version
  >>>>>>> branch-name
  ```
- For each conflict block:
  1. Read both versions carefully. Understand what each side was trying to achieve.
  2. Decide: keep ours, keep theirs, or merge both changes.
  3. The correct resolution is usually **both** -- integrate the incoming change into our modified code (or vice versa). Do not blindly pick one side.
  4. Remove the conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`).
  5. Verify the resolved code compiles and makes logical sense.

**Step 3: Verify the resolution.**
- For Go code: run `go build ./path/to/package/` to check compilation.
- Run `go vet ./path/to/package/` to catch issues.
- If tests exist for the affected code, run them.
- Read the resolved file end-to-end to catch orphaned conflict markers or garbled code.

**Step 4: Stage and verify.**
- `git add <resolved-files>`
- Run `git status` to confirm no remaining unmerged paths.
- Run `go build ./...` to verify the full project compiles.
- Commit with a descriptive message noting the merge and any non-trivial resolutions.

**Common pitfalls:**
- Don't leave conflict markers in the file.
- Don't accidentally delete code from one side that the other side depends on.
- Watch for duplicate imports after merging -- both sides may have added the same import.
- If a file was renamed on one side and modified on the other, `git` may not handle this well. Check for orphaned files.

Conflicted files:

$@
