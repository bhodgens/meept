---
name: dossier
description: Long-running profile accumulation — build a comprehensive dossier on a topic over time
tags:
  - research
  - methodology
requires:
  - reasoning
risk_level: low
---

# Dossier

## Purpose

Accumulate information on a topic over multiple sessions, building a comprehensive profile. Unlike a one-shot research task, a dossier grows over time.

## Process

### 1. Initialize
- Create a memory entry tagged "dossier:<topic>"
- Define sections: background, key players, timeline, analysis, open questions

### 2. Accumulate
- Each time new information arrives, update the dossier
- Use `retain` to store new facts with the dossier tag
- Cross-reference with existing entries

### 3. Periodic Review
- Review the dossier for consistency
- Update sections as new information arrives
- Retire outdated entries (mark as superseded)

### 4. Export
- When requested, compile the dossier into a document
- Include all sources, timeline, and analysis
