# leaf 04-skill-packaging/01 — make install ships bundled skills

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with verification.
**Do NOT commit. Do NOT run `git add`.**

- Parent: docs/plans/20260905-dependency-visibility/04-skill-packaging/orchestrator.md
- Scope STRICTLY: Makefile (install target + uninstall handling),
  docs/workflows/skills.md (one section). No Go files.
- Dependencies: none (Wave 1).
- Estimated context: ~30K.

## Contract G (verbatim from parent master)

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

No-clobber is the contract: existing user skills are NEVER overwritten.
Uninstall: remove `~/.meept/skills/` only if every dir in it is
byte-identical to a bundled one; otherwise list and keep. If the safety
check is disproportionate, minimum accepted: print the skills dir path
and leave it — report which you did and why.

## Tasks

### Task 1: read the Makefile install/uninstall targets

Confirm MEEPT_HOME definition, tab indentation, echo voice, and where
the uninstall target handles ~/.meept (Makefile:845-846 already removes
~/.meept/skills wholesale — READ IT; the contract's no-destroy rule may
already be satisfied or may need the byte-identical refinement; decide,
implement, report).

### Task 2: implement the install snippet

Contract G verbatim, placed per contract. Real tabs.

### Task 3: uninstall decision

Per Task 1's findings: either refine `rm -rf ~/.meept/skills/` to the
safe form or leave as-is with justification. The wholesale rm is
DESTRUCTIVE to user skills — default to fixing it unless there is a
blocking reason; a diffconflict with in-flight sibling edits to the
Makefile is a blocking reason (report, don't force).

### Task 4: docs

docs/workflows/skills.md — `### Bundled skills` section per parent
Contract G docs paragraph (what ships, where, no-clobber, shadowing,
re-install recipe).

### Task 5: verify

```
make -n install 2>&1 | grep -c "installed skill"   # dry-run: shows loop body (count may be 0 in -n; verify loop appears)
SB=$(mktemp -d) && HOME=$SB make install >/dev/null 2>&1; ls $SB/.meept/skills | wc -l   # expect 14
HOME=$SB make install 2>&1 | grep -c "skipping"    # second run: expect 14
ls $SB/.meept/skills/web-browsing/SKILL.md         # exists
rm -rf $SB
```

If `make install` in a sandbox HOME fails for UNRELATED reasons (gui
build, menubar), scope the verification to the skills portion by running
the loop manually against $SB — report the adaptation.

## Self-Verification Checklist

- [ ] Snippet verbatim; tab-indented; placed per contract
- [ ] Sandbox run: 14 installed, then 14 skipped; user edit survives
- [ ] Uninstall: safe form or justified leave-alone
- [ ] skills.md section written; matches behavior

## Review Checklist (for orchestrator)

- [ ] make -n shows the loop; no shell syntax errors (bash -n on extracted recipe)
- [ ] No other Makefile targets touched
- [ ] Docs match observed sandbox behavior

Do NOT commit.
