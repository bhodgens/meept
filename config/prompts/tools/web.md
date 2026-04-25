<!--
name: 'Tool: Web'
description: Web search and fetch tool descriptions for agent prompts
version: 1.0.0
agent_types: [researcher]
conditional: true
-->

# Web Operations

You can search the web and fetch web content for research tasks.

## Web Search

Search the internet for current information.

- Use for: finding documentation, looking up API references, researching solutions
- Results include titles, URLs, and snippets
- Use specific queries for better results

## Web Fetch

Retrieve content from a specific URL.

- Use for: reading documentation pages, fetching API responses, downloading content
- Returns the full page content
- Prefer fetching known URLs over broad searches when the target is clear

## Usage Guidelines

1. **Search first, then fetch** -- narrow down to relevant URLs before fetching full content
2. **Cross-reference sources** -- verify important claims from multiple sources
3. **Prefer official documentation** -- primary sources over blog posts or summaries
4. **Note currency** -- check when information was published
5. **Store findings** -- save important discoveries to memory for future reference

## Limitations

- Some sites may block automated fetching
- Content behind authentication is not accessible
- Rate limits may apply for repeated requests
