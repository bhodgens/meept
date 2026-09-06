# orchestrator.md — 04-skill-packaging branch

## Goal

Deliver Contract F-packaging (Contract G here for scanner uniqueness):
`make install` copies `config/skills/` into `~/.meept/skills/`
(no-clobber) so the 14 bundled skills actually reach users. Today NO
install step ships them — discovery tiers (internal/skills/discovery.go:31)
only look at ~/.meept/skills, ~/.config/meept/skills, and the project dir.

## Architecture Overview

Single leaf: 01-make-install-skills.md. Makefile change + docs + a
verification script. No Go code; no sub-branching.

## Interface Contracts

### Contract G: skills packaging (frozen)

Makefile `install` target gains, immediately BEFORE the existing
`Install complete` echo (Makefile:253):

```make
	@echo "Installing bundled skills (no-clobber)..."
	@mkdir -p $(MEEPT_HOME)/skills
	@for d in config/skills/*/; do \
		name=$$(basename $$d); \
		if [ ! -d "$(MEEPT_HOME)/skills/$$name" ]; then \
			cp -R "$$d" "$(MEEPT_HOME)/skills/$$name"; \
			echo "  installed skill $$name"; \
		else \
			echo "  skipping $$name (exists)"; \
		fi; \
	done
```

No-clobber is the contract: existing user skills are NEVER overwritten
(user tier shadows bundled tier in discovery anyway — internal/skills/discovery.go
PriorityUser > bundled location). `uninstall` gets the reverse: remove
`~/.meept/skills/` ONLY if every directory in it is byte-identical to a
bundled one; otherwise list them and keep (never destroy user edits).
If the uninstall safety check is disproportionate, the minimum accepted
uninstall behavior is: print the skills dir path and leave it — report
which you did and why.

Docs: docs/workflows/skills.md gains `### Bundled skills` — what ships,
where it installs, no-clobber rule, how user edits shadow bundled copies,
how to re-install a pristine copy (delete the skill dir, re-run make install).

## Child Index

| Doc | Scope | Est. context | Dependencies |
|-----|-------|--------------|--------------|
| 01-make-install-skills.md | Makefile install+uninstall + docs + sandbox test | ~30K | none (Wave 1) |

## Dispatch Protocol

Per parent master.md.

## Coding Conventions

Per parent master.md. Extra: Makefile recipe lines use real tabs (verify
the file's existing style — it does); echoes match the target's existing
voice ("Installing X...").

## Completion Tracking Table

| Doc | Status | Notes |
|-----|--------|-------|
| 01-make-install-skills.md | PENDING | |

## Review Checklist (branch)

- [ ] Contract G make snippet verbatim; placed before Install complete
- [ ] No-clobber proven by test (second run skips all)
- [ ] uninstall behavior implemented or explicitly minimized + reported
- [ ] skills.md section matches actual behavior
- [ ] `make -n install` dry-run shows the loop (orchestrator check)

## Integration Test Plan

Sandbox HOME test: `HOME=$(mktemp -d) make -n install` shows skill
copies; real run in a sandbox HOME installs 14 skills; second run
skips 14; a user-modified skill survives re-install. Mark branch
COMPLETE in parent master.md after review.
