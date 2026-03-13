# wf

### Infrastructure automation runtime. One binary. No required infrastructure. Anywhere.

[![Go Report Card](https://goreportcard.com/badge/github.com/joelfokou/workflow)](https://goreportcard.com/report/github.com/joelfokou/workflow)
[![GitHub release](https://img.shields.io/github/v/release/joelfokou/workflow)](https://github.com/joelfokou/workflow/releases)
[![License](https://img.shields.io/github/license/joelfokou/workflow)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/joelfokou/workflow.svg)](https://pkg.go.dev/github.com/joelfokou/workflow)

---

Automation tooling has converged around a single assumption: infrastructure is available. Cloud APIs, container runtimes, message brokers, schedulers — the entire ecosystem presupposes a connected, provisioned environment. That assumption excludes a large class of real-world operational environments.

`wf` is built without that assumption.

It is a single static binary that executes operational workflows defined in TOML. Execution is deterministic, every action is recorded in a local audit trail, and any failed run can be resumed from the exact point of failure — with full variable state restored. It requires nothing beyond the binary and the host OS. No network. No agent. No coordinator.

```bash
wf run provision-node --work-stealing --print-output
```

---

## The environments that matter

`wf` is designed for the environments where conventional automation tooling breaks down:

**Air-gapped infrastructure** — government, defence, financial, and classified systems that have no external network access. `wf` ships as a single binary with zero runtime dependencies. There is no phone-home, no registry pull, no cloud token. It operates indefinitely offline.

**Edge and remote nodes** — substations, factory floors, oil platforms, weather stations, satellite ground stations, remote edge gateways. These environments have intermittent or no connectivity. `wf` stores all state locally in SQLite. If a connection drops mid-run, the run can be resumed when the node is accessible again — nothing is lost.

**Critical infrastructure operations** — environments where an operator needs to prove that a procedure was executed in a specific order, with specific inputs, producing specific outputs, with a tamper-evident record. `wf`'s audit trail is append-only. Every state transition — task started, variable registered, retry attempted, forensic handler fired — is recorded with a timestamp and cannot be modified after the fact.

**Secure deployment pipelines** — environments that cannot tolerate arbitrary code execution through template injection, path traversal, or dynamic-linker hijacking. `wf` has a formal, documented security model with an automated test suite covering each threat class explicitly.

**Anywhere you'd otherwise write a shell script** — `wf` brings deterministic ordering, parallel execution, failure handling, variable passing, and a full audit trail to work that would otherwise be a fragile bash script with no recovery path.

---

## What it does

`wf` executes **workflows** — directed acyclic graphs of tasks defined in TOML. Tasks declare their dependencies. `wf` resolves the execution order, runs tasks in parallel where possible, captures their output as named variables, and writes every state change to a local SQLite database.

```toml
name = "deploy-node"
description = "Provision, configure, and verify a new edge node"
on_failure   = "alert-and-rollback"

[tasks.preflight]
cmd      = "./checks/preflight.sh"
register = "node_id"
timeout  = "2m"

[tasks.provision]
cmd        = "./provision.sh --node={{.node_id}} --env={{.target_env}}"
depends_on = ["preflight"]
retries    = 2
retry_delay = "30s"
on_failure  = "teardown-node"

[tasks.configure]
cmd        = "./configure.sh --node={{.node_id}}"
depends_on = ["provision"]
timeout    = "10m"

[tasks.verify]
cmd        = "./verify.sh --node={{.node_id}}"
depends_on = ["configure"]
register   = "verification_status"

[tasks.teardown-node]
type = "forensic"
cmd  = "./teardown.sh --node={{.node_id}} --reason='{{.error_message}}'"

[tasks.alert-and-rollback]
type           = "forensic"
cmd            = "./alert.sh --failed='{{.failed_task}}' --node='{{.node_id}}'"
ignore_failure = true
```

```bash
# Run with parallel execution, 30-minute ceiling
wf run deploy-node --work-stealing --timeout 30m --print-output

# If anything fails, resume from the point of failure
wf resume <run-id>

# Full audit trail
wf audit <run-id>
```

---

## Capabilities

**Execution model**
- Directed acyclic graph — tasks declare `depends_on`, `wf` resolves order and detects cycles at parse time
- Three schedulers: sequential, level-parallel (`--parallel`), dependency-driven work-stealing (`--work-stealing`)
- Per-run wall-clock timeout with immediate process-group cancellation
- Per-task timeout, retries, and retry delay — context-aware (timeout-cancelled retries abort immediately)

**Variables and control flow**
- `register` — capture task stdout as a named variable
- `{{.varname}}` — safe regex-based interpolation in downstream commands (no template logic, by design)
- `if` — conditional task execution evaluated against runtime variable values
- `matrix` — fan a single task definition out over a parameter grid (e.g., three databases, five registries)
- `--var KEY=VALUE` — inject variables at runtime without modifying the workflow file

**Failure handling**
- `on_failure` — wire a forensic task to any task or to the entire workflow
- `type = "forensic"` — tasks that run only on failure, implementing the Saga pattern
- `ignore_failure` — continue execution regardless of exit code
- Forensic tasks receive `{{.failed_task}}` and `{{.error_message}}` automatically

**Observability**
- Append-only audit trail — every state transition recorded with timestamp
- Per-task log files (mode 0600)
- `wf inspect` — structured run details, registered variables, forensic logs
- `wf diff` — compare any two runs side-by-side
- `wf audit` — chronological event trail
- `wf export` — full run record as JSON or tar archive (for compliance and incident response)
- `wf graph` — DAG visualisation (HTML, ASCII, Graphviz DOT, Mermaid, JSON)

**Security model** *(formally documented, tested)*
- Path traversal prevention with two-layer validation
- Template injection prevention — `text/template` replaced with a purpose-built regex substitutor
- Dynamic-linker injection deny-list (`LD_PRELOAD`, `DYLD_INSERT_LIBRARIES`, etc.)
- Working directory restrictions (`/proc`, `/sys`, `/dev` blocked)
- Output buffer cap — 10 MiB per task, silent drop, no pipe break
- File permissions — database and log files created at 0600 before any write
- Parameterised SQL throughout — no string interpolation into queries

**Deployment**
- Single static binary — no runtime dependencies, no CGo
- Cross-platform: Linux, macOS, Windows
- Configuration via YAML file, `WF_*` environment variables, or CLI flags
- Zero external network calls at runtime

---

## Getting started

```bash
# Install (Linux / macOS)
curl -fsSL https://joelfokou.github.io/workflow/install.sh | sh

# Initialise workspace
wf init

# Point at your workflows directory
export WF_PATHS_WORKFLOWS=/path/to/workflows

# Validate before running
wf validate my-workflow

# Run
wf run my-workflow --parallel --print-output
```

Pre-built binaries for Linux, macOS, and Windows are available on the [Releases page](https://github.com/joelfokou/workflow/releases). The install script verifies checksums automatically.

Full documentation: **[joelfokou.github.io/workflow](https://joelfokou.github.io/workflow)**

- [Installation](https://joelfokou.github.io/workflow/getting-started/installation/)
- [Quick Start](https://joelfokou.github.io/workflow/getting-started/quick-start/)
- [Workflow File Reference](https://joelfokou.github.io/workflow/reference/workflow-file/)
- [CLI Reference](https://joelfokou.github.io/workflow/reference/cli/)
- [Security Model](https://joelfokou.github.io/workflow/security/model/)
- [Examples](https://joelfokou.github.io/workflow/examples/)

---

## Design principles

**Operational certainty over convenience.** Every feature is evaluated against the question: does this make execution more predictable and auditable, or less? Template logic was removed from variable interpolation. The audit trail is append-only. Process groups are killed on timeout, not just the shell. These are not accidents.

**No required infrastructure.** The dependency graph for a run is: one binary, one workflow file, one OS. SQLite is embedded. The executor is in-process. The core execution model will never require a network service, a cloud API, or a container runtime. Optional capabilities — such as trigger listeners for event-driven scheduling — are additive and do not change this invariant for direct `wf run` invocations.

**Failure is a first-class concern.** Resume is not a nice-to-have — it is the core answer to the question "what happens when step 12 of a 20-step procedure fails at 3am on an air-gapped node." The Saga pattern (compensating transactions via `on_failure` forensic tasks) exists because some side effects cannot simply be re-run.

**Workflows are code.** TOML files live in git. They are reviewed, tagged, diffed, and audited. The workflow definition is the source of truth for what happened — not a UI, not a log file, not institutional memory.

**Security is not a layer.** It is not added on top of a working system. It is the reason certain design decisions were made in the first place.

---

## Architecture

```
CLI (cmd/)
  └── Parser (dag/parser.go)
        └── Builder (dag/builder.go) → DAG{Levels [][]*Node}
              └── Executor (sequential | parallel | work-stealing)
                    ├── ContextMap (variables, interpolation, conditions)
                    ├── Storage (SQLite — runs, task_executions, audit_trail, snapshots)
                    └── Logger (log/slog — console + file)
```

The executor, storage backend, and scheduler are deliberately separated. The system is designed so that each layer can evolve independently. The trigger system (cron, file-watch, webhook) is defined in the schema and awaits implementation. The storage interface is abstracted over a concrete SQLite implementation.

Details: [Architecture Overview](https://joelfokou.github.io/workflow/architecture/overview/)

---

## Contributing

`wf` is open source under the Apache License 2.0.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development guide, test requirements, and pull request process.

Security vulnerabilities: see [SECURITY.md](SECURITY.md).

---

## Licence

Apache License 2.0 — see [LICENSE](LICENSE).
