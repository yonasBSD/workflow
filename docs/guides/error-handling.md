# Error Handling & Retries

`wf` provides four mechanisms for dealing with unreliable tasks: `ignore_failure`, `retries`, `retry_delay`, and `timeout`. This guide explains each one and how to combine them.

---

## `ignore_failure` — Treat Failure as Success

When `ignore_failure = true`, a task's non-zero exit code is treated as if it were 0. The task is marked `success` and its dependants continue normally.

```toml
[tasks.send-notification]
cmd            = "curl -X POST $SLACK_WEBHOOK -d '{\"text\": \"Deploy done\"}'"
depends_on     = ["deploy"]
ignore_failure = true    # Slack being down should not abort the workflow
```

**When to use:**

- Notifications, metrics pushes, and other non-critical side effects
- Optional cleanup steps
- Tasks that produce useful output even when they "fail" (e.g., a linter that exits 1 but still writes a report)

**What `ignore_failure` does NOT do:**

- It does not suppress the task's error output — the log file still contains everything
- It does not stop the task from being marked in the audit trail as having a non-zero exit code
- It does not prevent the forensic handler from running if the task is wired with `on_failure` (the handler fires on non-zero exit regardless)

---

## `retries` — Retry on Failure

```toml
[tasks.call-api]
cmd         = "curl https://api.example.com/webhook"
retries     = 3
retry_delay = "10s"
```

The task will be executed up to `retries + 1` times. If it succeeds on any attempt, the task is marked `success` and no further retries occur.

The `retry_delay` is the pause between the end of one attempt and the start of the next. It is context-aware: if the run timeout fires during a retry delay, the delay is cancelled and the task is marked `cancelled`.

**Total possible execution time for the example above:**

```
attempt 1: fails
retry_delay: 10s
attempt 2: fails
retry_delay: 10s
attempt 3: fails
retry_delay: 10s
attempt 4: passes (or fails — 4th attempt is final)

max time = (task_duration × 4) + (10s × 3)
```

---

## `timeout` — Bound Task Duration

```toml
[tasks.integration-test]
cmd     = "pytest tests/integration/"
timeout = "5m"
```

If the task runs longer than the timeout, the process group is killed with `SIGKILL` and the task is marked `failed`. The full timeout details are recorded in the forensic log.

`timeout` and `retries` compose naturally:

```toml
[tasks.flaky-service-check]
cmd         = "curl -f --max-time 10 https://myservice/health"
timeout     = "15s"    # per-attempt limit
retries     = 4
retry_delay = "5s"
```

Each attempt has a 15-second limit. If the attempt times out, it is retried (up to 4 times).

Duration syntax: `30s`, `5m`, `1m30s`, `2h`.

---

## Combining All Four

A production-grade task:

```toml
[tasks.deploy]
name           = "Deploy to Production"
cmd            = "./deploy.sh --env=production --version={{.version}}"
depends_on     = ["smoke-test-staging"]
retries        = 2
retry_delay    = "30s"
timeout        = "10m"
ignore_failure = false    # deploy failure IS fatal
on_failure     = "rollback-production"
```

Breakdown:

- Up to 3 attempts, each bounded to 10 minutes
- 30 seconds between attempts
- If all attempts fail, `rollback-production` forensic task fires

---

## Per-Run Timeout (`--timeout`)

In addition to per-task timeouts, the entire run can be bounded:

```bash
wf run deploy --timeout 30m
```

The run-level timeout cancels all running tasks (via `SIGKILL`) when it fires. Unlike per-task `timeout`, the run timeout is not retried — the run is immediately marked `cancelled`.

---

## Failure Propagation

By default, when a task fails:

1. The task is marked `failed`
2. All tasks that depend on it (directly or transitively) are marked `cancelled`
3. Tasks at the same level that are already running continue to completion
4. The forensic handler fires (if configured)

With `ignore_failure = true`, step 1 becomes `success` and steps 2–4 do not apply.

---

## Best Practices

| Scenario | Recommendation |
|---|---|
| Slack/PagerDuty notifications | `ignore_failure = true` — external services should not abort pipelines |
| External API calls | `retries = 3`, `retry_delay = "10s"`, `timeout = "30s"` per call |
| Database operations | `retries = 1` max — most DB failures are not transient |
| CI tests | `timeout = "10m"` — prevent hanging test suites from blocking the pipeline |
| Deploy tasks | `retries = 2`, `timeout = "15m"`, `on_failure = "rollback"` |
| Health checks | `retries = 5`, `retry_delay = "5s"`, `ignore_failure = true` — eventual consistency |

---

## Inspecting Retry Behaviour

The audit trail shows every attempt:

```bash
wf audit $RUN_ID | grep -E 'task_started|task_failed|task_retrying|task_success'
```

`wf inspect $RUN_ID` shows the attempt count for each task in the task table.
