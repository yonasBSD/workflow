# wf list

List all available workflows.

## Synopsis

```
wf list [flags]
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json`, `-j` | bool | `false` | Output as JSON |
| `--detailed`, `-d` | bool | `false` | Include run statistics (total runs, last run status, last run duration) |

## Output

Default output shows the workflow name and task count:

```
WORKFLOW              TASKS
cicd-pipeline         12
data-etl              8
database-backup       6
```

With `--detailed`:

```
WORKFLOW              TASKS  TOTAL RUNS  SUCCESS  FAILED  LAST RUN
cicd-pipeline         12     42          40       2       2026-03-09 14:22:01
data-etl              8      128         120      8       2026-03-09 02:00:03
```

## Examples

```bash
wf list
wf list --detailed
wf list --json | jq '.[].name'
```

## See Also

- [wf runs](runs.md) — filter and analyse past runs
- [wf validate](validate.md) — validate workflow definitions
