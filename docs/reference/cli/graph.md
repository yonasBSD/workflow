# wf graph

Visualise the workflow DAG.

## Synopsis

```
wf graph <workflow> [flags]
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--format`, `-f` | string | `ascii` | Output format: `ascii`, `dot`, `html`, `mermaid`, `json` |
| `--detail`, `-d` | bool | `false` | Show timeout, retries, tags, and other task metadata |
| `--stats` | bool | `false` | Show workflow statistics (task count, levels, parallelism) |
| `--matrix` | bool | `false` | Show matrix-expanded nodes instead of collapsed task blocks |
| `--forensic` | bool | `false` | Include forensic/failure-handler tasks in the graph |
| `--colour` | bool | `true` | Use ANSI colour in ASCII output |
| `--highlight` | string | — | Highlight a specific task or the critical path through it |
| `--export`, `-o` | string | — | Write graph to a file (format inferred from extension) |

## Output Formats

=== "ASCII (default)"

    ```bash
    wf graph my-workflow
    ```

    Prints a text-based DAG to stdout — suitable for terminals, log files, and SSH sessions:

    ```
    [lint]
      └── [test-unit]
      └── [test-integration]
            └── [build]
                  └── [deploy]
    ```

=== "HTML"

    ```bash
    wf graph my-workflow --format html
    wf graph my-workflow --format html --export pipeline.html
    ```

    Writes an interactive HTML page to a file (default filename: `<workflow>_graph.html`). Nodes are clickable and show task details. Dependencies are drawn as directed arrows. Supports zoom and pan. Open the file in a browser.

=== "Graphviz DOT"

    ```bash
    wf graph my-workflow --format dot
    wf graph my-workflow --format dot | dot -Tsvg -o dag.svg
    wf graph my-workflow --format dot | dot -Tpng -o dag.png
    ```

=== "Mermaid"

    ```bash
    wf graph my-workflow --format mermaid
    ```

    Outputs a Mermaid flowchart definition, embeddable in Markdown:

    ````markdown
    ```mermaid
    flowchart TD
      lint --> test-unit
      lint --> test-integration
      test-unit --> build
      test-integration --> build
      build --> deploy
    ```
    ````

=== "JSON"

    ```bash
    wf graph my-workflow --format json
    wf graph my-workflow --format json | jq '.nodes[] | .id'
    ```

    Machine-readable representation of the DAG: nodes, edges, levels, and metadata.

## Examples

```bash
# Print ASCII DAG (default)
wf graph cicd-pipeline

# Export to SVG
wf graph cicd-pipeline --format dot | dot -Tsvg -o pipeline.svg

# Show with matrix expansion
wf graph database-backup --matrix

# Show with forensic tasks
wf graph order-processing --forensic

# Include detail and statistics
wf graph ml-training --detail --stats

# Export to file
wf graph my-workflow --format dot --export pipeline.dot
wf graph my-workflow --format html --export pipeline.html  # writes interactive HTML file

# Highlight a specific task's dependency chain
wf graph my-workflow --highlight deploy
```

## See Also

- [DAG Execution Model](../../concepts/dag.md)
- [Matrix Expansion](../../concepts/matrix.md)
- [Forensic Tasks](../../concepts/forensic-tasks.md)
