"""Issue detection for the self-improvement system.

Scans pytest output, runtime logs, type checking, and linting to find
problems that need investigation.
"""

from __future__ import annotations

import asyncio
import logging
import re
import uuid
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import TYPE_CHECKING

from meept.selfimprove.models import Issue, IssueSeverity, IssueSource

if TYPE_CHECKING:
    from meept.selfimprove.config import DetectionConfig

log = logging.getLogger(__name__)


class IssueDetector:
    """Detects issues from various sources.

    Scans pytest output, runtime logs, mypy type checking, and ruff
    linting to identify problems that may need fixes.
    """

    def __init__(
        self,
        config: DetectionConfig,
        project_root: Path | None = None,
    ) -> None:
        self._config = config
        self._project_root = project_root or Path.cwd()
        self._issues: list[Issue] = []

    async def detect_all(self) -> list[Issue]:
        """Run all enabled detection scans and return found issues."""
        self._issues = []

        tasks = []
        if self._config.scan_pytest:
            tasks.append(self._scan_pytest())
        if self._config.scan_runtime_logs:
            tasks.append(self._scan_runtime_logs())
        if self._config.scan_type_check:
            tasks.append(self._scan_type_check())
        if self._config.scan_lint:
            tasks.append(self._scan_lint())

        if tasks:
            await asyncio.gather(*tasks)

        # Deduplicate by (source, file_path, line_number, error_type)
        seen: set[tuple] = set()
        unique_issues: list[Issue] = []
        for issue in self._issues:
            key = (issue.source, issue.file_path, issue.line_number, issue.error_type)
            if key not in seen:
                seen.add(key)
                unique_issues.append(issue)

        log.info("detector: found %d unique issues", len(unique_issues))
        return unique_issues

    async def detect_pytest(self) -> list[Issue]:
        """Run pytest and return issues for failing tests."""
        self._issues = []
        await self._scan_pytest()
        return self._issues

    async def detect_from_log(self) -> list[Issue]:
        """Scan runtime logs and return issues."""
        self._issues = []
        await self._scan_runtime_logs()
        return self._issues

    async def detect_type_errors(self) -> list[Issue]:
        """Run mypy and return type errors."""
        self._issues = []
        await self._scan_type_check()
        return self._issues

    async def detect_lint_errors(self) -> list[Issue]:
        """Run ruff and return lint errors."""
        self._issues = []
        await self._scan_lint()
        return self._issues

    async def _scan_pytest(self) -> None:
        """Run pytest and parse failures."""
        log.debug("detector: running pytest scan")

        args = ["python", "-m", "pytest", *self._config.pytest_args, "--tb=long"]
        try:
            proc = await asyncio.create_subprocess_exec(
                *args,
                cwd=str(self._project_root),
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.STDOUT,
            )
            stdout, _ = await proc.communicate()
            output = stdout.decode("utf-8", errors="replace")

            if proc.returncode == 0:
                log.debug("detector: all pytest tests passed")
                return

            # Parse pytest output for failures
            self._parse_pytest_output(output)

        except FileNotFoundError:
            log.warning("detector: pytest not found")
        except Exception:
            log.exception("detector: pytest scan failed")

    def _parse_pytest_output(self, output: str) -> None:
        """Parse pytest output and extract failing tests."""
        # Pattern for test failures: FAILED path/to/test.py::test_name
        failed_pattern = re.compile(r"FAILED\s+([\w/._-]+)::(\w+)")

        # Pattern for error details
        error_pattern = re.compile(
            r"([\w/._-]+):(\d+):\s+in\s+(\w+)\s*\n.*?([A-Z]\w*Error|AssertionError):\s*(.+?)(?=\n\n|\Z)",
            re.DOTALL,
        )

        # Simpler pattern for assertion errors
        assert_pattern = re.compile(
            r">\s+assert\s+(.+?)\nE\s+([A-Z]\w*Error|AssertionError):\s*(.+?)(?=\n[^E]|\Z)",
            re.MULTILINE | re.DOTALL,
        )

        # Find failed tests
        for match in failed_pattern.finditer(output):
            file_path = match.group(1)
            test_name = match.group(2)

            # Try to find the specific error for this test
            test_section_pattern = re.compile(
                rf"_{5,}\s+{re.escape(test_name)}\s+_{5,}(.+?)(?=_{5,}|\Z)",
                re.DOTALL,
            )
            test_match = test_section_pattern.search(output)
            test_section = test_match.group(1) if test_match else ""

            # Extract error type and message
            error_type = "AssertionError"
            error_msg = ""
            stack_trace = test_section.strip() if test_section else ""

            for err_match in error_pattern.finditer(test_section):
                error_type = err_match.group(4)
                error_msg = err_match.group(5).strip()
                break

            if not error_msg:
                for assert_match in assert_pattern.finditer(test_section):
                    error_type = assert_match.group(2)
                    error_msg = assert_match.group(3).strip()
                    break

            # Determine severity based on error type
            severity = IssueSeverity.MEDIUM
            if "Critical" in error_type or "Security" in error_type:
                severity = IssueSeverity.CRITICAL
            elif "Error" in error_type:
                severity = IssueSeverity.HIGH

            issue = Issue(
                id=f"pytest-{uuid.uuid4().hex[:8]}",
                source=IssueSource.PYTEST,
                severity=severity,
                title=f"Test failure: {test_name}",
                description=error_msg or f"Test {test_name} failed",
                file_path=file_path,
                test_name=test_name,
                error_type=error_type,
                stack_trace=stack_trace[:2000] if stack_trace else None,
            )
            self._issues.append(issue)

    async def _scan_runtime_logs(self) -> None:
        """Scan runtime logs for errors."""
        log.debug("detector: scanning runtime logs")

        log_path = Path(self._config.log_file).expanduser()
        if not log_path.exists():
            log.debug("detector: log file not found: %s", log_path)
            return

        try:
            cutoff = datetime.now(UTC) - timedelta(hours=self._config.log_lookback_hours)
            content = log_path.read_text(encoding="utf-8", errors="replace")

            # Pattern for log lines with errors/exceptions
            error_pattern = re.compile(
                r"(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+\[ERROR\]\s+(\S+):\s+(.+?)(?=\n\d{4}|\Z)",
                re.DOTALL,
            )
            exception_pattern = re.compile(
                r"(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+\[(?:ERROR|WARNING)\]\s+(\S+):\s+.*?([A-Z]\w*(?:Error|Exception)):\s*(.+?)(?=\n\d{4}|\Z)",
                re.DOTALL,
            )

            for match in exception_pattern.finditer(content):
                timestamp_str = match.group(1)
                module = match.group(2)
                error_type = match.group(3)
                message = match.group(4).strip()

                # Check timestamp is within lookback window
                try:
                    timestamp = datetime.strptime(timestamp_str, "%Y-%m-%d %H:%M:%S")
                    timestamp = timestamp.replace(tzinfo=UTC)
                    if timestamp < cutoff:
                        continue
                except ValueError:
                    pass

                severity = IssueSeverity.MEDIUM
                if "Critical" in error_type:
                    severity = IssueSeverity.CRITICAL
                elif "Error" in error_type:
                    severity = IssueSeverity.HIGH

                issue = Issue(
                    id=f"log-{uuid.uuid4().hex[:8]}",
                    source=IssueSource.RUNTIME_LOG,
                    severity=severity,
                    title=f"{error_type} in {module}",
                    description=message[:500],
                    error_type=error_type,
                    metadata={"module": module, "log_timestamp": timestamp_str},
                )
                self._issues.append(issue)

            # Also catch generic ERROR lines
            for match in error_pattern.finditer(content):
                timestamp_str = match.group(1)
                module = match.group(2)
                message = match.group(3).strip()

                # Skip if already captured as exception
                if any(
                    i.metadata.get("module") == module
                    and i.metadata.get("log_timestamp") == timestamp_str
                    for i in self._issues
                ):
                    continue

                try:
                    timestamp = datetime.strptime(timestamp_str, "%Y-%m-%d %H:%M:%S")
                    timestamp = timestamp.replace(tzinfo=UTC)
                    if timestamp < cutoff:
                        continue
                except ValueError:
                    pass

                issue = Issue(
                    id=f"log-{uuid.uuid4().hex[:8]}",
                    source=IssueSource.RUNTIME_LOG,
                    severity=IssueSeverity.MEDIUM,
                    title=f"Error in {module}",
                    description=message[:500],
                    metadata={"module": module, "log_timestamp": timestamp_str},
                )
                self._issues.append(issue)

        except Exception:
            log.exception("detector: failed to scan runtime logs")

    async def _scan_type_check(self) -> None:
        """Run mypy and parse type errors."""
        log.debug("detector: running type check scan")

        args = ["python", "-m", "mypy", "src/", *self._config.mypy_args]
        try:
            proc = await asyncio.create_subprocess_exec(
                *args,
                cwd=str(self._project_root),
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.STDOUT,
            )
            stdout, _ = await proc.communicate()
            output = stdout.decode("utf-8", errors="replace")

            if proc.returncode == 0:
                log.debug("detector: no type errors")
                return

            # Parse mypy output: path:line: error: message
            pattern = re.compile(r"^([^:]+):(\d+):\s*(error|warning):\s*(.+)$", re.MULTILINE)

            for match in pattern.finditer(output):
                file_path = match.group(1)
                line_num = int(match.group(2))
                level = match.group(3)
                message = match.group(4)

                severity = IssueSeverity.MEDIUM if level == "error" else IssueSeverity.LOW

                issue = Issue(
                    id=f"mypy-{uuid.uuid4().hex[:8]}",
                    source=IssueSource.TYPE_CHECK,
                    severity=severity,
                    title=f"Type error in {Path(file_path).name}:{line_num}",
                    description=message,
                    file_path=file_path,
                    line_number=line_num,
                    error_type="TypeError",
                )
                self._issues.append(issue)

        except FileNotFoundError:
            log.warning("detector: mypy not found")
        except Exception:
            log.exception("detector: type check scan failed")

    async def _scan_lint(self) -> None:
        """Run ruff and parse lint errors."""
        log.debug("detector: running lint scan")

        args = ["ruff", "check", "src/", "--output-format=json", *self._config.ruff_args]
        try:
            proc = await asyncio.create_subprocess_exec(
                *args,
                cwd=str(self._project_root),
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, _ = await proc.communicate()

            if proc.returncode == 0:
                log.debug("detector: no lint errors")
                return

            import json

            try:
                violations = json.loads(stdout.decode("utf-8"))
            except json.JSONDecodeError:
                log.warning("detector: failed to parse ruff output")
                return

            for violation in violations:
                file_path = violation.get("filename", "")
                line_num = violation.get("location", {}).get("row", 0)
                code = violation.get("code", "")
                message = violation.get("message", "")

                # Determine severity from rule code
                severity = IssueSeverity.LOW
                if code.startswith("E") or code.startswith("F"):
                    severity = IssueSeverity.MEDIUM
                if code.startswith("S"):  # Security rules
                    severity = IssueSeverity.HIGH

                issue = Issue(
                    id=f"ruff-{uuid.uuid4().hex[:8]}",
                    source=IssueSource.LINT,
                    severity=severity,
                    title=f"Lint: {code} in {Path(file_path).name}",
                    description=message,
                    file_path=file_path,
                    line_number=line_num,
                    error_type=code,
                    metadata={"rule_code": code},
                )
                self._issues.append(issue)

        except FileNotFoundError:
            log.warning("detector: ruff not found")
        except Exception:
            log.exception("detector: lint scan failed")
