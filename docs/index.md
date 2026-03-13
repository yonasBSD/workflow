# wf — Infrastructure Automation Runtime

**One binary. No required infrastructure. Anywhere.**

---

```bash
# Run a workflow on any node — connected or not
wf run provision-node --work-stealing --timeout 30m --print-output

# Resume any failed run from the exact point it stopped
wf resume <run-id>

# Full tamper-evident audit trail
wf audit <run-id>
```

---

## Built for environments where other tools can't go

Most automation tooling assumes infrastructure is available — a cloud API, a container runtime, a scheduler, a network connection. `wf` makes no such assumption.

It is a single static binary that executes operational workflows defined in TOML. Execution is deterministic. Every action is recorded in a local, append-only audit trail. Any failed run can be resumed from the exact point of failure with full variable state restored. It requires nothing beyond the binary and the host OS.

| Environment | Why wf fits |
|---|---|
| **Air-gapped systems** | Zero runtime dependencies. No network calls at runtime. One binary. |
| **Edge & remote nodes** | State is local SQLite. Runs survive connectivity loss and node restarts. |
| **Critical infrastructure** | Append-only audit trail. Formally documented security model with automated tests. |
| **Secure pipelines** | Path traversal prevention, template injection prevention, env-key deny-list — built in. |
| **Anywhere you'd write a shell script** | DAG ordering, parallel execution, retries, full audit trail. No infrastructure overhead. |

---

## Core Capabilities

<div class="grid cards" markdown>

-   **DAG Execution**

    Tasks declare `depends_on`. `wf` resolves execution order, detects cycles at parse time, and assigns [topological levels](concepts/dag.md) for parallel dispatch.

-   **Three Execution Modes**

    [Sequential](concepts/execution-modes.md), [level-parallel](concepts/execution-modes.md#parallel-level-based), and [work-stealing](concepts/execution-modes.md#work-stealing-dependency-driven) — choose the right trade-off between simplicity and throughput for each workflow.

-   **Variables & Interpolation**

    Tasks [capture stdout as named variables](concepts/variables.md#register-capture-task-output) via `register` and [reference them](concepts/variables.md#varname-interpolation) in downstream commands with `{{.varname}}`. Conditions (`if`) can gate tasks on runtime values.

-   **Matrix Expansion**

    A single task definition with a `matrix` field [expands into N parallel nodes](concepts/matrix.md) — one per parameter combination. Back up three databases, build for five platforms, scan ten services — from one task block.

-   **Forensic Failure Handling**

    The [Saga pattern](concepts/forensic-tasks.md) is built in. Declare `on_failure` on any task to wire a compensating transaction. Forensic tasks run only on failure and are excluded from normal execution.

-   **Resume**

    A failed run can be [resumed](concepts/resume.md) from the point of failure. Already-succeeded tasks are skipped. Registered variables are restored from snapshots. No re-work.

</div>

---

## Quick Start

```bash
go build -o wf .
wf init
export WF_PATHS_WORKFLOWS=/path/to/workflows
wf validate my-workflow
wf run      my-workflow --parallel --print-output
```

---

## Navigation

- **New here?** → [Quick Start](getting-started/quick-start.md)
- **TOML schema** → [Workflow File Reference](reference/workflow-file.md)
- **CLI flags** → [CLI Reference](reference/cli/index.md)
- **Real-world examples** → [Examples](examples/index.md)
- **Security** → [Security Model](security/model.md)
- **How it works inside** → [Architecture](architecture/overview.md)
