# The Saga Pattern

The Saga pattern is a strategy for managing failures in sequences of operations that each have side effects. When step N fails, you need to undo the effects of steps 1 through N-1. `wf` implements this with `on_failure` forensic tasks.

---

## The Problem

Consider an order processing flow:

```
1. Reserve inventory
2. Charge the customer
3. Create a shipment
4. Send a confirmation email
```

If step 3 fails, you need to:

- Refund the customer (undo step 2)
- Release the inventory reservation (undo step 1)

Without explicit compensation, the customer is charged for an order that was never shipped.

---

## The `wf` Solution

Wire a compensating transaction to each task via `on_failure`:

```toml
name = "order-processing"

[tasks.validate-order]
cmd      = "./validate.sh {{.order_id}}"
register = "validation_status"

[tasks.reserve-inventory]
cmd        = "./reserve.sh {{.order_id}}"
depends_on = ["validate-order"]
register   = "reservation_id"
on_failure = "release-inventory"       # ← compensating tx

[tasks.charge-customer]
cmd        = "./charge.sh --amount={{.order_total}} --reservation={{.reservation_id}}"
depends_on = ["reserve-inventory"]
register   = "charge_id"
on_failure = "refund-customer"         # ← compensating tx

[tasks.create-shipment]
cmd        = "./ship.sh --charge={{.charge_id}}"
depends_on = ["charge-customer"]
register   = "tracking_number"
on_failure = "cancel-shipment"         # ← compensating tx

[tasks.send-confirmation]
cmd            = "./notify.sh --tracking={{.tracking_number}}"
depends_on     = ["create-shipment"]
ignore_failure = true    # notification failure is not fatal

# ── Compensating transactions ────────────────────────────────────────

[tasks.release-inventory]
type           = "forensic"
cmd            = "./release.sh {{.reservation_id}}"
retries        = 2
retry_delay    = "5s"
ignore_failure = true    # don't mask original error if rollback also fails

[tasks.refund-customer]
type           = "forensic"
cmd            = "./refund.sh --charge-id={{.charge_id}}"
retries        = 3
retry_delay    = "10s"
ignore_failure = true

[tasks.cancel-shipment]
type           = "forensic"
cmd            = "./cancel-ship.sh {{.tracking_number}}"
ignore_failure = true
```

---

## Execution Flow

**Happy path:**

```
validate-order → reserve-inventory → charge-customer → create-shipment → send-confirmation
```

**If `charge-customer` fails:**

```
validate-order → reserve-inventory → charge-customer (FAILED)
                                                    ↓
                                              refund-customer (forensic — if charge was attempted)
                                              release-inventory (forensic — because reserve-inventory failed? No.)
```

Wait — this is an important nuance: each `on_failure` handler fires for *that specific task only*. If `charge-customer` fails, `refund-customer` fires (wired to `charge-customer`). But `release-inventory` is wired to `reserve-inventory` — it only fires if `reserve-inventory` itself fails.

**If `create-shipment` fails:**

```
validate-order → reserve-inventory → charge-customer → create-shipment (FAILED)
                                                                       ↓
                                                               cancel-shipment (forensic)
```

Only `cancel-shipment` fires. `refund-customer` and `release-inventory` do **not** fire — those compensations are only needed if their specific predecessors fail.

---

## Global Notification

Wire a workflow-level `on_failure` for a notification that fires regardless of which step failed:

```toml
name       = "order-processing"
on_failure = "notify-ops"

# ... tasks ...

[tasks.notify-ops]
type           = "forensic"
cmd            = """
curl -X POST $PAGERDUTY_URL \
  -d "{\"message\": \"Order processing failed at: {{.failed_task}}\",
       \"details\": \"{{.error_message}}\"}"
"""
ignore_failure = true
timeout        = "30s"
```

Both task-level and workflow-level handlers can coexist — both fire for the same failure event.

---

## Key Design Rules

1. **Always set `ignore_failure = true` on compensating transactions.** If the rollback itself fails, you don't want to mask the original error with a "rollback failed" error.

2. **Add `retries` to compensating transactions.** A refund or inventory release is critical — give it multiple chances to succeed.

3. **Compensating transactions should be idempotent.** If the workflow is resumed, the compensation may be attempted again. Design the compensation script to be safe to call multiple times.

4. **Keep compensating transaction scope narrow.** Each task's `on_failure` should only undo *that specific task's* side effect — not all previous side effects.

5. **Use `{{.failed_task}}` and `{{.error_message}}` for diagnostics.** These are injected automatically and give your notification tasks full context.

---

## Example: Infrastructure Provisioning with Teardown

```toml
name       = "provision-infra"
on_failure = "teardown-everything"

[tasks.provision-network]
cmd        = "terraform apply -target=module.network"
register   = "vpc_id"
on_failure = "teardown-network"
retries    = 1

[tasks.provision-database]
cmd        = "terraform apply -target=module.database"
depends_on = ["provision-network"]
register   = "db_endpoint"
on_failure = "teardown-database"
retries    = 1

[tasks.provision-compute]
cmd        = "terraform apply -target=module.compute"
depends_on = ["provision-database"]
register   = "cluster_arn"
retries    = 1

# ── Teardown handlers ─────────────────────────────────────────────────

[tasks.teardown-network]
type           = "forensic"
cmd            = "terraform destroy -target=module.network -auto-approve"
timeout        = "10m"
ignore_failure = true

[tasks.teardown-database]
type           = "forensic"
cmd            = "terraform destroy -target=module.database -auto-approve"
timeout        = "10m"
ignore_failure = true

[tasks.teardown-everything]
type           = "forensic"
cmd            = """
echo "Tearing down all infrastructure due to: {{.failed_task}}"
terraform destroy -auto-approve
"""
timeout        = "30m"
ignore_failure = true
```

---

## See Also

- [Forensic Tasks concept](../concepts/forensic-tasks.md)
- [Order Processing example](../examples/order-processing.md)
- [Infrastructure Provisioning example](../examples/infra-provisioning.md)
