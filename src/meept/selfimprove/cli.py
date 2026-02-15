"""CLI entry point for the self-improvement system.

Provides commands for running individual phases or the full cycle.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import logging
import sys
from pathlib import Path

log = logging.getLogger(__name__)


def setup_logging(verbose: bool = False) -> None:
    """Configure logging for CLI usage."""
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        datefmt="%H:%M:%S",
    )


async def cmd_detect(args: argparse.Namespace) -> int:
    """Run issue detection."""
    from meept.selfimprove.config import DetectionConfig
    from meept.selfimprove.detector import IssueDetector

    config = DetectionConfig()
    detector = IssueDetector(config, project_root=Path(args.project))

    issues = await detector.detect_all()

    if args.json:
        print(json.dumps([i.to_dict() for i in issues], indent=2))
    else:
        print(f"\nFound {len(issues)} issue(s):\n")
        for issue in issues:
            severity = issue.severity.value.upper()
            print(f"  [{severity}] {issue.title}")
            if issue.file_path:
                print(f"         {issue.file_path}:{issue.line_number or '?'}")
            print(f"         {issue.description[:80]}...")
            print()

    return 0 if not issues else 1


async def cmd_analyze(args: argparse.Namespace) -> int:
    """Run root cause analysis."""
    from meept.selfimprove.config import AIInfraConfig
    from meept.selfimprove.analyzer import RootCauseAnalyzer
    from meept.selfimprove.controller import SelfImproveController
    from meept.selfimprove.config import SelfImproveConfig

    config = SelfImproveConfig()
    controller = SelfImproveController(config, project_root=Path(args.project))

    # First detect issues
    issues = await controller.detect()
    if not issues:
        print("No issues detected.")
        return 0

    # Analyze
    analyses = await controller.analyze()

    if args.json:
        print(json.dumps([a.to_dict() for a in analyses], indent=2))
    else:
        print(f"\nAnalyzed {len(analyses)} issue(s):\n")
        for analysis in analyses:
            print(f"  Issue: {analysis.issue_id}")
            print(f"  Root Cause: {analysis.root_cause[:100]}...")
            print(f"  Confidence: {analysis.confidence:.0%}")
            print(f"  Affected Files: {', '.join(analysis.affected_files)}")
            print()

    await controller.cleanup()
    return 0


async def cmd_generate(args: argparse.Namespace) -> int:
    """Generate fix proposals."""
    from meept.selfimprove.controller import SelfImproveController
    from meept.selfimprove.config import SelfImproveConfig

    config = SelfImproveConfig()
    controller = SelfImproveController(config, project_root=Path(args.project))

    # Detect and analyze first
    issues = await controller.detect()
    if not issues:
        print("No issues detected.")
        return 0

    analyses = await controller.analyze()
    if not analyses:
        print("No analyses completed.")
        return 0

    # Generate fixes
    fixes = await controller.generate()

    if args.json:
        print(json.dumps([f.to_dict() for f in fixes], indent=2))
    else:
        print(f"\nGenerated {len(fixes)} fix proposal(s):\n")
        for fix in fixes:
            print(f"  Fix: {fix.id}")
            print(f"  Title: {fix.title}")
            print(f"  Risk: {fix.risk_level.value}")
            print(f"  Confidence: {fix.confidence:.0%}")
            print(f"  Patches: {len(fix.patches)} file(s)")
            for patch in fix.patches:
                print(f"    - {patch.file_path}: {patch.description[:50]}...")
            print()

    await controller.cleanup()
    return 0


async def cmd_validate(args: argparse.Namespace) -> int:
    """Validate generated fixes."""
    from meept.selfimprove.controller import SelfImproveController
    from meept.selfimprove.config import SelfImproveConfig

    config = SelfImproveConfig()
    controller = SelfImproveController(config, project_root=Path(args.project))

    # Run full pipeline up to validation
    issues = await controller.detect()
    if not issues:
        print("No issues detected.")
        return 0

    analyses = await controller.analyze()
    if not analyses:
        print("No analyses completed.")
        return 0

    fixes = await controller.generate()
    if not fixes:
        print("No fixes generated.")
        return 0

    # Validate
    validations = await controller.validate()

    if args.json:
        print(json.dumps([v.to_dict() for v in validations], indent=2))
    else:
        print(f"\nValidated {len(validations)} fix(es):\n")
        for val in validations:
            status = "PASSED" if val.success else "FAILED"
            print(f"  [{status}] Fix: {val.fix_id}")
            print(f"          Tests: {val.tests_passed} passed, {val.tests_failed} failed")
            if val.error_message:
                print(f"          Error: {val.error_message}")
            print()

    await controller.cleanup()
    passed = sum(1 for v in validations if v.success)
    return 0 if passed > 0 else 1


async def cmd_apply(args: argparse.Namespace) -> int:
    """Apply validated fixes."""
    from meept.selfimprove.controller import SelfImproveController
    from meept.selfimprove.config import SelfImproveConfig

    config = SelfImproveConfig()

    # Override approval requirement if --force
    if args.force:
        config.safety.require_human_approval = False

    controller = SelfImproveController(config, project_root=Path(args.project))

    if args.fix_id:
        # Apply a specific fix
        try:
            applied = await controller.apply_fix(args.fix_id, approved_by="human")
            print(f"Applied fix {applied.fix_id} as commit {applied.commit_hash[:8]}")
            return 0
        except Exception as e:
            print(f"Error: {e}")
            return 1
    else:
        print("Specify --fix-id to apply a specific fix, or use 'full-cycle --interactive'")
        return 1


async def cmd_full_cycle(args: argparse.Namespace) -> int:
    """Run the full improvement cycle."""
    from meept.selfimprove.controller import SelfImproveController
    from meept.selfimprove.config import SelfImproveConfig

    config = SelfImproveConfig()
    controller = SelfImproveController(config, project_root=Path(args.project))

    try:
        cycle = await controller.run_full_cycle(interactive=args.interactive)

        if args.json:
            print(json.dumps(cycle.to_dict(), indent=2))
        else:
            print(f"\nImprovement Cycle: {cycle.id}")
            print(f"  Status: {cycle.status}")
            print(f"  Issues Detected: {cycle.issues_detected}")
            print(f"  Issues Analyzed: {cycle.issues_analyzed}")
            print(f"  Fixes Generated: {cycle.fixes_generated}")
            print(f"  Fixes Validated: {cycle.fixes_validated}")
            print(f"  Fixes Applied: {cycle.fixes_applied}")

            if cycle.error:
                print(f"  Error: {cycle.error}")

        return 0 if cycle.status == "completed" else 1

    finally:
        await controller.cleanup()


async def cmd_status(args: argparse.Namespace) -> int:
    """Show current status."""
    from meept.selfimprove.controller import SelfImproveController
    from meept.selfimprove.config import SelfImproveConfig

    config = SelfImproveConfig()
    controller = SelfImproveController(config, project_root=Path(args.project))

    status = controller.get_status()

    if args.json:
        print(json.dumps(status, indent=2))
    else:
        print("\nSelf-Improvement Status:")
        print(f"  Issues: {status['issues_count']}")
        print(f"  Analyses: {status['analyses_count']}")
        print(f"  Fixes: {status['fixes_count']}")
        print(f"  Validations: {status['validations_count']}")
        print(f"  Applied: {status['applied_count']}")
        print(f"  Pending Approvals: {len(status['pending_approvals'])}")
        print(f"  Cycles Completed: {status['cycles_completed']}")

    return 0


async def cmd_regression(args: argparse.Namespace) -> int:
    """Run regression check after self-modification."""
    import subprocess

    print("Running regression tests...")

    # Run pytest
    result = subprocess.run(
        ["python", "-m", "pytest", "tests/", "-v", "--tb=short"],
        cwd=args.project,
    )

    if result.returncode != 0:
        print("\nRegression tests FAILED!")
        return 1

    print("\nRegression tests PASSED!")
    return 0


def main() -> int:
    """Main entry point."""
    parser = argparse.ArgumentParser(
        description="Meept Self-Improvement System",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--project",
        default=".",
        help="Project root directory (default: current directory)",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="Output results as JSON",
    )
    parser.add_argument(
        "-v", "--verbose",
        action="store_true",
        help="Verbose output",
    )

    subparsers = parser.add_subparsers(dest="command", help="Commands")

    # detect
    sub = subparsers.add_parser("detect", help="Detect issues")
    sub.set_defaults(func=cmd_detect)

    # analyze
    sub = subparsers.add_parser("analyze", help="Analyze root causes")
    sub.set_defaults(func=cmd_analyze)

    # generate-fixes
    sub = subparsers.add_parser("generate-fixes", help="Generate fix proposals")
    sub.set_defaults(func=cmd_generate)

    # validate
    sub = subparsers.add_parser("validate", help="Validate fixes in sandbox")
    sub.set_defaults(func=cmd_validate)

    # apply
    sub = subparsers.add_parser("apply", help="Apply validated fixes")
    sub.add_argument("--fix-id", help="Specific fix ID to apply")
    sub.add_argument("--force", action="store_true", help="Skip approval requirement")
    sub.set_defaults(func=cmd_apply)

    # full-cycle
    sub = subparsers.add_parser("full-cycle", help="Run full improvement cycle")
    sub.add_argument("--interactive", action="store_true", help="Prompt for approvals")
    sub.set_defaults(func=cmd_full_cycle)

    # status
    sub = subparsers.add_parser("status", help="Show current status")
    sub.set_defaults(func=cmd_status)

    # regression-check
    sub = subparsers.add_parser("regression-check", help="Run regression tests")
    sub.set_defaults(func=cmd_regression)

    args = parser.parse_args()
    setup_logging(args.verbose)

    if not args.command:
        parser.print_help()
        return 1

    return asyncio.run(args.func(args))


if __name__ == "__main__":
    sys.exit(main())
