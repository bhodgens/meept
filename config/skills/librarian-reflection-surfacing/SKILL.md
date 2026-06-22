---
name: librarian-reflection-surfacing
description: Surface reflection themes, contradictions, and auto-claim candidates to the user
tags:
  - memory
  - librarian
requires:
  - reasoning
risk_level: low
---

# Reflection Surfacing (Path D)

## Purpose

Run the deepened `reflect` tool and present findings to the user in an actionable format.

## Process

### 1. Run Reflect
- Call `reflect` with an open-ended prompt for the period
- The tool returns: themes, contradictions, potential_contradictions, supersessions, pending_reviews, auto_candidates, open_questions

### 2. Present Themes
For each theme:
- Name and summary
- Memory IDs involved
- Ask: "Want to drill into this theme?"

### 3. Surface Contradictions
For each contradiction:
- Show both claims (old and new)
- Show the edge explanation
- Offer: supersede / investigate / dismiss

### 4. Surface Pending Reviews
For each pending decision/prediction:
- Show the original call/forecast
- Ask: "What actually happened?" → record via `record_review` or `mark_resolved`

### 5. Drive Promotion Pipeline
For each auto-claim candidate:
- Show the claim text and detected confidence
- Offer: promote / reject / edit / skip

### 6. Identify Unrecorded Assertions
Scan recent conversation for assertions that haven't been recorded as claims.
- Present as: "You said X on DATE — record as claim?"
- On user approval, write as `status=auto`
