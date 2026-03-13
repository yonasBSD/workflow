# Example 08 — Log Analysis

**File**: `files/examples/08-log-analysis.toml`
**Industry**: SRE / Observability
**Tags**: `observability`, `logs`, `sre`

## Features Demonstrated

- `matrix` expansion across five services (api, auth, payments, notifications, search)
- Parallel log collection and parsing per service
- `register` for error counts per service
- `if` conditional for SLO breach alert
- `ignore_failure` on collection tasks
- `timeout` per task
- `working_dir` for log storage path

## Why this pattern matters

Log analysis at scale has two problems: collection from many sources is slow if sequential, and the aggregate view (cross-service error correlation) is only useful if the per-service inputs are complete. A matrix-expanded DAG solves both: five parallel collection nodes finish faster, and the correlation task (`correlate-errors`) has a hard dependency on all five parse nodes — it cannot start until every service's error count is registered.

The SLO breach check uses `if` against a registered error rate. This means the alert condition is evaluated against the number the analysis task actually computed and wrote to stdout — not a threshold checked in a cron script that has no access to that context. When the SLO alert fires, `wf audit` shows the full chain: what was collected, what was parsed, what error rate triggered the alert. When it doesn't fire, the run record shows that too — same traceability for passing and failing runs.

## Pipeline Structure

```
[collect-logs[service=api]]        ─┐
[collect-logs[service=auth]]        │
[collect-logs[service=payments]]    ├→ (per service)
[collect-logs[service=notifications]│   [parse-logs[service=*]] → [correlate-errors]
[collect-logs[service=search]]      ┘           ↓
                                          (if error_rate > threshold)
                                          [trigger-alert]
                                          [generate-dashboard]
                                                ↓
                                          [archive-logs]
```

## Run Commands

```bash
# Collect and analyse logs
wf run log-analysis --parallel --print-output

# Work-stealing for five-service fan-out
wf run log-analysis --work-stealing --max-parallel 10 --print-output

# Visualise matrix expansion
wf graph log-analysis --matrix
```

## What to Observe

- Ten matrix nodes total: five `collect-logs` + five `parse-logs`
- `correlate-errors` waits for all ten nodes to complete
- `wf inspect` shows `api_errors`, `auth_errors`, `payments_errors`, `notifications_errors`, `search_errors`
- `trigger-alert` is gated by an `if` condition on error rate
- `generate-dashboard` uses all five error count variables in its command

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag observability --limit 1 | awk 'NR==2{print $1}')
wf inspect $RUN_ID
wf audit   $RUN_ID | grep task_started    # confirm parallel starts

# Diff two analysis runs
RUN_A=$RUN_ID
wf run log-analysis --parallel
RUN_B=$(wf runs --tag observability --limit 1 | awk 'NR==2{print $1}')
wf diff $RUN_A $RUN_B    # compare error counts between runs
```
