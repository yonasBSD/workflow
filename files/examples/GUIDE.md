# Example Workflows — Usage Guide

Twelve production-grade example workflows that together exercise every implemented feature of the `wf` orchestrator. Build the binary once, then follow the section for each workflow to test specific features.

---

## Quick Start

```bash
# Build
go build -o wf .
./wf --version

# Point wf at the examples directory (choose one method)
export WF_PATHS_WORKFLOWS="$(pwd)/files/examples"
# — OR use a config file —
mkdir -p ~/.config/workflow
echo 'paths:\n  workflows: /path/to/workflow/files/examples' > ~/.config/workflow/config.yaml

# Verify the workflows are discoverable
./wf list
```

---

## Feature Coverage Matrix

| Feature | 01 | 02 | 03 | 04 | 05 | 06 | 07 | 08 | 09 | 10 | 11 | 12 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `depends_on` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `register` (capture output) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `if` conditional | ✓ | ✓ | — | ✓ | ✓ | ✓ | — | ✓ | ✓ | ✓ | ✓ | ✓ |
| `matrix` expansion | — | — | ✓ | — | — | — | ✓ | ✓ | — | — | ✓ | ✓ |
| `ignore_failure` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `retries` + `retry_delay` | ✓ | ✓ | ✓ | — | ✓ | — | ✓ | — | ✓ | — | — | ✓ |
| `timeout` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `env` vars on task | ✓ | ✓ | — | ✓ | ✓ | ✓ | ✓ | — | ✓ | ✓ | ✓ | ✓ |
| `clean_env` | — | — | ✓ | — | — | ✓ | — | — | — | ✓ | — | — |
| `working_dir` | — | ✓ | ✓ | ✓ | — | — | ✓ | ✓ | — | ✓ | ✓ | — |
| `tags` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Task `on_failure` (forensic) | ✓ | — | ✓ | — | ✓ | — | — | — | ✓ | — | — | ✓ |
| Global `on_failure` (forensic) | ✓ | ✓ | — | ✓ | ✓ | — | — | — | — | — | ✓ | — |
| `--parallel` mode | ✓ | ✓ | ✓ | ✓ | — | ✓ | ✓ | ✓ | — | ✓ | ✓ | ✓ |
| `--work-stealing` mode | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `--print-output` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `--var` runtime variables | — | ✓ | — | ✓ | ✓ | ✓ | ✓ | — | — | — | — | ✓ |
| `wf graph` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `wf resume` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `wf export` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `wf diff` | pair any two run IDs | | | | | | | | | | |
| `wf audit` | works on any completed run | | | | | | | | | | |
| `wf runs --tag` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

---

## 01 — CI/CD Pipeline

**Industry**: Platform Engineering / DevOps
**What to test**: full DAG lifecycle, cross-platform parallel builds, forensic trap escalation, conditional gate, per-run timeout

```bash
# Sequential (baseline)
./wf run cicd-pipeline --print-output

# Parallel across platforms (build-linux, build-darwin, build-windows)
./wf run cicd-pipeline --parallel --print-output

# Work-stealing — maximum throughput
./wf run cicd-pipeline --work-stealing --max-parallel 4 --print-output

# Bounded run: abort if entire pipeline takes > 3 minutes
./wf run cicd-pipeline --work-stealing --timeout 3m

# Visualise the DAG
./wf graph cicd-pipeline                    # ASCII DAG to stdout (default)
./wf graph cicd-pipeline --format dot       # Graphviz dot output
./wf graph cicd-pipeline --format html      # writes cicd-pipeline_graph.html

# Inspect task state after completion
./wf runs --tag ci
RUN_ID=$(./wf runs --tag ci --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID
./wf audit   $RUN_ID
./wf export  $RUN_ID --format json > /tmp/cicd-run.json
./wf export  $RUN_ID --format tar  > /tmp/cicd-run.tar.gz
```

**What to observe**:
- `build-linux`, `build-darwin`, `build-windows` all start simultaneously after `test` completes
- `deploy-production` waits for `deploy-staging` to fully succeed
- If any task fails, the workflow-level `on_failure = "alert-oncall"` forensic task fires
- `smoke-test-staging` uses `{{.staging_url}}` — confirm the interpolated URL appears in output

---

## 02 — Nightly Data ETL

**Industry**: Data Engineering
**What to test**: parallel extraction, register + if gate, retries with delay, runtime `--var`

```bash
# Pass runtime variables (controls warehouse target)
./wf run data-etl --var WAREHOUSE_SCHEMA=staging_test --parallel --print-output

# Watch retry behaviour (transform-data has retries=3)
./wf run data-etl --work-stealing --print-output

# Inspect registered variables
RUN_ID=$(./wf runs --tag etl --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID          # shows context variables: crm_rows, erp_rows, analytics_rows, etc.

# Diff two ETL runs to compare row counts
RUN_A=$RUN_ID
./wf run data-etl --var WAREHOUSE_SCHEMA=staging_test --parallel --print-output
RUN_B=$(./wf runs --tag etl --limit 1 | awk 'NR==2{print $1}')
./wf diff $RUN_A $RUN_B
```

**What to observe**:
- `extract-crm`, `extract-erp`, `extract-analytics` all start at level 0 (no deps)
- `send-alert` depends on the `if = 'total_rows < "100"'` condition; confirm it is skipped or executed based on registered value
- `generate-report` uses `{{.total_rows}}` and `{{.warehouse_schema}}` interpolation

---

## 03 — Database Backup (Matrix)

**Industry**: Database Administration
**What to test**: matrix expansion, per-matrix-node forensic traps, clean_env, parallel matrix tasks

```bash
./wf run database-backup --parallel --print-output

# Confirm 3 matrix nodes were created
./wf graph database-backup        # should show backup-dump[db=postgres], [db=mysql], [db=redis]

RUN_ID=$(./wf runs --tag backup --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID              # shows per-db checksum variables
./wf logs    $RUN_ID backup-dump  # log output for a specific task prefix
```

**What to observe**:
- Three `backup-dump` nodes run in parallel: `backup-dump[db=postgres]`, `backup-dump[db=mysql]`, `backup-dump[db=redis]`
- Each matrix node has its own `register` output (`checksum_postgres`, `checksum_mysql`, `checksum_redis`)
- `verify-all` aggregates all three and runs after all matrix nodes complete
- `clean_env = true` means matrix tasks start with an empty environment — confirm no inherited `HOME`, `PATH` side effects

---

## 04 — ML Training Pipeline

**Industry**: Machine Learning / MLOps
**What to test**: three parallel model trainers, conditional model promotion, working_dir, runtime var for experiment tracking

```bash
./wf run ml-training --var EXPERIMENT_NAME=run-$(date +%s) --parallel --print-output --timeout 10m

./wf graph ml-training

RUN_ID=$(./wf runs --tag ml --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID    # shows exp_id, best_f1, champion_model variables
./wf audit   $RUN_ID
```

**What to observe**:
- `train-xgboost`, `train-lightgbm`, `train-tabnet` run in parallel at the same DAG level
- `select-champion` runs only after all three complete and picks the best F1
- `promote-champion` is gated by `if = 'best_f1 != "0.94"'` — check whether it runs based on registered F1
- `deploy-champion` uses `{{.champion_model}}` interpolation

---

## 05 — Infrastructure Provisioning

**Industry**: Cloud / Platform Engineering (Terraform-style)
**What to test**: strict sequential dependency chain, cascading forensic teardown, retries with delays, runtime env vars

```bash
./wf run infra-provisioning --var REGION=us-east-1 --var ENV=staging --work-stealing --print-output

# Observe the teardown chain if a task fails
# The forensic tasks: teardown-network, teardown-database, teardown-all
./wf graph infra-provisioning   # strict chain: network → subnets → db → compute → alb → dns → smoke

RUN_ID=$(./wf runs --tag infrastructure --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID   # vpc_id, rds_endpoint, cluster_arn, alb_dns, etc.
./wf export  $RUN_ID --format json
```

**What to observe**:
- Every step depends on the one before — the graph is a straight chain
- Registered variables (`vpc_id`, `rds_endpoint`) are interpolated into later tasks
- `retries = 2`, `retry_delay = "10s"` on several tasks — inspect `audit` to see attempt counts
- If `provision-network` fails, `teardown-network` fires; if `provision-database` fails, `teardown-database` fires

---

## 06 — Security Audit Pipeline

**Industry**: AppSec / SecOps
**What to test**: six parallel scanners, clean_env for isolation, conditional pentest gate, `--var` for risk threshold

```bash
./wf run security-audit --var PENTEST_TARGET=https://api.staging.example.com --parallel --print-output

# All six scanners independent — should show parallel execution
./wf graph security-audit

RUN_ID=$(./wf runs --tag security --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID   # sast_findings, sca_findings, dast_findings, secrets_findings, etc.
./wf audit   $RUN_ID
```

**What to observe**:
- `scan-sast`, `scan-sca`, `scan-dast`, `scan-secrets`, `scan-network`, `scan-config` all run in parallel
- Each scanner uses `clean_env = true` — no environment leakage between scanners
- `run-pentest` is conditionally gated: only runs when registered risk level passes the `if` condition
- `generate-report` uses `{{.sast_findings}}` etc. — confirm values are interpolated

---

## 07 — Release Management (Matrix: 5 Registries)

**Industry**: Open Source / SaaS DevOps
**What to test**: five-registry matrix, per-registry working_dir, retries, `--var` for release channel

```bash
./wf run release-management --var RELEASE_CHANNEL=stable --parallel --print-output

./wf graph release-management   # shows publish[registry=npm], [registry=pypi], etc.

RUN_ID=$(./wf runs --tag release --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID            # release_version variable
./wf export  $RUN_ID --format tar
```

**What to observe**:
- Five `publish` matrix nodes run in parallel (npm, pypi, dockerhub, ghcr, homebrew)
- `announce-release` runs after all publishes complete and uses `{{.release_version}}`
- `ignore_failure = true` on announcements — Slack/tweet failures don't abort the run
- `retries = 3` on publish tasks — audit logs will show retry attempts

---

## 08 — Log Analysis (Matrix: 5 Services)

**Industry**: Observability / SRE
**What to test**: matrix log collection + parse per service, SLO breach alert condition

```bash
./wf run log-analysis --parallel --print-output

./wf graph log-analysis     # collect[service=api], collect[service=auth], etc.

RUN_ID=$(./wf runs --tag observability --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID        # error counts per service
./wf diff $RUN_ID $(./wf runs --tag observability --limit 2 | awk 'NR==3{print $1}')
```

**What to observe**:
- Five `collect-logs` and five `parse-logs` matrix nodes
- `correlate-errors` depends on all parse nodes — confirm it only starts after all five complete
- `trigger-alert` is gated by an `if` condition on error rate — confirm skip/execute based on registered value
- `generate-dashboard` uses multiple `{{.api_errors}}`, `{{.auth_errors}}` etc. interpolations

---

## 09 — Order Processing (Saga Pattern)

**Industry**: E-Commerce
**What to test**: Saga compensating transactions, strict sequential, per-task forensic traps, retries

```bash
./wf run order-processing --print-output    # sequential only (strict chain)

./wf graph order-processing    # straight chain: validate → payment → inventory → fulfillment → shipping → notify

RUN_ID=$(./wf runs --tag ecommerce --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID    # order_id, payment_id, reservation_id, fulfillment_id, tracking_number
./wf audit   $RUN_ID    # shows forensic trap registrations
./wf logs    $RUN_ID
```

**What to observe**:
- `place-payment` has `on_failure = "refund-payment"` — the Saga compensating transaction
- `reserve-inventory` has `on_failure = "release-inventory"` — another compensating transaction
- `create-fulfillment` has `on_failure = "cancel-fulfillment"`
- `credit-loyalty-points` only runs if `if = 'order_total != "299.99"'` condition passes
- All forensic tasks are `type = "forensic"` and excluded from normal DAG execution

---

## 10 — System Maintenance

**Industry**: SRE / Platform Engineering
**What to test**: six parallel health checks, `register` of system metrics, conditional cert renewal, `clean_env`, `working_dir`

```bash
./wf run system-maintenance --parallel --print-output
./wf run system-maintenance --work-stealing --max-parallel 4 --print-output

./wf graph system-maintenance

RUN_ID=$(./wf runs --tag maintenance --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID     # disk_pct, mem_free_mb, cpu_load, network_status, services_status, min_cert_days
./wf logs    $RUN_ID
./wf export  $RUN_ID --format json
```

**What to observe**:
- All six health checks (`check-disk`, `check-memory`, `check-cpu`, `check-network`, `check-services`, `check-certs`) are independent of each other and run in parallel
- `analyse-health` waits for all six — confirm it only starts after all complete
- `renew-certs` is gated by `if = 'min_cert_days != "12"'` — by default `min_cert_days` registers as `12`, so the cert renewal task is **skipped**
- `maintenance-report` uses all six registered variables — confirm full interpolation in output
- `cleanup-tmp` has `env = {MAX_AGE_DAYS = "7", DRY_RUN = "false"}` — confirm env isolation with `clean_env`

---

## 11 — Compliance Audit (Regulatory Framework Mapping)

**Industry**: Finance / Healthcare / Government
**What to test**: control domain verification, matrix framework mapping (SOC2/ISO27001/HIPAA), coverage gate, forensic escalation on incomplete audit

```bash
./wf run compliance-audit --work-stealing --print-output

# Parallel control verification
./wf run compliance-audit --parallel --max-parallel 4 --print-output

# Visualise — note matrix nodes for three frameworks
./wf graph compliance-audit

RUN_ID=$(./wf runs --tag compliance --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID      # ac_pass, dc_pass, oc_pass, total_pass, coverage_result, report_path
./wf audit   $RUN_ID      # see whether generate-compliance-report ran or was skipped
./wf logs    $RUN_ID verify-data-controls    # see the DC-9 BAA finding
```

**What to observe**:
- `verify-access-controls`, `verify-data-controls`, `verify-operational-controls` all start simultaneously
- Each domain registers an integer pass count — `wf inspect` shows `ac_pass`, `dc_pass`, `oc_pass`
- Matrix expands `map-framework-controls` into three nodes: `[framework=SOC2]`, `[framework=ISO27001]`, `[framework=HIPAA]`
- `generate-compliance-report` is gated by `if = 'coverage_result == "pass"'` — if coverage < 90%, it is **skipped**
- `escalate-incomplete-audit` is `type = "forensic"`, wired to `on_failure` at the workflow level — only fires if the audit cannot complete
- `submit-to-compliance-portal` has `retries = 2` — audit log will show retry events

---

## 12 — Cost Modeling (Pre/Post Deployment)

**Industry**: Engineering / FinOps
**What to test**: cost estimation matrix, budget approval gate, post-deployment measurement, variance analysis, conditional alert

```bash
./wf run cost-modeling --work-stealing --print-output

# Override budget threshold (default 15%)
./wf run cost-modeling --var BUDGET_THRESHOLD=5 --work-stealing --print-output

# Strict threshold — should trigger "requires_approval" decision
./wf run cost-modeling --var BUDGET_THRESHOLD=1 --work-stealing --print-output

# Visualise matrix nodes per environment
./wf graph cost-modeling

RUN_ID=$(./wf runs --tag finops --limit 1 | awk 'NR==2{print $1}')
./wf inspect $RUN_ID    # baseline_monthly_usd, estimated_delta_*, actual_delta_*, budget_decision, variance_status
./wf audit   $RUN_ID    # see what ran and what was skipped
./wf logs    $RUN_ID check-budget-threshold    # see the approval gate decision
./wf logs    $RUN_ID compute-variance          # see the side-by-side variance table
```

**What to observe**:
- `estimate-deployment-cost` expands into three matrix nodes — `[env=dev]`, `[env=staging]`, `[env=prod]` — all run in parallel; each registers `estimated_delta_dev`, `estimated_delta_staging`, `estimated_delta_prod`
- `check-budget-threshold` waits for all three and registers `budget_decision`
- `deploy-infrastructure` has `if = 'budget_decision == "approved"'` — run with `--var BUDGET_THRESHOLD=1` to see it skipped
- `measure-actual-cost` is another three-node matrix — each environment measured in parallel post-deployment
- `alert-cost-variance` only fires `if variance_status == "over_threshold"` — the prod environment has a built-in variance that triggers this
- `preserve-cost-snapshot` is `type = "forensic"` on `deploy-infrastructure` — only runs if deployment fails

---

## Cross-Workflow Commands

These commands work on any completed run regardless of which workflow was used.

```bash
# List runs, filter by tag
./wf runs
./wf runs --tag ci
./wf runs --tag etl --limit 5

# Inspect context variables registered during a run
./wf inspect <run-id>

# View raw task output and logs
./wf logs <run-id>
./wf logs <run-id> <task-id>   # logs for a specific task

# Full forensic audit trail
./wf audit <run-id>

# Diff two runs of the same workflow
./wf diff <run-id-a> <run-id-b>

# Export a complete run record
./wf export <run-id> --format json              # JSON to stdout
./wf export <run-id> --format tar > run.tar.gz  # archive with log files

# Status of a currently-running workflow
./wf status <run-id>

# Resume a failed or interrupted run
./wf resume <run-id>
./wf resume <run-id> --parallel
./wf resume <run-id> --work-stealing

# Health check — verifies database, config, workflows dir
./wf health

# Validate a workflow file without running it
./wf validate cicd-pipeline
./wf validate database-backup

# Visualise any workflow DAG
./wf graph <workflow-name>                      # ASCII DAG to stdout (default)
./wf graph <workflow-name> --format dot         # Graphviz DOT to stdout
./wf graph <workflow-name> --format html        # writes <name>_graph.html — open in browser
```

---

## Execution Mode Comparison

Run the same workflow three ways and compare the results:

```bash
# 1. Sequential (one task at a time)
./wf run system-maintenance --print-output
SEQ_RUN=$(./wf runs --limit 1 | awk 'NR==2{print $1}')

# 2. Parallel (level-locked concurrency)
./wf run system-maintenance --parallel --max-parallel 3 --print-output
PAR_RUN=$(./wf runs --limit 1 | awk 'NR==2{print $1}')

# 3. Work-stealing (dependency-driven, maximum throughput)
./wf run system-maintenance --work-stealing --max-parallel 6 --print-output
WS_RUN=$(./wf runs --limit 1 | awk 'NR==2{print $1}')

# Compare outcomes (status, duration, task order)
./wf diff $SEQ_RUN $PAR_RUN
./wf diff $PAR_RUN $WS_RUN
```

Sequential will show tasks completing one after another. Parallel will show batches completing by DAG level. Work-stealing will show tasks starting as soon as their dependencies clear, regardless of level — this typically yields the shortest wall-clock time.

---

## Resume After Failure

To test `wf resume`, deliberately cause a failure and resume:

```bash
# The renew-certs task in system-maintenance uses: if = 'min_cert_days != "12"'
# min_cert_days registers as "12", so renew-certs is SKIPPED by default.
# To test a real resume scenario, use order-processing which has retries:

./wf run order-processing --print-output
RUN_ID=$(./wf runs --tag ecommerce --limit 1 | awk 'NR==2{print $1}')

# If the run has any failed tasks:
./wf inspect $RUN_ID     # check which tasks succeeded
./wf resume  $RUN_ID     # re-runs only failed/pending tasks; restores context variables
./wf resume  $RUN_ID --parallel
```

On resume, already-succeeded tasks are skipped. Registered variables from the previous run are restored from snapshots and are available for interpolation in resumed tasks.
