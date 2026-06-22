---
name: competitive-teardown
description: Multi-dimension competitive analysis — compare products/technologies across structured criteria
tags:
  - analysis
  - strategy
requires:
  - reasoning
risk_level: low
---

# Competitive Teardown

## Purpose

Systematically compare competing products, technologies, or approaches across multiple dimensions.

## Process

### 1. Define Competitors
- What are we comparing?
- What is the comparison context (use case, market, technical fit)?

### 2. Define Dimensions
Choose 5-10 dimensions relevant to the decision:
- Features
- Performance
- Cost
- Complexity
- Community/support
- Maturity
- Integration
- Documentation quality
- Licensing

### 3. Gather Data
- For each competitor × dimension: gather evidence
- Use `web_search` for external data
- Use `file_read` for codebase evidence
- Cite sources

### 4. Score
- Score each competitor on each dimension (1-5 scale)
- Justify each score with evidence
- Note uncertainty

### 5. Synthesize
- Build the comparison matrix
- Identify the leader per dimension
- Identify the overall leader
- Note trade-offs (no option wins everything)

### 6. Recommend
- State the recommendation and why
- Note what would change the recommendation
- Suggest next steps (POC, deeper evaluation)
