# Forensic Tasks & Failure Handling

`wf` implements a first-class failure handling model inspired by the **Saga pattern** from distributed systems. Compensating transactions, rollbacks, and notifications are wired directly into the workflow definition — no external orchestration needed.

---

## Overview

There are two levels of failure handling:

| Level | Field | Fires when |
|---|---|---|
| **Task-level** | `on_failure` on a task | That specific task fails |
| **Workflow-level** | `on_failure` at the top of the TOML | Any task in the workflow fails |

Both point to a task with `type = "forensic"`. Forensic tasks are **excluded from normal DAG execution** — they only run on failure.

---

## Task-Level `on_failure`

Wire a compensating transaction directly to a task:

```toml
[tasks.charge-card]
cmd        = "./charge.sh --amount={{.order_total}}"
depends_on = ["reserve-inventory"]
on_failure = "refund-card"

[tasks.refund-card]
type = "forensic"
cmd  = "./refund.sh --payment-id={{.payment_id}} --reason='charge failed'"
```

If `charge-card` fails, `refund-card` runs immediately. The rest of the workflow is cancelled.

---

## Workflow-Level `on_failure`

A global handler that fires when *any* task fails (unless a task-level handler fires first):

```toml
name       = "deploy"
on_failure = "alert-oncall"

[tasks.alert-oncall]
type = "forensic"
cmd  = """
curl -X POST $PAGERDUTY_WEBHOOK \
  -d '{"message": "Deploy failed: {{.error_message}}", "task": "{{.failed_task}}"}'
"""
```

---

## Forensic Variables

The executor injects special variables into forensic task commands:

| Variable | Scope | Value |
|---|---|---|
| `{{.failed_task}}` | Task-level handlers | ID of the task that failed |
| `{{.error_message}}` | Both levels | stderr/output of the failed task |
| `{{.failed_dag}}` | Workflow-level handler | Name of the workflow |

```toml
[tasks.rollback-db]
type = "forensic"
cmd  = """
echo "Rolling back due to failure in {{.failed_task}}"
echo "Error: {{.error_message}}"
psql -c "ROLLBACK;"
"""
```

---

## The Saga Pattern

In distributed systems, a **Saga** is a sequence of operations where each step has a corresponding compensating transaction. If step N fails, compensating transactions for steps N-1, N-2, … are executed in reverse order.

`wf` implements this naturally with task-level `on_failure` handlers:

```toml
name = "order-processing"

[tasks.reserve-inventory]
cmd        = "./reserve.sh {{.order_id}}"
depends_on = ["validate-order"]
register   = "reservation_id"
on_failure = "release-inventory"

[tasks.charge-customer]
cmd        = "./charge.sh {{.reservation_id}}"
depends_on = ["reserve-inventory"]
register   = "charge_id"
on_failure = "refund-customer"

[tasks.create-shipment]
cmd        = "./ship.sh {{.charge_id}}"
depends_on = ["charge-customer"]
register   = "tracking_number"
on_failure = "cancel-shipment"

# ── Compensating transactions ────────────────────────────

[tasks.release-inventory]
type = "forensic"
cmd  = "./release.sh {{.reservation_id}}"

[tasks.refund-customer]
type = "forensic"
cmd  = "./refund.sh {{.charge_id}}"

[tasks.cancel-shipment]
type = "forensic"
cmd  = "./cancel-ship.sh {{.tracking_number}}"
```

If `create-shipment` fails, only `cancel-shipment` fires (not `refund-customer` or `release-inventory` — those only fire if their specific predecessor fails).

---

## Forensic Task Properties

A forensic task is any task with `type = "forensic"`. All standard task fields apply:

```toml
[tasks.emergency-rollback]
type           = "forensic"
cmd            = "./rollback.sh"
timeout        = "5m"
retries        = 2
retry_delay    = "10s"
ignore_failure = true   # don't fail the handler if rollback itself fails
env            = {ROLLBACK_MODE = "force"}
```

---

## `ignore_failure` on Forensic Tasks

Setting `ignore_failure = true` on a forensic task prevents a failure in the handler from masking the original error. Without it, a failing handler would itself be reported as the final failure — which obscures what actually went wrong.

```toml
[tasks.notify-slack]
type           = "forensic"
cmd            = "curl $SLACK_WEBHOOK -d '{\"text\": \"Deploy failed\"}'"
ignore_failure = true   # Slack being down shouldn't change the run outcome
```

---

## Execution Order

1. A task fails
2. If the task has `on_failure = "X"`, task X runs immediately
3. All remaining normal tasks are cancelled
4. If the workflow has a top-level `on_failure = "Y"`, task Y runs after the run settles
5. The run is marked `failed`

Task-level and workflow-level handlers can coexist — both fire for the same failure.

---

## What Forensic Tasks Cannot Do

- Forensic tasks cannot have `depends_on` pointing to normal tasks (they are excluded from the normal DAG)
- Forensic tasks cannot register variables that downstream *normal* tasks read (there are no downstream normal tasks left after a failure)
- A forensic task cannot itself trigger another forensic task chain
