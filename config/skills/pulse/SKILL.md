---
name: pulse
description: Recurring topic monitoring — check for updates on a topic on a schedule
tags:
  - research
  - scheduler
requires:
  - reasoning
risk_level: low
---

# Pulse

## Purpose

Monitor a topic for new developments on a recurring schedule. When triggered by the scheduler, perform a focused search and report changes since the last pulse.

## Process

### 1. Configure
- Define the topic and search queries
- Set the schedule (daily, weekly, monthly)
- Define what counts as a "new development"

### 2. Execute (on schedule)
- Run the search queries
- Compare results against the last pulse (stored in memory)
- Identify new, changed, and removed items

### 3. Report
- Summarize changes since last pulse
- Flag significant developments
- Store this pulse as the new baseline

### 4. Escalate
- If a significant change is detected, suggest deeper investigation
- Offer to update the dossier if one exists for this topic
