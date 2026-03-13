# Example 01 — CI/CD Pipeline

**File**: `files/examples/cicd-pipeline.toml`
**Industry**: Platform Engineering / DevOps
**Tags**: `ci`, `deploy`, `prod`

## Features Demonstrated

- Parallel cross-platform builds (Linux, macOS, Windows)
- `register` + `if` conditional deployment gate
- Task-level `on_failure` forensic rollback (staging and production)
- Global `on_failure` for on-call alerting
- `retries` + `retry_delay` on deployment tasks
- `timeout` on every task
- `env` for GOOS/GOARCH
- `clean_env` for build isolation

## Why this pattern matters

A CI/CD pipeline has a dual failure problem: a broken build must be caught early (before it wastes downstream time), but a broken deployment must be rolled back *immediately* (because something is already running in production). A plain shell script can do one or the other — not both, not reliably, and not with a record of what happened.

Using `wf`, the three platform builds run in parallel so a Linux failure surfaces before macOS or Windows waste time. The deployment gate (`if` on smoke test result) is evaluated against a registered variable — the same value the smoke test wrote — not a parsed log file. And if production deploy fails, `rollback-production` fires as a compensating transaction with the full context of the failed run already available as `{{.error_message}}`.

The audit trail produced by `wf audit <run-id>` is also the deployment record — every state transition timestamped and append-only.

## Pipeline Structure

```
[init] → [test] → [build-linux]   ┐
                  [build-darwin]   ├→ [push-image] → [deploy-staging] → [smoke-staging]
                  [build-windows]  ┘                      ↓ (on_failure)
                                                    [rollback-staging]
                                                          ↓ (if gate passes)
                                              [deploy-production] → [smoke-production]
                                                      ↓ (on_failure)
                                               [rollback-production]
```

Global forensic: `[alert-oncall]` — fires on any failure.

## Run Commands

```bash
# Sequential
wf run cicd-pipeline --print-output

# Parallel builds
wf run cicd-pipeline --parallel --print-output

# Maximum throughput
wf run cicd-pipeline --work-stealing --max-parallel 6 --print-output

# With runtime variables
wf run cicd-pipeline \
  --work-stealing \
  --var REGISTRY=ghcr.io/myorg \
  --var TAG=$(git rev-parse --short HEAD) \
  --print-output \
  --timeout 30m

# Dry run — see the plan
wf run cicd-pipeline --dry-run

# Visualise
wf graph cicd-pipeline
wf graph cicd-pipeline --forensic    # show rollback tasks
```

## What to Observe

- `build-linux`, `build-darwin`, `build-windows` all appear with the same timestamp in `wf audit` — confirming parallel execution
- `deploy-production` is gated by an `if` condition — check `wf inspect` to see whether the condition was met or the task was skipped
- `wf inspect` shows `staging_url`, `version`, `git_sha` in the variables section
- If a deployment fails, `rollback-staging` or `rollback-production` fires — visible in `wf audit` as a `forensic_triggered` event

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag ci --limit 1 | awk 'NR==2{print $1}')
wf inspect $RUN_ID       # variables: version, staging_url, etc.
wf audit   $RUN_ID       # full event timeline
wf export  $RUN_ID --format json > /tmp/ci-run.json
```
