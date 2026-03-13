# Example 10 — System Maintenance

**File**: `files/examples/10-system-maintenance.toml`
**Industry**: SRE / Platform Engineering / Systems Administration
**Tags**: `maintenance`, `daily`, `ops`, `cron`

## Features Demonstrated

- Six parallel health checks (disk, memory, CPU, network, services, certs)
- `register` for every health metric
- `if` conditional on cert expiry for certificate renewal gate
- `clean_env = true` on health check tasks
- `working_dir` for report output
- `env` for maintenance thresholds
- `ignore_failure` on all health checks
- `timeout` on every health check
- `{{.varname}}` interpolation in the final report

## Why this pattern matters

System maintenance scripts typically run as cron jobs that execute checks sequentially and write to a log file. The problems: they take longer than needed (six independent checks run one at a time), they mix operational concerns (a disk check and a cert check have nothing to do with each other), and when one fails, the remaining checks may not run at all.

The six health checks here are fully independent. They run in parallel and all use `ignore_failure` — a failing CPU check should not prevent the cert expiry check from running and registering `min_cert_days`. `clean_env = true` means each health check cannot accidentally pick up state from another. The cert renewal gate uses `if` to check the actual registered expiry value — if the cert has 12 days left, renewal is triggered; if 90 days, it is skipped. Both outcomes are in the run record.

The final maintenance report uses all six registered variables in one template — `{{.disk_pct}}`, `{{.mem_free_mb}}`, `{{.cpu_load}}`, etc. — producing a structured summary that `wf export` can persist as JSON or a tar archive for historical comparison.

## Pipeline Structure

```
[init]
  ├── [check-disk]       → disk_pct
  ├── [check-memory]     → mem_free_mb     (all parallel, all ignore_failure, all clean_env)
  ├── [check-cpu]        → cpu_load
  ├── [check-network]    → network_status
  ├── [check-services]   → services_status
  └── [check-certs]      → min_cert_days
        └── [analyse-health]
              ├── [rotate-logs]
              ├── [cleanup-tmp]
              ├── [vacuum-database]
              ├── [clear-cache]
              ├── [renew-certs]         (if min_cert_days < 30)
              └── [verify-backups]
                    └── [maintenance-report] → [notify-ops]
```

## Run Commands

```bash
# Standard maintenance run
wf run system-maintenance --parallel --print-output

# Work-stealing for maximum throughput
wf run system-maintenance --work-stealing --max-parallel 4 --print-output

# Visualise
wf graph system-maintenance
```

## What to Observe

- All six `check-*` tasks appear with the same timestamp in `wf audit` — confirming parallel execution
- `analyse-health` only starts after all six checks complete
- `wf inspect` shows six health metric variables: `disk_pct`, `mem_free_mb`, `cpu_load`, `network_status`, `services_status`, `min_cert_days`
- `renew-certs` is gated: `if = 'min_cert_days != "12"'` — because `check-certs` registers `12`, `renew-certs` is **skipped** by default. This is by design (the condition is written as a demo; in production: `min_cert_days < "30"`)
- The final report uses all six registered variables — confirm full interpolation in the report output
- `cleanup-tmp` has `env = {MAX_AGE_DAYS = "7", DRY_RUN = "false"}` — combined with `clean_env` on other tasks, shows environment isolation

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag maintenance --limit 1 | awk 'NR==2{print $1}')
wf inspect $RUN_ID             # all health metrics
wf logs    $RUN_ID maintenance-report   # the generated report
wf audit   $RUN_ID | grep skipped       # confirm renew-certs was skipped
wf export  $RUN_ID --format json > maintenance-$(date +%Y%m%d).json
```

## Scheduling as a Cron Job

```cron
# Run daily maintenance at 2:00 AM
0 2 * * * WF_PATHS_WORKFLOWS=/path/to/workflows /usr/local/bin/wf run system-maintenance --parallel >> /var/log/wf-maintenance.log 2>&1
```
