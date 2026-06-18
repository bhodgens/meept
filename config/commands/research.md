---
name: research
description: In-depth research with Open Knowledge Format storage
arguments:
  - topic: The research topic or question
---
# Research Task: $ARGUMENTS

## Objective
Conduct thorough research on the topic above and produce a comprehensive report stored in Open Knowledge Format (OKF).

## Research Process

1. **Scope Definition**
   - Identify key questions to answer
   - Define research boundaries and depth

2. **Source Collection**
   - Use web search and firecrawl tools
   - Gather primary and secondary sources
   - Document URLs and timestamps

3. **Analysis**
   - Synthesize findings across sources
   - Identify patterns and contradictions
   - Note confidence levels for claims

4. **Output (OKF Format)**
   Save findings in the following structure:

```
docs/knowledge/{topic}/
├── summary.md          # Executive summary
├── findings/           # Detailed findings
│   ├── finding-001.md
│   └── ...
├── sources.md          # Annotated bibliography
└── metadata.json       # OKF metadata
```

## Tools to Use
- `web_search` - Initial discovery
- `firecrawl_scrape` - Deep content extraction
- `memory_write` - Store findings incrementally
- `file_create` - Generate OKF output files

## Success Criteria
- All claims sourced and timestamped
- Findings stored in OKF-compliant structure
- Summary actionable and self-contained
