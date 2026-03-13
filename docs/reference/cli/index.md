# CLI Reference

`wf` is the single binary that provides all commands. All commands share a set of global flags.

## Global Flags

These flags are available on every subcommand.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--config` | string | platform default | Path to the config file |
| `--log-level` | string | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `--verbose`, `-v` | bool | `false` | Shorthand for `--log-level debug` |

## Commands

| Command | Description |
|---|---|
| [`wf init`](init.md) | Initialise workspace: create directories, database, and config file |
| [`wf run`](run.md) | Execute a workflow |
| [`wf resume`](resume.md) | Resume a failed or interrupted run |
| [`wf validate`](validate.md) | Validate workflow definitions without running them |
| [`wf list`](list.md) | List all available workflows |
| [`wf runs`](runs.md) | List, filter, and analyse past workflow runs |
| [`wf logs`](logs.md) | View task logs for a specific run |
| [`wf graph`](graph.md) | Visualise the workflow DAG |
| [`wf inspect`](inspect.md) | Show detailed information about a run |
| [`wf status`](status.md) | Live-poll the status of a running workflow |
| [`wf audit`](audit.md) | Show the chronological audit trail for a run |
| [`wf diff`](diff.md) | Compare two runs side-by-side |
| [`wf export`](export.md) | Export a complete run record as JSON or tar archive |
| [`wf health`](health.md) | System health check |

## Shell Completion

```bash
# Bash
wf completion bash > /etc/bash_completion.d/wf

# Zsh
wf completion zsh > "${fpath[1]}/_wf"

# Fish
wf completion fish > ~/.config/fish/completions/wf.fish
```
