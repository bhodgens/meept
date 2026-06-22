---
name: code-tour
description: Guided codebase walkthrough — explain architecture, key files, and data flow
tags:
  - research
  - code
requires:
  - reasoning
  - code
risk_level: low
---

# Code Tour

## Purpose

Provide a guided walkthrough of a codebase or subsystem, explaining architecture, key files, and data flow.

## Process

### 1. Survey
- Use `list_directory` to map the structure
- Identify entry points (main, cmd/)
- Identify core packages and their responsibilities

### 2. Trace the Request Flow
- Start from the entry point
- Follow a typical request through the system
- Note key interfaces and data transformations

### 3. Identify Patterns
- What architectural patterns are used?
- What conventions are followed?
- Where do new contributors typically get confused?

### 4. Highlight Key Files
- The 5-10 files a newcomer should read first
- Why each file matters
- What to pay attention to

### 5. Present
- Start with the 30-second overview
- Then the component map
- Then the request flow
- End with "if you want to change X, look at Y"
