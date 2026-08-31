# Plan: Skill evolution: improve agent-daemon-benchmarking

## Meta

- plan_id: plan-20260831224606-0035
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is low (0.41) with more negatives (24) than positives (19). The skill lacks clear trigger conditions, concrete steps, and output expectations, leading to inconsistent usage. Adding structured decision logic, explicit preconditions, and concrete deliverables should reduce ambiguity and improve outcomes.

Candidate content:
# Agent Daemon Benchmarking

## Purpose
Systematically benchmark agent daemon performance across multiple runs to establish reliability baselines and detect regressions.

## When to Use
- After agent daemon configuration changes
- Before deploying agent daemons to production
- When investigating intermittent agent behavior
- As part of CI/CD pipeline validation

## Preconditions
- [ ] Agent daemon is installed and configured
- [ ] Benchmark test suite or workload script is available
- [ ] Sufficient disk space for logs and metrics
- [ ] Environment isolation (no competing workloads)

## Steps

1. **Define Benchmark Scope**
   - Identify workloads to test (e.g., concurrent requests, message volume)
   - Set expected success criteria (latency, throughput, error rate)
   - Determine run count (minimum 10 runs recommended)

2. **Prepare Environment**
   - Clear previous benchmark artifacts and logs
   - Reset daemon state to baseline configuration
   - Record environment metadata (OS, versions, resources)

3. **Execute Benchmark Runs**
   - Run daemon under each workload scenario
   - Capture metrics: response time, throughput, memory/CPU usage, error rates
   - Log all runs to timestamped directories for traceability

4. **Aggregate Results**
   - Compute mean, median, min, max, p95, p99 for latency
   - Calculate success rate across all runs
   - Identify outliers and investigate root causes

5. **Generate Report**
   - Summary table of metrics per workload
   - Pass/fail against success criteria
   - Recommendations for optimization or regression flags

## Output Artifacts
- `benchmark_results/<timestamp>/` — per-run logs and metrics
- `benchmark_report.md` — consolidated findings and pass/fail verdict

## Success Criteria
- All latency percentiles within defined thresholds
- Error rate below 1%
- Consistent results across runs (no non-deterministic failures)

## Anti-Patterns to Avoid
- Running benchmarks on shared/noisy environments
- Using insufficient run count (<5)
- Ignoring outlier analysis
- Benchmarking without clear success criteria

## Notes

