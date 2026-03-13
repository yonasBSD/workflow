# wf export

Export a complete run record as JSON or a tar.gz archive.

## Synopsis

```
wf export <run-id> [flags]
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--format`, `-f` | string | `json` | Export format: `json` or `tar` |
| `--output`, `-o` | string | stdout | Output file path. Required for `tar` format. Defaults to stdout for `json`. |

## Formats

=== "JSON"

    A self-contained JSON document with all run data:

    ```bash
    wf export 2Xk7p9QrVnYoJ1mT3s
    wf export 2Xk7p9QrVnYoJ1mT3s > run.json
    wf export 2Xk7p9QrVnYoJ1mT3s --output run.json
    ```

    JSON structure:

    ```json
    {
      "exported_at": "2026-03-09T14:30:00Z",
      "run": {
        "id": "2Xk7p9QrVnYoJ1mT3s",
        "workflow_name": "cicd-pipeline",
        "status": "success",
        "started_at": "2026-03-09T14:22:01Z",
        "duration_ms": 72400,
        "execution_mode": "work_stealing"
      },
      "tags": ["ci", "deploy"],
      "tasks": [...],
      "dependencies": [...],
      "context_snapshots": [...],
      "forensic_logs": [...],
      "audit_trail": [...]
    }
    ```

=== "tar.gz Archive"

    An archive containing the JSON export plus all raw log files:

    ```bash
    wf export 2Xk7p9QrVnYoJ1mT3s --format tar --output run-export.tar.gz
    ```

    Archive contents:

    ```
    run-2Xk7p9QrVnYoJ1mT3s/
    ├── run.json                  # full JSON export
    ├── logs/
    │   ├── lint.log
    │   ├── test-unit.log
    │   ├── test-integration.log
    │   ├── build-linux.log
    │   └── ...
    ```

## Use Cases

**Incident investigation** — export a failed run and share it with your team:

```bash
wf export $FAILED_RUN_ID --format tar --output incident-$(date +%Y%m%d).tar.gz
```

**CI artefact** — upload run records as build artefacts:

```yaml
# GitHub Actions
- name: Export run record
  if: always()
  run: wf export $RUN_ID --format tar --output wf-run.tar.gz

- uses: actions/upload-artifact@v4
  if: always()
  with:
    name: workflow-run
    path: wf-run.tar.gz
```

**Compliance** — export and archive all production runs:

```bash
wf runs --tag production --json | jq -r '.[].id' | while read id; do
  wf export $id --format tar --output archives/$id.tar.gz
done
```

## See Also

- [wf inspect](inspect.md) — interactive run inspection
- [wf audit](audit.md) — audit trail
- [wf runs](runs.md) — list run IDs
