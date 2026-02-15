"""Main orchestrator for the self-improvement cycle.

Coordinates detection, analysis, generation, validation, and
application of fixes in a complete improvement cycle.
"""

from __future__ import annotations

import asyncio
import json
import logging
import uuid
from datetime import UTC, datetime
from pathlib import Path
from typing import TYPE_CHECKING, Any

from meept.selfimprove.analyzer import RootCauseAnalyzer
from meept.selfimprove.applier import ApprovalRequired, ChangeApplier
from meept.selfimprove.detector import IssueDetector
from meept.selfimprove.generator import PatchGenerator
from meept.selfimprove.models import (
    AppliedFix,
    ImprovementCycle,
    Issue,
    ProposedFix,
    RootCauseAnalysis,
    ValidationResult,
)
from meept.selfimprove.validator import FixValidator

if TYPE_CHECKING:
    from meept.core.bus import MessageBus
    from meept.llm.client import LLMClient
    from meept.selfimprove.config import SelfImproveConfig

log = logging.getLogger(__name__)


class SelfImproveController:
    """Orchestrates the full self-improvement cycle.

    Coordinates all components to detect issues, analyze root causes,
    generate fixes, validate them, and apply approved fixes.
    """

    def __init__(
        self,
        config: SelfImproveConfig,
        bus: MessageBus | None = None,
        llm_client: LLMClient | None = None,
        project_root: Path | None = None,
    ) -> None:
        self._config = config
        self._bus = bus
        self._llm_client = llm_client
        self._project_root = project_root or Path.cwd()

        # Initialize components
        self._detector = IssueDetector(config.detection, project_root)
        self._analyzer = RootCauseAnalyzer(config.ai_infra, llm_client, project_root)
        self._generator = PatchGenerator(
            config.ai_infra, config.safety, llm_client, project_root
        )
        self._validator = FixValidator(config.sandbox, config.safety, project_root)
        self._applier = ChangeApplier(config.safety, project_root, bus)

        # State (protected by _state_lock)
        self._state_lock = asyncio.Lock()
        self._current_cycle: ImprovementCycle | None = None
        self._cycles: list[ImprovementCycle] = []
        self._issues: list[Issue] = []
        self._analyses: list[RootCauseAnalysis] = []
        self._fixes: list[ProposedFix] = []
        self._validations: list[ValidationResult] = []
        self._applied: list[AppliedFix] = []
        self._initialized = False

        # Error tracking for circuit breaker
        self._failure_counts: dict[str, int] = {}  # issue_id -> failure count
        self._max_failures_per_issue = 3
        self._consecutive_failures = 0
        self._max_consecutive_failures = 5  # Circuit breaker threshold

    async def initialize(self) -> None:
        """Initialize the controller by loading persisted state."""
        if self._initialized:
            return
        await self._load_state()
        self._initialized = True
        log.info("controller: initialized, loaded %d cycles, %d issues, %d fixes",
                 len(self._cycles), len(self._issues), len(self._fixes))

    async def run_full_cycle(
        self,
        interactive: bool = False,
    ) -> ImprovementCycle:
        """Run a complete improvement cycle.

        Parameters
        ----------
        interactive:
            If True, prompt for approval before applying fixes.

        Returns
        -------
        ImprovementCycle
            The completed cycle record.
        """
        # Ensure controller is initialized
        await self.initialize()

        cycle_id = f"cycle-{uuid.uuid4().hex[:8]}"
        self._current_cycle = ImprovementCycle(id=cycle_id)

        log.info("controller: starting improvement cycle %s", cycle_id)
        await self._publish_status("started", {"cycle_id": cycle_id})

        try:
            # Phase 1: Detection
            log.info("controller: phase 1 - detecting issues")
            await self._publish_status("detecting", {"cycle_id": cycle_id})
            self._issues = await self._detector.detect_all()
            self._current_cycle.issues_detected = len(self._issues)

            if not self._issues:
                log.info("controller: no issues detected")
                self._current_cycle.status = "completed"
                self._current_cycle.completed_at = datetime.now(UTC).isoformat()
                self._cycles.append(self._current_cycle)
                return self._current_cycle

            # Phase 2: Analysis
            log.info("controller: phase 2 - analyzing %d issues", len(self._issues))
            await self._publish_status("analyzing", {
                "cycle_id": cycle_id,
                "issues_count": len(self._issues),
            })

            for issue in self._issues[:self._config.max_iterations_per_cycle]:
                # Check circuit breaker
                if self._check_circuit_breaker():
                    log.warning("controller: stopping analysis due to circuit breaker")
                    break

                # Skip issues that have failed too many times
                if self._should_skip_issue(issue.id):
                    log.info("controller: skipping issue %s (failed %d times)",
                             issue.id, self._failure_counts.get(issue.id, 0))
                    continue

                try:
                    analysis = await self._analyzer.analyze(issue)
                    self._analyses.append(analysis)
                    self._current_cycle.issues_analyzed += 1
                    self._record_success(issue.id)
                except Exception:
                    log.exception("controller: failed to analyze issue %s", issue.id)
                    self._record_failure(issue.id)

            if not self._analyses:
                log.warning("controller: no analyses completed")
                self._current_cycle.status = "completed"
                self._current_cycle.completed_at = datetime.now(UTC).isoformat()
                self._cycles.append(self._current_cycle)
                return self._current_cycle

            # Phase 3: Generation
            log.info("controller: phase 3 - generating fixes for %d analyses", len(self._analyses))
            await self._publish_status("generating", {
                "cycle_id": cycle_id,
                "analyses_count": len(self._analyses),
            })

            for analysis in self._analyses[:self._config.max_fixes_per_cycle]:
                # Check circuit breaker
                if self._check_circuit_breaker():
                    log.warning("controller: stopping generation due to circuit breaker")
                    break

                # Skip analyses for issues that have failed too many times
                if self._should_skip_issue(analysis.issue_id):
                    log.info("controller: skipping generation for %s (failed %d times)",
                             analysis.issue_id, self._failure_counts.get(analysis.issue_id, 0))
                    continue

                try:
                    fix = await self._generator.generate(analysis)
                    if fix is not None:
                        self._fixes.append(fix)
                        self._current_cycle.fixes_generated += 1
                        self._record_success(analysis.issue_id)
                except Exception:
                    log.exception("controller: failed to generate fix for %s", analysis.issue_id)
                    self._record_failure(analysis.issue_id)

            if not self._fixes:
                log.warning("controller: no fixes generated")
                self._current_cycle.status = "completed"
                self._current_cycle.completed_at = datetime.now(UTC).isoformat()
                self._cycles.append(self._current_cycle)
                return self._current_cycle

            # Phase 4: Validation
            log.info("controller: phase 4 - validating %d fixes", len(self._fixes))
            await self._publish_status("validating", {
                "cycle_id": cycle_id,
                "fixes_count": len(self._fixes),
            })

            for fix in self._fixes:
                try:
                    result = await self._validator.validate(fix)
                    self._validations.append(result)
                    if result.success:
                        self._current_cycle.fixes_validated += 1
                except Exception:
                    log.exception("controller: failed to validate fix %s", fix.id)

            # Phase 5: Application
            validated_fixes = [
                (fix, val)
                for fix, val in zip(self._fixes, self._validations)
                if val.success
            ]

            if not validated_fixes:
                log.warning("controller: no fixes passed validation")
                self._current_cycle.status = "completed"
                self._current_cycle.completed_at = datetime.now(UTC).isoformat()
                self._cycles.append(self._current_cycle)
                return self._current_cycle

            log.info("controller: phase 5 - applying %d validated fixes", len(validated_fixes))
            await self._publish_status("applying", {
                "cycle_id": cycle_id,
                "validated_count": len(validated_fixes),
            })

            for fix, validation in validated_fixes:
                try:
                    approved_by = "human" if interactive else "auto"
                    if self._config.safety.require_human_approval:
                        # In interactive mode, assume approval
                        if not interactive:
                            log.info("controller: fix %s requires human approval", fix.id)
                            continue
                    applied = await self._applier.apply(fix, validation, approved_by)
                    self._applied.append(applied)
                    self._current_cycle.fixes_applied += 1
                except ApprovalRequired:
                    log.info("controller: fix %s pending approval", fix.id)
                except Exception:
                    log.exception("controller: failed to apply fix %s", fix.id)

            # Complete
            self._current_cycle.status = "completed"
            self._current_cycle.completed_at = datetime.now(UTC).isoformat()
            self._cycles.append(self._current_cycle)

            log.info(
                "controller: cycle %s completed - detected=%d, analyzed=%d, "
                "generated=%d, validated=%d, applied=%d",
                cycle_id,
                self._current_cycle.issues_detected,
                self._current_cycle.issues_analyzed,
                self._current_cycle.fixes_generated,
                self._current_cycle.fixes_validated,
                self._current_cycle.fixes_applied,
            )

            await self._save_state()
            await self._publish_status("completed", self._current_cycle.to_dict())
            return self._current_cycle

        except Exception as exc:
            log.exception("controller: cycle %s failed", cycle_id)
            if self._current_cycle:
                self._current_cycle.status = "failed"
                self._current_cycle.error = str(exc)
                self._current_cycle.completed_at = datetime.now(UTC).isoformat()
                self._cycles.append(self._current_cycle)
            await self._save_state()
            await self._publish_status("failed", {"cycle_id": cycle_id, "error": str(exc)})
            raise

    async def detect(self) -> list[Issue]:
        """Run only the detection phase."""
        self._issues = await self._detector.detect_all()
        await self._save_state()
        return self._issues

    async def analyze(self, issues: list[Issue] | None = None) -> list[RootCauseAnalysis]:
        """Run only the analysis phase."""
        issues = issues or self._issues
        self._analyses = await self._analyzer.analyze_batch(issues)
        await self._save_state()
        return self._analyses

    async def generate(self, analyses: list[RootCauseAnalysis] | None = None) -> list[ProposedFix]:
        """Run only the generation phase."""
        analyses = analyses or self._analyses
        self._fixes = await self._generator.generate_batch(analyses)
        await self._save_state()
        return self._fixes

    async def validate(self, fixes: list[ProposedFix] | None = None) -> list[ValidationResult]:
        """Run only the validation phase."""
        fixes = fixes or self._fixes
        self._validations = await self._validator.validate_batch(fixes)
        await self._save_state()
        return self._validations

    async def apply_fix(self, fix_id: str, approved_by: str = "human") -> AppliedFix:
        """Apply a specific fix."""
        # Find the fix and validation
        fix = next((f for f in self._fixes if f.id == fix_id), None)
        if fix is None:
            raise ValueError(f"Fix {fix_id} not found")

        validation = next((v for v in self._validations if v.fix_id == fix_id), None)
        if validation is None:
            raise ValueError(f"No validation for fix {fix_id}")

        applied = await self._applier.apply(fix, validation, approved_by)
        self._applied.append(applied)
        await self._save_state()
        return applied

    async def approve_fix(self, fix_id: str) -> AppliedFix:
        """Approve a pending fix."""
        applied = await self._applier.approve(fix_id)
        self._applied.append(applied)
        await self._save_state()
        return applied

    async def reject_fix(self, fix_id: str, reason: str = "") -> None:
        """Reject a pending fix."""
        await self._applier.reject(fix_id, reason)

    async def rollback(self, fix_id: str) -> bool:
        """Rollback an applied fix."""
        applied = next((a for a in self._applied if a.fix_id == fix_id), None)
        if applied is None:
            raise ValueError(f"Applied fix {fix_id} not found")
        return await self._applier.rollback(applied)

    async def cleanup(self) -> None:
        """Clean up resources."""
        await self._validator.cleanup()
        await self._analyzer.close()
        await self._generator.close()

    async def stop(self) -> None:
        """Stop the controller (called during daemon shutdown)."""
        await self.cleanup()

    def _record_failure(self, issue_id: str) -> None:
        """Record a failure for an issue."""
        self._failure_counts[issue_id] = self._failure_counts.get(issue_id, 0) + 1
        self._consecutive_failures += 1
        log.debug("controller: recorded failure for %s (count=%d, consecutive=%d)",
                  issue_id, self._failure_counts[issue_id], self._consecutive_failures)

    def _record_success(self, issue_id: str) -> None:
        """Record a success, resetting the consecutive failure counter."""
        self._consecutive_failures = 0
        # Don't reset per-issue failures - they're historical

    def _should_skip_issue(self, issue_id: str) -> bool:
        """Check if an issue should be skipped due to repeated failures."""
        return self._failure_counts.get(issue_id, 0) >= self._max_failures_per_issue

    def _check_circuit_breaker(self) -> bool:
        """Check if the circuit breaker has tripped (too many consecutive failures)."""
        if self._consecutive_failures >= self._max_consecutive_failures:
            log.warning("controller: circuit breaker tripped after %d consecutive failures",
                        self._consecutive_failures)
            return True
        return False

    def get_status(self) -> dict[str, Any]:
        """Get current status."""
        return {
            "current_cycle": self._current_cycle.to_dict() if self._current_cycle else None,
            "issues_count": len(self._issues),
            "analyses_count": len(self._analyses),
            "fixes_count": len(self._fixes),
            "validations_count": len(self._validations),
            "applied_count": len(self._applied),
            "consecutive_failures": self._consecutive_failures,
            "circuit_breaker_tripped": self._check_circuit_breaker(),
            "failed_issues": {k: v for k, v in self._failure_counts.items() if v > 0},
            "pending_approvals": list(self._applier.pending_approvals.keys()),
            "cycles_completed": len(self._cycles),
        }

    async def _publish_status(self, phase: str, data: dict[str, Any]) -> None:
        """Publish status update to the bus."""
        if self._bus is None:
            return

        from meept.models.messages import BusMessage, MessageType

        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"phase": phase, **data},
            source="selfimprove.controller",
        )
        await self._bus.publish("selfimprove.status", msg)

    async def _save_state(self) -> None:
        """Save current state to disk."""
        async with self._state_lock:
            data_dir = self._config.data_path
            data_dir.mkdir(parents=True, exist_ok=True)

            state = {
                "issues": [i.to_dict() for i in self._issues],
                "analyses": [a.to_dict() for a in self._analyses],
                "fixes": [f.to_dict() for f in self._fixes],
                "validations": [v.to_dict() for v in self._validations],
                "applied": [a.to_dict() for a in self._applied],
                "cycles": [c.to_dict() for c in self._cycles],
                "timestamp": datetime.now(UTC).isoformat(),
            }

            state_file = data_dir / "state.json"
            state_file.write_text(json.dumps(state, indent=2), encoding="utf-8")
            log.debug("controller: saved state to %s", state_file)

    async def _load_state(self) -> None:
        """Load state from disk."""
        state_file = self._config.data_path / "state.json"
        if not state_file.exists():
            log.debug("controller: no state file found at %s", state_file)
            return

        try:
            async with self._state_lock:
                state = json.loads(state_file.read_text(encoding="utf-8"))

                # Deserialize issues
                self._issues = [
                    Issue.from_dict(d) for d in state.get("issues", [])
                ]

                # Deserialize analyses
                self._analyses = [
                    RootCauseAnalysis.from_dict(d) for d in state.get("analyses", [])
                ]

                # Deserialize fixes
                self._fixes = [
                    ProposedFix.from_dict(d) for d in state.get("fixes", [])
                ]

                # Deserialize validations
                self._validations = [
                    ValidationResult.from_dict(d) for d in state.get("validations", [])
                ]

                # Deserialize applied fixes
                self._applied = [
                    AppliedFix.from_dict(d) for d in state.get("applied", [])
                ]

                # Deserialize cycles
                self._cycles = [
                    ImprovementCycle.from_dict(d) for d in state.get("cycles", [])
                ]

            log.info("controller: loaded state from %s", state_file)
        except Exception:
            log.exception("controller: failed to load state")

    async def subscribe_to_bus(self, bus: MessageBus) -> None:
        """Subscribe to bus events for RPC dispatch."""
        from meept.models.messages import BusMessage, MessageType

        self._bus = bus

        # Initialize state before handling any requests
        await self.initialize()

        async def _on_detect(topic: str, msg: BusMessage) -> None:
            try:
                issues = await self.detect()
                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={
                        "issues": [i.to_dict() for i in issues],
                        "count": len(issues),
                    },
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)
            except Exception as exc:
                log.exception("controller: detect failed")
                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={"issues": [], "error": str(exc)},
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)

        async def _on_analyze(topic: str, msg: BusMessage) -> None:
            try:
                analyses = await self.analyze()
                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={
                        "analyses": [a.to_dict() for a in analyses],
                        "count": len(analyses),
                    },
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)
            except Exception as exc:
                log.exception("controller: analyze failed")
                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={"analyses": [], "error": str(exc)},
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)

        async def _on_generate(topic: str, msg: BusMessage) -> None:
            try:
                fixes = await self.generate()
                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={
                        "fixes": [f.to_dict() for f in fixes],
                        "count": len(fixes),
                    },
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)
            except Exception as exc:
                log.exception("controller: generate failed")
                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={"fixes": [], "error": str(exc)},
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)

        async def _on_validate(topic: str, msg: BusMessage) -> None:
            try:
                validations = await self.validate()
                passed = sum(1 for v in validations if v.success)
                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={
                        "validations": [v.to_dict() for v in validations],
                        "passed": passed,
                        "failed": len(validations) - passed,
                    },
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)
            except Exception as exc:
                log.exception("controller: validate failed")
                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={"validations": [], "error": str(exc)},
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)

        async def _on_apply(topic: str, msg: BusMessage) -> None:
            try:
                fix_ids = msg.payload.get("fix_ids", [])
                applied = []
                for fix_id in fix_ids:
                    try:
                        result = await self.approve_fix(fix_id)
                        applied.append(result.to_dict())
                    except Exception:
                        log.exception("controller: failed to apply fix %s", fix_id)

                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={"applied": applied, "count": len(applied)},
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)
            except Exception as exc:
                log.exception("controller: apply failed")
                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={"applied": [], "error": str(exc)},
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)

        async def _on_status(topic: str, msg: BusMessage) -> None:
            status = self.get_status()
            reply = BusMessage(
                type=MessageType.STATUS_UPDATE,
                payload=status,
                source="selfimprove.controller",
                reply_to=msg.id,
            )
            await bus.publish("selfimprove.result", reply)

        async def _on_cycle(topic: str, msg: BusMessage) -> None:
            try:
                interactive = msg.payload.get("interactive", False)
                cycle = await self.run_full_cycle(interactive=interactive)
                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={
                        "cycle_id": cycle.id,
                        "status": "completed",
                        **cycle.to_dict(),
                    },
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)
            except Exception as exc:
                log.exception("controller: cycle failed")
                reply = BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={"status": "failed", "error": str(exc)},
                    source="selfimprove.controller",
                    reply_to=msg.id,
                )
                await bus.publish("selfimprove.result", reply)

        bus.subscribe("selfimprove.detect", _on_detect)
        bus.subscribe("selfimprove.analyze", _on_analyze)
        bus.subscribe("selfimprove.generate", _on_generate)
        bus.subscribe("selfimprove.validate", _on_validate)
        bus.subscribe("selfimprove.apply", _on_apply)
        bus.subscribe("selfimprove.status", _on_status)
        bus.subscribe("selfimprove.cycle", _on_cycle)

        log.info("controller: subscribed to bus events")
