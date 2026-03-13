# Example 12 — Infrastructure Cost Modeling

**File**: `files/examples/cost-modeling.toml`
**Industry**: Engineering / Finance / FinOps
**Tags**: `finops`, `cost`, `modeling`, `deployment`

## Features Demonstrated

- `register` — captures cost estimates, actual measurements, and computed deltas per environment
- `if` conditional — gates deployment on budget approval and triggers variance alert conditionally
- `matrix` — runs cost estimation and post-deployment measurement across three environments (dev/staging/prod) in parallel
- `on_failure` (task-level) — wires a forensic snapshot task to deployment, preserving cost state for rollback analysis
- `retries` + `retry_delay` — cloud cost API calls may be transient; all measurement tasks retry automatically
- `timeout` — cost API and Terraform calls are time-bounded
- `ignore_failure` — cost reporting tasks must not block the deployment record
- `env` vars — AWS region, cost granularity, FinOps dashboard URL, budget threshold

## Why This Pattern Matters

Infrastructure deployments change your cost structure. Without explicit cost modeling in the deployment workflow itself, teams routinely discover cost surprises in the next billing cycle — after it's too late to act. Embedding cost estimation and post-deployment measurement directly into the deployment workflow means:

- Budget approval happens automatically, based on actual IaC plan output
- Actual vs estimated variance is measured and stored with the run record
- Finance and engineering are alerted to overruns immediately, not at month-end
- The full cost timeline is part of the audit trail alongside the technical deployment record

## Pipeline Structure

```
[snapshot-cost-baseline]
         ↓
[estimate-deployment-cost] × {dev, staging, prod}  ← matrix: IaC plan per env
         ↓
[check-budget-threshold]   ← register: budget_decision ("approved" / "requires_approval")
         ↓  (if budget_decision == "approved")
[deploy-infrastructure]    ← on_failure → [preserve-cost-snapshot]
         ↓
[measure-actual-cost] × {dev, staging, prod}        ← matrix: actual cost delta per env
         ↓
[compute-variance]         ← register: variance_status
         ↓  (if variance_status == "over_threshold")
[alert-cost-variance]
         ↓
[publish-finops-report]

[preserve-cost-snapshot]   ← forensic, fires only if deployment fails
```

## Run Commands

```bash
# Standard run (work-stealing for fastest cost API parallelism)
wf run cost-modeling --work-stealing --print-output

# Override the budget threshold at runtime (default: 15%)
wf run cost-modeling --var BUDGET_THRESHOLD=10 --work-stealing --print-output

# Strict threshold — require sign-off on anything over 5%
wf run cost-modeling --var BUDGET_THRESHOLD=5 --work-stealing --print-output

# With overall timeout ceiling
wf run cost-modeling --work-stealing --timeout 1h --print-output

# Visualise the DAG (note matrix nodes for three environments)
wf graph cost-modeling
wf graph cost-modeling --format html --export cost-modeling.html   # writes HTML file
```

## What to Observe

- `estimate-deployment-cost` expands into three matrix nodes — `[env=dev]`, `[env=staging]`, `[env=prod]` — all running in parallel; each registers `estimated_delta_dev`, `estimated_delta_staging`, `estimated_delta_prod`
- `check-budget-threshold` waits for all three and computes the total delta as a percentage of baseline; registers `budget_decision`
- `deploy-infrastructure` has `if = 'budget_decision == "approved"'` — if the threshold is exceeded, the task is **skipped entirely** (no deployment), which is visible in `wf audit`
- `measure-actual-cost` is another three-node matrix — runs post-deployment to measure the real delta
- `compute-variance` compares estimates to actuals and registers `variance_status`
- `alert-cost-variance` only runs `if variance_status == "over_threshold"` — check `wf audit` to see whether it fired
- `preserve-cost-snapshot` is a `type = "forensic"` task — it only runs if `deploy-infrastructure` fails; it preserves cost context for rollback analysis

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag finops --limit 1 | awk 'NR==2{print $1}')

# See all registered cost variables
wf inspect $RUN_ID

# Chronological event trail — see what ran, what was skipped, if alert fired
wf audit $RUN_ID

# Per-task output
wf logs $RUN_ID check-budget-threshold     # see the approval decision
wf logs $RUN_ID compute-variance           # see the variance table
wf logs $RUN_ID alert-cost-variance        # present only if variance exceeded threshold

# Compare two deployment runs (e.g. before and after a cost optimisation)
wf diff <prev-run-id> $RUN_ID
```

## Adapting for Production

1. **Cost estimation**: Replace the simulated `case` blocks in `estimate-deployment-cost` with real tooling:
   - [Infracost](https://www.infracost.io/) — `infracost breakdown --path ./infra/{{.env}} --format json`
   - [OpenCost](https://opencost.io/) — query the API post-plan
   - Terraform cost estimation via Terraform Cloud / HCP Terraform API
2. **Actual cost measurement**: Replace the simulated deltas in `measure-actual-cost` with:
   - AWS Cost Explorer API (`aws ce get-cost-and-usage`)
   - Azure Cost Management API
   - GCP Cloud Billing API
3. **Budget threshold**: Expose `BUDGET_THRESHOLD` as a per-environment parameter; different thresholds for prod vs non-prod are common
4. **Variance alert routing**: Update `alert-cost-variance` to call your PagerDuty, Slack, or JIRA API
5. **FinOps report**: Update `publish-finops-report` to push data to your cost allocation system (CloudHealth, Apptio, etc.)
6. **Currency**: All dollar amounts in this example are USD monthly deltas; adjust the arithmetic for hourly costs or non-USD currencies
