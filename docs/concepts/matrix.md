# Matrix Expansion

The `matrix` field fans a single task definition out into N parallel nodes — one per parameter combination. This eliminates copy-paste for tasks that differ only in their inputs.

---

## Syntax

```toml
[tasks.build]
cmd    = "GOOS={{.build.os}} GOARCH={{.build.arch}} go build -o bin/app-{{.build.os}}-{{.build.arch}} ."
matrix = {os = ["linux", "darwin", "windows"], arch = ["amd64", "arm64"]}
```

This produces **6 nodes**:

```
build[os=linux,arch=amd64]
build[os=linux,arch=arm64]
build[os=darwin,arch=amd64]
build[os=darwin,arch=arm64]
build[os=windows,arch=amd64]
build[os=windows,arch=arm64]
```

Each node runs independently and in parallel (in `--parallel` or `--work-stealing` mode).

---

## Single-Dimension Matrix

The most common case — one parameter, N values:

```toml
[tasks.backup-db]
cmd    = "pg_dump {{.backup-db.db}} > /backups/{{.backup-db.db}}.sql"
matrix = {db = ["users", "orders", "inventory", "analytics"]}
```

Produces: `backup-db[db=users]`, `backup-db[db=orders]`, `backup-db[db=inventory]`, `backup-db[db=analytics]`.

---

## Accessing Matrix Variables in Commands

Matrix variables are accessed in `cmd` using `{{.taskid.paramname}}`:

```toml
[tasks.test]
cmd    = "go test ./... -run {{.test.suite}}"
matrix = {suite = ["unit", "integration", "e2e"]}
```

The variable name is `test.suite` — the task ID (`test`) followed by a dot and the parameter name (`suite`).

---

## `depends_on` with Matrix Tasks

When a downstream task depends on a matrix task, it waits for **all expanded nodes** to complete:

```toml
[tasks.backup]
cmd    = "dump {{.backup.db}}"
matrix = {db = ["postgres", "mysql", "redis"]}

[tasks.verify-all]
cmd        = "echo 'All backups complete'"
depends_on = ["backup"]   # waits for backup[db=postgres], [db=mysql], [db=redis]
```

If any matrix node fails (and `ignore_failure = false`), `verify-all` is marked `cancelled`.

---

## `register` with Matrix Tasks

Each matrix node has its own `register` output. The variable is stored with a scoped key:

```toml
[tasks.backup]
cmd      = "dump {{.backup.db}} && echo 'checksum_abc123'"
register = "checksum"
matrix   = {db = ["postgres", "mysql"]}
```

Registers `checksum` for each node independently. The values are namespaced internally but downstream tasks can reference the last-written value via `{{.checksum}}` (last-completing node wins).

For deterministic access, use separate non-matrix tasks per database if the specific checksum per db matters.

---

## `ignore_failure` on Matrix Tasks

`ignore_failure = true` applies to each expanded node independently. A failed node does not cancel sibling nodes:

```toml
[tasks.deploy]
cmd            = "./deploy.sh {{.deploy.env}}"
matrix         = {env = ["staging", "canary", "prod"]}
ignore_failure = true
```

All three deploy nodes run even if one fails.

---

## Full Matrix Example

```toml
name = "cross-platform-release"

[tasks.build]
cmd    = """
GOOS={{.build.os}} GOARCH=amd64 go build -o dist/app-{{.build.os}} .
echo "built {{.build.os}}"
"""
register = "build_status"
matrix   = {os = ["linux", "darwin", "windows"]}
timeout  = "5m"

[tasks.package]
cmd        = "tar czf dist/app-{{.package.os}}.tar.gz dist/app-{{.package.os}}"
matrix     = {os = ["linux", "darwin", "windows"]}
depends_on = ["build"]
timeout    = "2m"

[tasks.publish]
cmd        = "echo Publishing all packages..."
depends_on = ["package"]
```

---

## Visualising Matrix Expansion

```bash
wf graph my-workflow --matrix    # show expanded matrix nodes
wf graph my-workflow             # collapsed view
```

The HTML graph view shows each matrix node as a separate box with its parameter label.

---

## Constraints

- Matrix parameters must be arrays of strings
- Parameter names must be valid variable names (letters, digits, `_`, `-`, `.`)
- Matrix expansion happens at DAG build time — the number of nodes is fixed before execution starts
- Circular dependencies between matrix nodes of the same task are not possible (they are siblings)
