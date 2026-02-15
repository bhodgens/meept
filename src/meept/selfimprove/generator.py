"""Patch generator using LLM for code fix generation.

Uses claude-sonnet for fast, high-quality code generation to create
patches that fix identified issues.
"""

from __future__ import annotations

import fnmatch
import logging
import os
import uuid
from pathlib import Path
from typing import TYPE_CHECKING

from meept.selfimprove.models import (
    FilePatch,
    ProposedFix,
    RiskLevel,
    RootCauseAnalysis,
)

if TYPE_CHECKING:
    from meept.llm.client import LLMClient
    from meept.selfimprove.config import SafetyConfig, AIInfraConfig

log = logging.getLogger(__name__)


_GENERATION_PROMPT = """\
You are an expert software engineer fixing a bug in a Python codebase.

## Root Cause Analysis

**Root Cause:** {root_cause}

**Reasoning:** {reasoning}

**Suggested Approach:** {suggested_approach}

## Files to Modify

{affected_files}

## Current Source Code

{source_code}

## Your Task

Generate specific code patches to fix this issue. For each file that needs changes:

1. Identify the exact lines that need modification
2. Provide the original content (for verification)
3. Provide the new content
4. Explain what the change does

Important guidelines:
- Make minimal, focused changes - only fix what's broken
- Preserve existing code style and formatting
- Don't add unnecessary error handling or logging
- Don't refactor unrelated code
- Make sure the fix is complete and doesn't introduce new issues

Respond in the following JSON format:
```json
{{
    "title": "Brief description of the fix",
    "description": "Detailed explanation of what the fix does",
    "risk_level": "low|medium|high|critical",
    "confidence": 0.85,
    "reasoning": "Why this fix will work",
    "patches": [
        {{
            "file_path": "path/to/file.py",
            "original_content": "exact content being replaced",
            "new_content": "new content to insert",
            "start_line": 42,
            "end_line": 45,
            "description": "What this specific change does"
        }}
    ],
    "tests_to_run": ["test_file.py::test_name"],
    "rollback_instructions": "How to undo this fix if needed"
}}
```

Risk level guidelines:
- **low**: Simple fix, well-understood, unlikely to cause issues
- **medium**: Moderate change, may affect related functionality
- **high**: Significant change, could have side effects
- **critical**: Core functionality, security implications, or high blast radius
"""


class PatchGenerator:
    """Generates code patches to fix identified issues.

    Uses an LLM (preferably claude-sonnet) for fast, accurate code
    generation based on root cause analysis.
    """

    def __init__(
        self,
        ai_config: AIInfraConfig,
        safety_config: SafetyConfig,
        llm_client: LLMClient | None = None,
        project_root: Path | None = None,
    ) -> None:
        self._ai_config = ai_config
        self._safety_config = safety_config
        self._llm_client = llm_client
        self._project_root = project_root or Path.cwd()

    async def generate(
        self,
        analysis: RootCauseAnalysis,
        issue_ids: list[str] | None = None,
    ) -> ProposedFix | None:
        """Generate a fix proposal based on root cause analysis.

        Parameters
        ----------
        analysis:
            The root cause analysis to base the fix on.
        issue_ids:
            Optional list of issue IDs this fix addresses.

        Returns
        -------
        ProposedFix | None
            The proposed fix, or None if generation failed.
        """
        log.info("generator: generating fix for analysis %s", analysis.issue_id)

        # Check confidence threshold
        if analysis.confidence < self._safety_config.min_confidence_threshold:
            log.warning(
                "generator: skipping low-confidence analysis (%.2f < %.2f)",
                analysis.confidence,
                self._safety_config.min_confidence_threshold,
            )
            return None

        # Check for blocked paths
        blocked_files = self._filter_blocked_files(analysis.affected_files)
        if blocked_files:
            log.warning(
                "generator: blocked files in affected list: %s",
                blocked_files,
            )

        # Get source code for affected files
        source_code = await self._gather_source_code(analysis.affected_files)

        # Build the prompt
        prompt = _GENERATION_PROMPT.format(
            root_cause=analysis.root_cause,
            reasoning=analysis.reasoning,
            suggested_approach=analysis.suggested_approach,
            affected_files="\n".join(f"- {f}" for f in analysis.affected_files),
            source_code=source_code,
        )

        # Call the LLM
        response = await self._call_llm(prompt)

        # Parse the response
        fix = self._parse_response(
            analysis.issue_id,
            issue_ids or [analysis.issue_id],
            response,
        )

        if fix is None:
            return None

        # Apply safety checks
        fix = self._apply_safety_checks(fix)

        log.info(
            "generator: created fix %s with %d patches",
            fix.id,
            len(fix.patches),
        )
        return fix

    async def generate_batch(
        self,
        analyses: list[RootCauseAnalysis],
    ) -> list[ProposedFix]:
        """Generate fixes for multiple analyses.

        Parameters
        ----------
        analyses:
            List of root cause analyses.

        Returns
        -------
        list[ProposedFix]
            Generated fixes (may be fewer than analyses if some fail).
        """
        fixes = []
        for analysis in analyses:
            try:
                fix = await self.generate(analysis)
                if fix is not None:
                    fixes.append(fix)
            except Exception:
                log.exception("generator: failed to generate fix for %s", analysis.issue_id)
        return fixes

    def _filter_blocked_files(self, files: list[str]) -> list[str]:
        """Return files that match blocked path patterns."""
        blocked = []
        for file_path in files:
            for pattern in self._safety_config.blocked_paths:
                if fnmatch.fnmatch(file_path, pattern):
                    blocked.append(file_path)
                    break
        return blocked

    async def _gather_source_code(self, files: list[str]) -> str:
        """Gather source code from affected files."""
        snippets = []

        for file_path in files:
            # Skip blocked files
            if self._filter_blocked_files([file_path]):
                continue

            try:
                path = Path(file_path)
                if not path.is_absolute():
                    path = self._project_root / path

                if not path.exists():
                    continue

                content = path.read_text(encoding="utf-8")
                lines = content.split("\n")

                # Include line numbers
                numbered = "\n".join(f"{i+1:4d}: {line}" for i, line in enumerate(lines))
                snippets.append(f"### {file_path}\n```python\n{numbered}\n```")

            except Exception:
                log.debug("generator: failed to read file %s", file_path)

        return "\n\n".join(snippets) if snippets else "No source code available."

    async def _call_llm(self, prompt: str) -> str:
        """Call the LLM with the generation prompt."""
        if self._llm_client is None:
            if not self._ai_config.enabled:
                raise RuntimeError("AI-infra is not enabled and no LLM client provided")

            from meept.llm.client import LLMClient
            from meept.llm.models import ModelConfig

            api_key = os.environ.get(self._ai_config.api_key_env, "")
            config = ModelConfig(
                base_url=self._ai_config.base_url,
                model_id=self._ai_config.generation_model,
                api_key=api_key,
                max_tokens=4096,
                temperature=0.2,
            )
            self._llm_client = LLMClient(config)

        from meept.llm.models import ChatMessage, Role

        messages = [
            ChatMessage(role=Role.USER, content=prompt),
        ]

        response = await self._llm_client.chat(messages, temperature=0.2)
        return response.content or ""

    def _parse_response(
        self,
        analysis_id: str,
        issue_ids: list[str],
        response: str,
    ) -> ProposedFix | None:
        """Parse the LLM response into a ProposedFix."""
        import json
        import re

        # Try to extract JSON from the response
        json_match = re.search(r"```json\s*\n(.*?)\n```", response, re.DOTALL)
        if json_match:
            json_str = json_match.group(1)
        else:
            json_match = re.search(r"\{.*\}", response, re.DOTALL)
            json_str = json_match.group(0) if json_match else "{}"

        try:
            data = json.loads(json_str)
        except json.JSONDecodeError:
            log.warning("generator: failed to parse JSON response")
            return None

        if not data.get("patches"):
            log.warning("generator: no patches in response")
            return None

        # Parse patches
        patches = []
        for p in data.get("patches", []):
            try:
                patch = FilePatch(
                    file_path=p["file_path"],
                    original_content=p["original_content"],
                    new_content=p["new_content"],
                    start_line=int(p.get("start_line", 1)),
                    end_line=int(p.get("end_line", 1)),
                    description=p.get("description", ""),
                )
                patches.append(patch)
            except (KeyError, ValueError) as exc:
                log.warning("generator: invalid patch: %s", exc)

        if not patches:
            return None

        # Parse risk level
        risk_str = data.get("risk_level", "medium").lower()
        try:
            risk_level = RiskLevel(risk_str)
        except ValueError:
            risk_level = RiskLevel.MEDIUM

        return ProposedFix(
            id=f"fix-{uuid.uuid4().hex[:8]}",
            issue_ids=issue_ids,
            title=data.get("title", f"Fix for {analysis_id}"),
            description=data.get("description", ""),
            patches=patches,
            risk_level=risk_level,
            confidence=float(data.get("confidence", 0.5)),
            reasoning=data.get("reasoning", ""),
            tests_to_run=data.get("tests_to_run", []),
            rollback_instructions=data.get("rollback_instructions", ""),
            model_used=self._ai_config.generation_model,
        )

    def _apply_safety_checks(self, fix: ProposedFix) -> ProposedFix:
        """Apply safety guardrails to a proposed fix."""
        # Filter out patches to blocked files
        safe_patches = []
        for patch in fix.patches:
            blocked = self._filter_blocked_files([patch.file_path])
            if blocked:
                log.warning(
                    "generator: removing patch to blocked file: %s",
                    patch.file_path,
                )
                continue
            safe_patches.append(patch)

        # Check file count limit
        if len(safe_patches) > self._safety_config.max_files_per_fix:
            log.warning(
                "generator: too many files (%d > %d), truncating",
                len(safe_patches),
                self._safety_config.max_files_per_fix,
            )
            safe_patches = safe_patches[:self._safety_config.max_files_per_fix]

        # Check total lines changed
        total_lines = sum(
            len(p.new_content.split("\n")) + len(p.original_content.split("\n"))
            for p in safe_patches
        )
        if total_lines > self._safety_config.max_lines_changed_per_fix:
            log.warning(
                "generator: too many lines changed (%d > %d)",
                total_lines,
                self._safety_config.max_lines_changed_per_fix,
            )
            # Elevate risk level
            if fix.risk_level == RiskLevel.LOW:
                fix.risk_level = RiskLevel.MEDIUM
            elif fix.risk_level == RiskLevel.MEDIUM:
                fix.risk_level = RiskLevel.HIGH

        # Block critical risk if configured
        if (
            fix.risk_level == RiskLevel.CRITICAL
            and self._safety_config.block_critical_risk
        ):
            log.warning("generator: blocking critical risk fix")
            safe_patches = []

        # Check if risk level is allowed
        if fix.risk_level.value not in self._safety_config.allowed_risk_levels:
            log.warning(
                "generator: risk level %s not allowed",
                fix.risk_level.value,
            )
            safe_patches = []

        # Return modified fix
        return ProposedFix(
            id=fix.id,
            issue_ids=fix.issue_ids,
            title=fix.title,
            description=fix.description,
            patches=safe_patches,
            risk_level=fix.risk_level,
            confidence=fix.confidence,
            reasoning=fix.reasoning,
            tests_to_run=fix.tests_to_run,
            rollback_instructions=fix.rollback_instructions,
            timestamp=fix.timestamp,
            model_used=fix.model_used,
        )

    async def close(self) -> None:
        """Close any resources."""
        if self._llm_client is not None:
            await self._llm_client.close()
