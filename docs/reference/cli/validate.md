# wf validate

Validate workflow definitions without executing them.

## Synopsis

```
wf validate [workflow] [flags]
```

If `[workflow]` is omitted, all `.toml` files in the workflows directory are validated.

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json`, `-j` | bool | `false` | Output validation results as JSON |
| `--store` | bool | `false` | Also verify database connectivity |

## What is validated

- TOML syntax
- Required fields (`name`, `cmd` per task)
- `name` field matches the filename
- No duplicate task IDs
- No self-referencing dependencies
- All `depends_on` targets exist
- No cycles in the dependency graph
- `working_dir` is not in `/proc`, `/sys`, or `/dev`
- `env` keys pass POSIX naming rules and the deny-list check
- Variable names in `register` are valid
- Matrix parameter names are valid
- `type` values are `"normal"` or `"forensic"`
- `on_failure` targets exist and are forensic tasks

## Examples

```bash
# Validate a single workflow
wf validate my-pipeline
# ✓ my-pipeline — 8 tasks, 0 cycles

# Validate all workflows
wf validate
# ✓ cicd-pipeline — 12 tasks
# ✓ data-etl — 7 tasks
# ✗ broken-workflow — cycle detected: a → b → a

# JSON output (useful in CI)
wf validate --json | jq '.[] | select(.valid == false)'

# Validate + check database
wf validate --store
```

## Exit Codes

| Code | Meaning |
|---|---|
| `0` | All validated workflows are valid |
| `1` | One or more workflows have validation errors |

This makes `wf validate` composable in CI pipelines:

```yaml
# GitHub Actions step
- name: Validate workflows
  run: wf validate
```

## See Also

- [Workflow File Reference](../workflow-file.md)
