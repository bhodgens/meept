---
name: qa-docker
description: QA automation test for recent commits using Docker
arguments:
  - scope: Optional scope (e.g., "api", "ui", "all")
---
# QA Automation: Test Recent Commit

## Objective
Run automated QA tests against the most recent commit using Docker containers for isolation and reproducibility.

## Test Scope
$ARGUMENTS

## Test Plan

### Phase 1: Environment Setup
1. Identify changed files from last commit (`git diff HEAD~1 --name-only`)
2. Determine test scope based on changes
3. Pull required Docker images:
   - `docker compose pull` (if compose.yml exists)
   - Or specify images: postgres:15, redis:7, etc.

### Phase 2: Test Execution
1. Start test containers:
   ```bash
   docker compose up -d test-db test-redis
   ```

2. Run test suite:
   ```bash
   docker compose run --rm tests
   ```

3. Capture output and exit codes

### Phase 3: Reporting
1. Parse test results (JUnit XML if available)
2. Generate summary:
   - Total tests
   - Passed/Failed/Skipped
   - Duration per test suite
3. If failures: extract stack traces and related diffs

### Phase 4: Cleanup
```bash
docker compose down -v
```

## Tools to Use
- `shell_execute` - Git and Docker commands
- `file_read` - Parse test reports
- `memory_write` - Store results for trending

## Output Format
```markdown
# QA Report - {date} - Commit {hash}

## Summary
- Tests: X passed, Y failed, Z skipped
- Duration: N minutes

## Failed Tests
{list with stack traces}

## Recommendations
{actionable items}
```
