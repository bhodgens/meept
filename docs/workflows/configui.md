---
title: Configui
---

# Configui (interactive config editor sections)

Status: In progress. The voice section follows the existing section pattern.

## Overview

Configui renders per-section editors for `meept config <section>`. Each
section file exposes one build function returning a field list; the
section switch in sections.go routes the section name to its builder.

## Problem

Config is JSON5 on disk; operators need a guided editor instead of raw
file edits, with every new schema section surfaced in the same UI.

## Behavior

- sections_<name>.go: one build<Name>Fields() per config section
- sections.go: switch mapping section name -> builder
- A section without both files is invisible or crashes the editor.

## Configuration options

The voice section (leaf 01 of the voice-sidecar plan,
docs/plans/20261102-voice-sidecar/) follows sections_plugins.go:
toggle for enabled, string fields for providers and endpoints,
integer fields for limits. See docs/workflows/voice.md for key meaning.

## Observability

Editor render failures surface in the CLI; no daemon involvement.

## Edge Cases

- Defaults shown are from DefaultConfig(), not the on-disk file.
- New keys must appear in both the section file and the switch case.
