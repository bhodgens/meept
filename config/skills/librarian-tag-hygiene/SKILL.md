---
name: librarian-tag-hygiene
description: Normalize tags to the controlled vocabulary and propose canonical claims
tags:
  - memory
  - librarian
requires:
  - reasoning
risk_level: low
---

# Tag Hygiene

## Purpose

Normalize tags on epistemic memories to the controlled vocabulary defined in `config/epistemic_tags.json5`, and propose canonical versions of near-duplicate claims.

## Process

### 1. Load Taxonomy
- Read `config/epistemic_tags.json5` for the controlled vocabulary
- Check `~/.meept/epistemic_tags.json5` for user extensions

### 2. Find Non-Standard Tags
- Search for claims with tags not in the taxonomy
- For each: suggest the closest standard tag or propose a new one

### 3. Propose Canonical Claims
- Find near-duplicate claims (similar content, same topic)
- Group them
- For each group: propose one as canonical, offer to supersede others

### 4. Report
- Summary: "Found N non-standard tags, M near-duplicate groups"
- For each item: present the suggestion and ask for user action
- Apply changes only on user approval
