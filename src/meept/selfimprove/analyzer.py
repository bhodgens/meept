"""Root cause analyzer using LLM for deep reasoning.

Uses claude-opus for thorough analysis of detected issues to identify
the underlying root cause and affected code.
"""

from __future__ import annotations

import logging
import os
from pathlib import Path
from typing import TYPE_CHECKING, Any

from meept.selfimprove.models import Issue, RootCauseAnalysis

if TYPE_CHECKING:
    from meept.llm.client import LLMClient
    from meept.llm.models import ChatMessage
    from meept.selfimprove.config import AIInfraConfig

log = logging.getLogger(__name__)


# Prompt template for root cause analysis
_ANALYSIS_PROMPT = """\
You are an expert software engineer analyzing a bug or issue in a Python codebase.

## Issue Details

**Title:** {title}
**Source:** {source}
**Severity:** {severity}
**Error Type:** {error_type}

**Description:**
{description}

**File:** {file_path}
**Line:** {line_number}

**Stack Trace:**
```
{stack_trace}
```

## Relevant Source Code

{source_code}

## Your Task

Analyze this issue and provide:

1. **Root Cause:** What is the underlying cause of this issue? Be specific about the exact problem.

2. **Affected Files:** List all files that likely need modification to fix this issue.

3. **Affected Functions:** List all functions/methods that need changes.

4. **Confidence:** Rate your confidence in this analysis (0.0-1.0).

5. **Reasoning:** Explain your step-by-step reasoning that led to this conclusion.

6. **Suggested Approach:** Describe at a high level how to fix this issue.

Respond in the following JSON format:
```json
{{
    "root_cause": "...",
    "affected_files": ["file1.py", "file2.py"],
    "affected_functions": ["function_name", "ClassName.method"],
    "confidence": 0.85,
    "reasoning": "Step 1: ... Step 2: ...",
    "suggested_approach": "..."
}}
```
"""


class RootCauseAnalyzer:
    """Analyzes issues to determine their root cause.

    Uses an LLM (preferably claude-opus) for deep reasoning about what
    caused an issue and what needs to change to fix it.
    """

    def __init__(
        self,
        config: AIInfraConfig,
        llm_client: LLMClient | None = None,
        project_root: Path | None = None,
    ) -> None:
        self._config = config
        self._llm_client = llm_client
        self._project_root = project_root or Path.cwd()

    async def analyze(self, issue: Issue) -> RootCauseAnalysis:
        """Perform root cause analysis on a single issue.

        Parameters
        ----------
        issue:
            The issue to analyze.

        Returns
        -------
        RootCauseAnalysis
            The analysis result.
        """
        log.info("analyzer: analyzing issue %s", issue.id)

        # Gather relevant source code
        source_code = await self._gather_source_code(issue)

        # Build the prompt
        prompt = _ANALYSIS_PROMPT.format(
            title=issue.title,
            source=issue.source.value,
            severity=issue.severity.value,
            error_type=issue.error_type or "Unknown",
            description=issue.description,
            file_path=issue.file_path or "Unknown",
            line_number=issue.line_number or "Unknown",
            stack_trace=issue.stack_trace or "N/A",
            source_code=source_code,
        )

        # Call the LLM
        response = await self._call_llm(prompt)

        # Parse the response
        analysis = self._parse_response(issue.id, response)
        analysis.model_used = self._config.analysis_model

        log.info(
            "analyzer: completed analysis for %s (confidence: %.2f)",
            issue.id,
            analysis.confidence,
        )
        return analysis

    async def analyze_batch(self, issues: list[Issue]) -> list[RootCauseAnalysis]:
        """Analyze multiple issues.

        Parameters
        ----------
        issues:
            List of issues to analyze.

        Returns
        -------
        list[RootCauseAnalysis]
            Analysis results for each issue.
        """
        results = []
        for issue in issues:
            try:
                analysis = await self.analyze(issue)
                results.append(analysis)
            except Exception:
                log.exception("analyzer: failed to analyze issue %s", issue.id)
        return results

    async def _gather_source_code(self, issue: Issue) -> str:
        """Gather relevant source code for context.

        Returns source code from the affected file and any related files
        mentioned in the stack trace.
        """
        code_snippets = []

        # Primary file
        if issue.file_path:
            snippet = await self._read_file_snippet(
                issue.file_path,
                issue.line_number,
                context_lines=20,
            )
            if snippet:
                code_snippets.append(f"### {issue.file_path}\n```python\n{snippet}\n```")

        # Extract files from stack trace
        if issue.stack_trace:
            import re
            file_pattern = re.compile(r'File "([^"]+)", line (\d+)')
            for match in file_pattern.finditer(issue.stack_trace):
                file_path = match.group(1)
                line_num = int(match.group(2))

                # Skip system files
                if "site-packages" in file_path or "/python" in file_path.lower():
                    continue

                # Skip if already included
                if any(file_path in s for s in code_snippets):
                    continue

                snippet = await self._read_file_snippet(file_path, line_num, context_lines=10)
                if snippet:
                    code_snippets.append(f"### {file_path}\n```python\n{snippet}\n```")

                # Limit to 5 snippets
                if len(code_snippets) >= 5:
                    break

        return "\n\n".join(code_snippets) if code_snippets else "No source code available."

    async def _read_file_snippet(
        self,
        file_path: str,
        line_number: int | None,
        context_lines: int = 15,
    ) -> str | None:
        """Read a snippet of source code around a specific line."""
        try:
            path = Path(file_path)
            if not path.is_absolute():
                path = self._project_root / path

            if not path.exists():
                return None

            content = path.read_text(encoding="utf-8")
            lines = content.split("\n")

            if line_number is None:
                # Return first 50 lines
                return "\n".join(f"{i+1:4d}: {line}" for i, line in enumerate(lines[:50]))

            start = max(0, line_number - context_lines - 1)
            end = min(len(lines), line_number + context_lines)

            snippet_lines = []
            for i in range(start, end):
                marker = ">>>" if i == line_number - 1 else "   "
                snippet_lines.append(f"{marker} {i+1:4d}: {lines[i]}")

            return "\n".join(snippet_lines)

        except Exception:
            log.debug("analyzer: failed to read file %s", file_path)
            return None

    async def _call_llm(self, prompt: str) -> str:
        """Call the LLM with the analysis prompt."""
        if self._llm_client is None:
            # Try to create a client for ai-infra
            if not self._config.enabled:
                raise RuntimeError("AI-infra is not enabled and no LLM client provided")

            from meept.llm.client import LLMClient
            from meept.llm.models import ModelConfig

            api_key = os.environ.get(self._config.api_key_env, "")
            config = ModelConfig(
                base_url=self._config.base_url,
                model_id=self._config.analysis_model,
                api_key=api_key,
                max_tokens=4096,
                temperature=0.3,
            )
            self._llm_client = LLMClient(config)

        from meept.llm.models import ChatMessage, Role

        messages = [
            ChatMessage(role=Role.USER, content=prompt),
        ]

        response = await self._llm_client.chat(messages, temperature=0.3)
        return response.content or ""

    def _parse_response(self, issue_id: str, response: str) -> RootCauseAnalysis:
        """Parse the LLM response into a RootCauseAnalysis."""
        import json
        import re

        # Try to extract JSON from the response
        json_match = re.search(r"```json\s*\n(.*?)\n```", response, re.DOTALL)
        if json_match:
            json_str = json_match.group(1)
        else:
            # Try to find raw JSON
            json_match = re.search(r"\{[^{}]*\}", response, re.DOTALL)
            json_str = json_match.group(0) if json_match else "{}"

        try:
            data = json.loads(json_str)
        except json.JSONDecodeError:
            log.warning("analyzer: failed to parse JSON response")
            data = {}

        return RootCauseAnalysis(
            issue_id=issue_id,
            root_cause=data.get("root_cause", "Analysis failed"),
            affected_files=data.get("affected_files", []),
            affected_functions=data.get("affected_functions", []),
            confidence=float(data.get("confidence", 0.5)),
            reasoning=data.get("reasoning", response[:1000]),
            suggested_approach=data.get("suggested_approach", ""),
        )

    async def close(self) -> None:
        """Close any resources."""
        if self._llm_client is not None:
            await self._llm_client.close()
