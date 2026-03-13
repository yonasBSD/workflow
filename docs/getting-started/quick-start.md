# Quick Start

This guide walks you from zero to a running multi-task workflow in five minutes.

## 1. Install and initialise

```bash
curl -fsSL https://joelfokou.github.io/workflow/install.sh | sh
wf init
```

!!! note
    No Go toolchain required. The install script downloads the pre-built binary for your platform and verifies its checksum. See [Installation](installation.md) for manual download options.

## 2. Point wf at a directory

```bash
export WF_PATHS_WORKFLOWS="$(pwd)/workflows"
mkdir -p workflows
```

## 3. Write your first workflow

```bash
cat > workflows/hello.toml <<'EOF'
name        = "hello"
description = "A simple three-task workflow"

[tasks.greet]
cmd      = "echo Hello, $(whoami)!"
register = "greeting"

[tasks.timestamp]
cmd      = "date +%Y-%m-%dT%H:%M:%S"
register = "ts"

[tasks.report]
cmd        = "echo '{{.greeting}} — run at {{.ts}}'"
depends_on = ["greet", "timestamp"]
EOF
```

## 4. Validate

```bash
wf validate hello
# ✓ hello — 3 tasks, no cycles detected
```

## 5. Run it

```bash
wf run hello --print-output
```

Expected output:

```
[greet]      Hello, alice!
[timestamp]  2026-03-09T14:22:01
[report]     Hello, alice! — run at 2026-03-09T14:22:01
✓ hello completed in 42ms
```

## 6. Inspect the run

```bash
# List all runs
wf runs

# Get the run ID from the last run
RUN_ID=$(wf runs --limit 1 | awk 'NR==2{print $1}')

# See registered variables
wf inspect $RUN_ID

# Full audit trail
wf audit $RUN_ID
```

## 7. Visualise the DAG

```bash
wf graph hello                    # ASCII DAG to stdout (default)
wf graph hello --format html      # writes hello_graph.html — open in browser
wf graph hello --format dot       # Graphviz DOT output
```

---

## Next: Add parallel tasks and error handling

```toml
name = "pipeline"
description = "Parallel build with rollback"

[tasks.lint]
cmd = "echo linting..."

[tasks.test-unit]
cmd        = "echo unit tests..."
depends_on = ["lint"]

[tasks.test-integration]
cmd        = "echo integration tests..."
depends_on = ["lint"]

[tasks.build]
cmd        = "echo building..."
depends_on = ["test-unit", "test-integration"]

[tasks.deploy]
cmd         = "echo deploying..."
depends_on  = ["build"]
retries     = 2
retry_delay = "5s"
on_failure  = "rollback"

[tasks.rollback]
type = "forensic"
cmd  = "echo rolling back..."
```

```bash
# Run with work-stealing scheduler for maximum parallelism
wf run pipeline --work-stealing --print-output
```

Both `test-unit` and `test-integration` start as soon as `lint` completes — they don't wait for each other.

---

## What to explore next

| Topic | Link |
|---|---|
| All TOML fields | [Workflow File Reference](../reference/workflow-file.md) |
| Parallel execution modes | [Execution Modes](../concepts/execution-modes.md) |
| Variables and `register` | [Variables & Interpolation](../concepts/variables.md) |
| Matrix fan-out | [Matrix Expansion](../concepts/matrix.md) |
| Failure handling | [Forensic Tasks](../concepts/forensic-tasks.md) |
| Resuming failed runs | [Resume](../concepts/resume.md) |
| Real-world examples | [Examples](../examples/index.md) |
