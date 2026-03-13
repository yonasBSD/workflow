# Concepts

Understanding these six concepts is enough to use `wf` effectively for any workflow.

| Concept | Summary |
|---|---|
| [DAG Execution Model](dag.md) | How tasks are ordered, how levels work, how cycles are detected |
| [Execution Modes](execution-modes.md) | Sequential, level-parallel, and work-stealing — when to use each |
| [Variables & Interpolation](variables.md) | `register`, `{{.var}}` syntax, `if` conditions, runtime `--var` |
| [Matrix Expansion](matrix.md) | Fan a single task definition out over a parameter grid |
| [Forensic Tasks & Failure Handling](forensic-tasks.md) | `on_failure`, compensating transactions, the Saga pattern |
| [Resume](resume.md) | How failed runs are resumed, variable restoration, skip logic |
