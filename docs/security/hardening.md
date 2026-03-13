# Hardening Guide

Checklist for deploying `wf` on production and critical infrastructure.

---

## File System

- [ ] **Restrict the workflows directory** — only the user running `wf` should have write access. Other users should not be able to add or modify workflow files.

    ```bash
    chmod 700 ~/.config/workflow/workflows/
    ```

- [ ] **Verify database permissions** — the SQLite file should be `0600`.

    ```bash
    ls -la ~/.cache/workflow/workflow.db
    # -rw------- 1 alice alice  ...
    ```

- [ ] **Verify log directory permissions** — log directories should be `0700`, log files `0600`.

    ```bash
    ls -la ~/.cache/workflow/logs/
    chmod 700 ~/.cache/workflow/logs/
    ```

- [ ] **Do not run wf as root** — there is no need. Running as root widens the blast radius of any task that misbehaves.

---

## Workflow Files

- [ ] **Review all TOML files before running** — `cmd` strings are executed by the shell. Treat workflow files like code: review them before running, and store them in git.

- [ ] **Use `clean_env = true` for sensitive tasks** — prevents secrets from the parent process environment leaking into task subprocesses.

    ```toml
    [tasks.deploy]
    cmd       = "./deploy.sh"
    clean_env = true
    env       = {DEPLOY_TOKEN = "{{.deploy_token}}"}
    ```

- [ ] **Avoid echoing secrets** — registered variable values (captured via `register`) are stored in the SQLite database. Do not register secrets as variables.

- [ ] **Use `timeout` on every task in production** — an unbounded task can block a workflow indefinitely. Set a realistic upper bound on every task.

    ```toml
    [tasks.long-job]
    cmd     = "./long-running-job.sh"
    timeout = "2h"
    ```

- [ ] **Run `wf validate` in CI** — add a CI step that validates all workflow files on every commit. Catches broken workflows before they reach production.

    ```yaml
    - run: wf validate
    ```

---

## Variables and Interpolation

- [ ] **Do not use `--var` to pass secrets from the shell** — variables passed via `--var` are stored in the database. Use environment variables in `env` blocks for sensitive values, not `--var`.

- [ ] **Understand that `register` captures the last stdout line** — if a task's script outputs a secret as its last line (e.g., for a token exchange), that secret is stored in the database snapshot. Design scripts to avoid this.

---

## Database

- [ ] **Back up the database regularly** — the database is the source of truth for all run history and variable snapshots. Losing it means losing all history and the ability to resume failed runs.

    ```bash
    sqlite3 ~/.cache/workflow/workflow.db ".backup /backups/workflow-$(date +%Y%m%d).db"
    ```

- [ ] **Do not share the database between users** — the database contains run output, registered variables, and forensic logs. Keep it user-local.

---

## Execution Environment

- [ ] **Pin `wf` to a specific version in production** — use a specific git tag or commit. Avoid building from `HEAD` on production machines without review.

- [ ] **Use `retries` conservatively on tasks with side effects** — if a task with `retries = 3` partially completes before failing, each retry may duplicate its side effects. Design idempotent scripts.

- [ ] **Set `--timeout` at the run level for production runs** — even if individual tasks have timeouts, a run-level timeout prevents a pathological case where many retried tasks accumulate.

    ```bash
    wf run deploy --timeout 1h
    ```

---

## Multi-User Environments

- [ ] **Isolate workflows per user** — each user should have their own `WF_PATHS_WORKFLOWS` directory, `WF_PATHS_DATABASE`, and `WF_PATHS_LOGS`. Do not share these between users.

- [ ] **Use separate service accounts for production workflows** — create a dedicated OS user (`wf-prod`) with minimal permissions. Run production workflows under that account.

---

## CI/CD Integration

- [ ] **Validate before deploy** — add `wf validate` to your CI pipeline to catch workflow errors before they reach production.

- [ ] **Export run records for compliance** — use `wf export --format tar` to archive run records for audit purposes.

    ```bash
    wf export $RUN_ID --format tar --output compliance/run-$RUN_ID.tar.gz
    ```

- [ ] **Monitor the health endpoint** — run `wf health` in your monitoring system or as a cron job to detect stale runs and disk pressure early.

---

## What `wf` Does Not Do

These are intentional limitations — understanding them helps you design secure deployments:

- `wf` does **not** sandbox task processes. Tasks run with the same OS-level permissions as the `wf` process itself. If you need sandboxing, run tasks inside Docker containers or with `systemd-run --property=...`.
- `wf` does **not** encrypt the database. Sensitive values registered via `register` are stored in plaintext. Use full-disk encryption (`LUKS`, `FileVault`, `BitLocker`) if the database host needs to be protected at rest.
- `wf` does **not** authenticate CLI users. Anyone with access to the binary and the workflows directory can run workflows. Use OS-level access controls.
- `wf` does **not** enforce network policies. Tasks can make arbitrary network connections. Use OS-level network namespaces or `nftables`/`iptables` rules if network isolation is required.
