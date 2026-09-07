# Plan: Split monolith config loading

## Meta

- task_id: t-20260906-config-split
- version: 1
- status: draft
- updated: 2026-09-06

## Goal

The CLI and the daemon each load only the config sections they need instead of the whole file.

## Decisions

- Decision: One shared schema package, two thin loaders — Rationale: validation lives in one place while loaders stay small.

## Open Questions

## Phases

### Phase 1: Config schema package

Extract schema definitions and validation into a shared internal package.

**Produces:**

- `config-schema` (schema) — validated struct definitions for all config sections
- `config-schema-tests` (test_suite) — table tests covering schema validation

**Consumes:** none

**Steps:**

1. Move struct definitions into internal/configschema [code]
2. Add validation table tests [code] (needs: Phase1.S1)
3. Run the suite and fix fallout [debug] (needs: Phase1.S2)

### Phase 2: CLI loader

Load only CLI-relevant sections in cmd/meept.

**Produces:**

- `cli-config-loader` (interface) — LoadCLIConfig entry point

**Consumes:**

- `config-schema` (schema) — shared definitions for the CLI section

**Steps:**

1. Implement LoadCLIConfig on top of config-schema [code] (needs: config-schema)
2. Wire cmd/meept flags to the loaded config [code] (needs: Phase2.S1)
3. Verify the CLI loads a sample config [bash] (needs: Phase2.S2, Phase1.S3)

### Phase 3: Daemon loader

Load only daemon-relevant sections in internal/daemon.

**Produces:**

- `daemon-config-loader` (interface) — LoadDaemonConfig entry point

**Consumes:**

- `config-schema` (schema) — shared definitions for the daemon section

**Steps:**

1. Implement LoadDaemonConfig on top of config-schema [code] (needs: config-schema)
2. Wire daemon startup to the loaded config [code] (needs: Phase3.S1)
3. Run both loader suites green [bash] (needs: Phase3.S2)

## Notes

Keep the legacy whole-file loader until both callers are migrated.
