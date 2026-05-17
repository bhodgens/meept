# Duplicate `help` Command in CLI

**Date**: 2026-05-15
**Phase**: 12 (CLI/TUI & session management)
**Severity**: low
**Component**: `cmd/meept/main.go`

## Description

The CLI `--help` output lists `help` twice in the `Available Commands` section:

```
Available Commands:
  ...
  help            Help about any command
  help            Help about any command
  ...
```

## Reproduction

```
$ ./bin/meept --help
```

## Evidence

The `Available Commands` section shows two identical `help` entries.

## Root Cause

The default cobra `help` command is registered, and then a second `help` command is explicitly added somewhere in the command tree (likely in `main.go` or a subcommand file).

## Proposed Fix

Find the explicit `help` command registration and remove it, or use `rootCmd.SetHelpCommand()` to replace the default instead of adding a duplicate.

## Status

**FIXED** -- The `SetHelpCommand(newHelpCmd(rootCmd))` call at `cmd/meept/main.go:129` replaces cobra's default `help` command. The `newHelpCmd` function is NOT added via `AddCommand`, so there is no duplicate entry. The fix was already applied before this issue was documented.

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
