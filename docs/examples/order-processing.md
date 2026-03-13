# Example 09 — Order Processing (Saga Pattern)

**File**: `files/examples/order-processing.toml`
**Industry**: E-Commerce
**Tags**: `ecommerce`, `saga`, `orders`

## Features Demonstrated

- Strict sequential dependency chain (Saga)
- Task-level `on_failure` compensating transactions for every step
- `register` for order_id, payment_id, reservation_id, fulfillment_id, tracking_number
- `retries` + `retry_delay` on payment and fulfillment
- `timeout` per transaction step
- `ignore_failure` on notification tasks
- `if` conditional for loyalty points gate
- `env` for service endpoints

## Why this pattern matters

An e-commerce order touches multiple external systems — payment gateway, inventory service, fulfillment provider, shipping carrier. Each step has a side effect that must be undone if a later step fails. You cannot simply re-run: you cannot charge the customer twice, and you cannot leave a reservation in inventory for an order that was never fulfilled.

The Saga pattern (compensating transactions) is the standard solution. `wf`'s `on_failure` wiring makes it explicit and auditable: `place-payment` is directly wired to `refund-payment`, `reserve-inventory` to `release-inventory`. When a failure occurs, the forensic task runs with `{{.failed_task}}` and `{{.error_message}}` already populated — the compensating transaction knows exactly what happened and to what. Every transaction ID registered before the failure is available for the rollback to use.

## Pipeline Structure

```
[validate-order]
  └── [place-payment]        → payment_id
        └── [reserve-inventory] → reservation_id
              └── [create-fulfillment] → fulfillment_id
                    └── [arrange-shipping] → tracking_number
                          ├── [send-confirmation]   (ignore_failure)
                          └── [credit-loyalty-points] (if order_total > 100)

Compensating transactions (on_failure):
  place-payment        → refund-payment
  reserve-inventory    → release-inventory
  create-fulfillment   → cancel-fulfillment
```

## Run Commands

```bash
# Sequential (strict order required)
wf run order-processing --print-output

# Visualise the chain + forensic handlers
wf graph order-processing
wf graph order-processing --forensic    # shows all compensating transactions

# Visualise as mermaid
wf graph order-processing --format mermaid
```

## What to Observe

- Every task depends on the previous — pure sequential chain
- `wf inspect` shows: `order_id`, `payment_id`, `reservation_id`, `fulfillment_id`, `tracking_number`
- `credit-loyalty-points` is gated by an `if` condition on `order_total`
- The three forensic handlers (`refund-payment`, `release-inventory`, `cancel-fulfillment`) are visible with `wf graph --forensic` but excluded from normal execution
- `send-confirmation` has `ignore_failure = true` — notification failures don't abort the order

## Testing the Saga

To see a compensating transaction fire, modify the TOML to temporarily make a task fail:

```bash
# Edit create-fulfillment to fail:
# cmd = "exit 1"
# Then run:
wf run order-processing --print-output
wf audit $(wf runs --limit 1 | awk 'NR==2{print $1}')
# forensic_triggered event for cancel-fulfillment appears
```

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag ecommerce --limit 1 | awk 'NR==2{print $1}')
wf inspect $RUN_ID      # all transaction IDs
wf audit   $RUN_ID      # chronological order of operations
wf logs    $RUN_ID arrange-shipping   # shipping confirmation output
```
