# Example 05 — Infrastructure Provisioning

**File**: `files/examples/infra-provisioning.toml`
**Industry**: Cloud Engineering / Platform Engineering
**Tags**: `infrastructure`, `terraform`, `cloud`

## Features Demonstrated

- Strict sequential dependency chain
- `register` for every provisioned resource ID
- `retries` + `retry_delay` for cloud API calls
- Task-level `on_failure` for targeted teardown
- Global `on_failure` for full teardown
- `timeout` on every provisioning task
- Runtime `--var` for region and environment
- `env` for cloud credentials
- `if` conditional for smoke test gate

## Why this pattern matters

Cloud infrastructure provisioning has strict ordering constraints (you cannot provision subnets before the VPC exists) and expensive partial-failure scenarios (if the database provision fails after the network and subnets are up, you have dangling resources that cost money and create security surface). A plain Terraform apply handles the first; it handles the second only if you remember to write destroy logic.

Every provisioned resource ID is registered as a variable (`vpc_id`, `rds_endpoint`, etc.). If provisioning fails at any step, the forensic teardown task for *that specific step* receives the IDs it needs to undo exactly the resources that were created — no more, no less. The global `on_failure` then triggers full teardown. On resume after a fix, already-succeeded tasks are skipped: the VPC and subnets are not re-created.

## Pipeline Structure

```
[init]
  └── [provision-network]    → vpc_id
        └── [provision-subnets]
              └── [provision-database]   → rds_endpoint
                    └── [provision-compute] → cluster_arn
                          └── [provision-alb]   → alb_dns
                                └── [configure-dns]
                                      └── [smoke-test]
                                            └── [tag-resources]

Each step: on_failure → teardown-<resource>
Global:    on_failure → teardown-all
```

## Run Commands

```bash
# Provision staging environment
wf run infra-provisioning \
  --var REGION=us-east-1 \
  --var ENV=staging \
  --work-stealing \
  --print-output \
  --timeout 45m

# Visualise the dependency chain
wf graph infra-provisioning              # straight sequential chain

# Show forensic teardown tasks
wf graph infra-provisioning --forensic
```

## What to Observe

- The graph is a straight sequential chain — each step depends on the one before
- `wf inspect` shows `vpc_id`, `rds_endpoint`, `cluster_arn`, `alb_dns`, `dns_record` — all registered resources
- Later tasks use `{{.vpc_id}}`, `{{.rds_endpoint}}` etc. — confirm interpolation in logs
- `retries = 2` on cloud resource tasks — `wf audit` shows retry events
- If any provisioning step fails, only that step's teardown fires (not the full teardown)

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag infrastructure --limit 1 | awk 'NR==2{print $1}')
wf inspect $RUN_ID
wf audit   $RUN_ID    # look for variable_registered events
wf export  $RUN_ID --format json
```
