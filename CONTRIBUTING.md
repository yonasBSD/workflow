# Contributing to wf

Thank you for contributing. This guide covers everything needed to go from idea to merged pull request.


---

## Development setup

**Prerequisites**: Go 1.24+, Git

```bash
git clone https://github.com/silocorp/workflow.git
cd workflow
go mod download
go build -o wf .
./wf --version
```

---

## Running tests

```bash
# All tests with race detection — required before opening a PR
go test -race ./...

# Targeted suites
go test -race ./tests/security/...      # security invariants
go test -race ./tests/e2e/...           # end-to-end
go test -race ./tests/integration/...   # integration

# Single test
go test -run TestName ./path/to/pkg/...
```

All tests must pass with `-race` before submission. The security suite (`tests/security/`) is part of the required baseline — changes that weaken a security invariant require a documented justification to be considered.

---

## Code quality

```bash
gofmt -w .     # format
go vet ./...   # lint
go mod tidy    # tidy dependencies
```

---

## Architecture

The execution pipeline is linear:

```
CLI (cmd/) → Parser (dag/parser.go) → Builder (dag/builder.go) → Executor → Storage
```

Key packages and their roles:

| Package | Responsibility |
|---|---|
| `cmd/` | Cobra CLI commands; flag parsing; executor selection |
| `internal/dag/` | TOML → `WorkflowDefinition` → `DAG`; cycle detection; matrix expansion |
| `internal/executor/` | Task lifecycle; retry loop; forensic traps; sequential / parallel / work-stealing |
| `internal/storage/` | SQLite persistence; versioned schema migrations; audit trail; snapshots |
| `internal/contextmap/` | Variable registry; safe `{{.var}}` interpolation; `if` condition evaluation |
| `internal/security/` | Path traversal; env-key deny-list; working_dir restrictions |
| `internal/config/` | Viper-based config; `config.Get()` — never a global `config.C` |
| `internal/logger/` | `log/slog` wrapper; `logger.Init()` must be called before use |

For deeper detail see the [Architecture documentation](docs/architecture/overview.md).

---

## Conventions

**Variable interpolation syntax**: `{{.varname}}` — not `${var}`. The substitution engine is a purpose-built regex, not `text/template`. Template logic is intentionally not supported.

**Configuration access**: always `config.Get()`. There is no package-level `config.C`.

**SQL**: all queries use parameterized statements (`?` placeholders). No string interpolation into SQL.

**File permissions**: log dirs at `0700`, log files and the database at `0600`.

**Run IDs**: KSUID format (sortable, collision-free, URL-safe).

---

## Scope

`wf` is an infrastructure automation runtime for deterministic, auditable, resumable workflow execution. Contributions are welcome across the full surface area — execution engine, storage, security, CLI, documentation, examples, and tests.

There is no hardcoded scope ceiling. If you are planning a significant change, open an issue first to discuss the design before writing code.

---

## Security contributions

Security is a foundational property of `wf`. Contributions adding new features must not weaken existing security invariants. If your change affects any of the following, include or update tests in `tests/security/`:

- Path validation (`internal/security/validate.go`)
- Variable interpolation (`internal/contextmap/template.go`)
- Environment variable handling
- File permission handling
- SQL query construction

To report a vulnerability privately, see [SECURITY.md](SECURITY.md).

---

## Pull request process

1. **Branch** from `master`:
   ```bash
   git checkout -b feat/your-feature
   # also: fix/, docs/, security/, perf/
   ```

2. **Write code** following standard Go idioms. Keep functions small and testable.

3. **Format, lint, test:**
   ```bash
   gofmt -w . && go vet ./... && go test -race ./...
   ```

4. **Commit** using [Conventional Commits](https://www.conventionalcommits.org/):
   ```
   feat: add cron trigger support
   fix: resolve race in work-stealing pending counter
   security: extend env-key deny-list to cover GLIBC_TUNABLES
   docs: document matrix variable scoping rules
   ```

5. **Open a PR** against `master`.

### Review checklist

- [ ] `go test -race ./...` passes
- [ ] `tests/security/...` passes (updated if the change is security-sensitive)
- [ ] New CLI flags or TOML fields are documented in `docs/`
- [ ] No new external dependencies without prior discussion
- [ ] Commit history is clean (squash work-in-progress commits)

---

## Reporting bugs

Include: `wf --version` output, OS and architecture, a minimal TOML file or command sequence that reproduces the issue, and `wf logs <run-id>` output or the error message.

---

## Code of Conduct

Be respectful and constructive. This project is committed to a welcoming environment for all contributors.