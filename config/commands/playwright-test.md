---
name: playwright-test
description: Playwright E2E testing against recent commit
arguments:
  - url: Optional base URL (default: http://localhost:3000)
---
# Playwright E2E Test Run

## Objective
Execute Playwright end-to-end tests against the application following the most recent commit.

## Configuration
- Base URL: ${1:-http://localhost:3000}
- Scope: $ARGUMENTS

## Test Plan

### Phase 1: Setup
1. Check if Playwright is installed:
   ```bash
   npx playwright --version
   ```

2. Install/update browsers if needed:
   ```bash
   npx playwright install
   ```

3. Verify/Start application server

### Phase 2: Test Execution
1. Run test suite:
   ```bash
   npx playwright test ${1:-}
   ```

2. Options to include:
   - `--reporter=html` for visual report
   - `--video=on-first-retry` for debugging
   - `--trace=on` for detailed traces

### Phase 3: Results Analysis
1. Parse HTML report or console output
2. Extract:
   - Pass/fail counts
   - Failed test names and traces
   - Screenshots/videos of failures

3. Compare with previous run if available

### Phase 4: Artifact Storage
1. Save reports to `test-results/{date}-{hash}/`
2. Link to screenshots and traces
3. Update trending document

## Tools to Use
- `shell_execute` - Playwright CLI commands
- `file_read` - Parse test reports
- `file_find` - Locate test files

## Output Format
```markdown
# Playwright Report - {date}

## Run Info
- Commit: {hash}
- URL: {url}
- Duration: {time}

## Results
| Suite | Passed | Failed | Skipped |
|-------|--------|--------|---------|
| ...   | ...    | ...    | ...     |

## Failures
{detailed list with error messages}

## Artifacts
- [HTML Report](path/to/report.html)
- [Screenshots](path/to/screenshots/)
- [Traces](path/to/traces/)
```
