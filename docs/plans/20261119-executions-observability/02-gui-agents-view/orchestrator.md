# 02 GUI Agents View Redesign - Implementation Branch

> **For the implementing agent:** You are the orchestrator for this branch.
> Test leaves first, then impl. Do NOT commit.

## Meta

- **Parent:** ../master.md
- **Children:** 00T-tests.md, 01-card-redesign.md, 02-sidebar-scroll.md, 03-executions-panel.md, 04-overflow-and-selection.md
- **Scope:** Redesign the agents grid + right-hand sidebar; fix overflow and selection bugs.

## Interface Contracts

### From master (C2, C3, C4)

Tile: 2x icon, 1/2 height, ~30% wider; 8-bit monochrome-green histogram;
active-agent orange pulse; per-card "other concurrent active" counter;
online wording; single scrollable sidebar; executions poll-based list;
Facts panel compact accordion.
