# Triggers (Roadmap)

!!! note "Roadmap feature"
    Trigger types (`cron`, `file_watch`, `webhook`) are defined in the workflow schema and parsed correctly, but execution is not yet wired. This page documents the planned design. The implementation will be tracked in a future release.

Triggers allow a workflow to be started automatically in response to an event, rather than requiring an explicit `wf run` invocation. Three trigger types are planned: `cron`, `file_watch`, and `webhook`.

---

## Defining triggers

Triggers are declared as a `[[triggers]]` array in the workflow TOML file:

```toml
name = "nightly-backup"

[[triggers]]
type     = "cron"
schedule = "0 2 * * *"    # 02:00 every night

[[triggers]]
type = "file_watch"
path = "/data/incoming/*.csv"

[[triggers]]
type = "webhook"
path = "/hooks/backup"
secret = "{{.WEBHOOK_SECRET}}"
```

The three types can coexist. A workflow with multiple triggers starts when any one of them fires.

---

## Trigger types

### `cron`

Runs the workflow on a schedule using standard five-field cron syntax.

```toml
[[triggers]]
type     = "cron"
schedule = "*/15 * * * *"   # every 15 minutes
```

**Important distinction from system daemons**: A cron trigger does *not* require `wf` to run as a system service or background daemon. It can be satisfied in two ways:

1. **OS cron (no persistent process)** — add a crontab entry that invokes `wf run <workflow>` directly. The OS scheduler handles timing; `wf` starts, executes, and exits. Nothing stays resident between runs. This is the default deployment model for cron triggers in air-gapped and edge environments.

2. **`wf` trigger listener** — when implemented, `wf listen` will start a lightweight foreground process that watches all defined triggers and fires workflows when they match. This is a single-purpose, user-initiated process — not a system service installed at boot, not a mandatory background daemon.

The distinction matters: the core `wf run` / `wf resume` execution model remains fully independent of any trigger system. Workflows that do not define triggers are unaffected.

### `file_watch`

Runs the workflow when a file matching a glob path is created or modified.

```toml
[[triggers]]
type    = "file_watch"
path    = "/var/spool/etl/incoming/*.json"
debounce = "5s"   # wait for burst to settle before firing
```

Use cases: ETL pipelines that process files as they arrive, audit workflows triggered by configuration changes, data ingestion from edge sensors.

### `webhook`

Exposes an HTTP endpoint. When a POST request is received (with a valid HMAC signature if `secret` is set), the workflow is started.

```toml
[[triggers]]
type   = "webhook"
path   = "/hooks/deploy"
secret = "{{.WEBHOOK_SECRET}}"
port   = 9090
```

Use cases: CI/CD integration, external event-driven workflows, integration with monitoring systems.

---

## Why this is not a daemon

"No daemons" has been a design principle of `wf`. It is worth being precise about what that means as triggers are added.

A **system daemon** is a long-running background process that:
- Is installed by the OS or an init system (systemd, launchd, Windows Service Manager)
- Starts at boot without user action
- Is expected to run indefinitely
- Typically has elevated privileges
- Represents required infrastructure — the system does not function without it

`wf`'s trigger listener is none of those things. It is:
- A foreground process, started explicitly by the user
- Single-purpose: it watches triggers and fires workflows
- Stoppable with a single Ctrl+C
- Optional: `wf run` always works without it

The `no required infrastructure` principle is preserved: you can run `wf` in any environment — air-gapped, edge, or offline — without a trigger listener. If you want scheduled or event-driven execution, you choose whether to use OS cron (zero persistent process) or the optional listener.

---

## Current status

The following trigger work is complete:

- `[[triggers]]` TOML block is parsed by `dag/parser.go` into `WorkflowDefinition.Triggers`
- All three trigger types (`cron`, `file_watch`, `webhook`) are structurally defined in `dag/dag.go`
- Validation of trigger fields runs at parse time

The following is planned but not yet implemented:

- `wf listen` command — starts the trigger listener for all workflows in the configured directory
- cron engine (likely using a standard Go cron library)
- `inotify`/`kqueue`/`ReadDirectoryChangesW` file watcher per platform
- HTTP webhook server with HMAC signature verification
- Trigger event logging in the audit trail (which trigger fired, when, what run it produced)

---

## See also

- [Resume](resume.md) — how `wf` handles runs that fail mid-execution
- [Architecture Overview](../architecture/overview.md) — where the trigger layer fits in the execution pipeline
- [CLI Reference](../reference/cli/index.md) — current `wf` commands
