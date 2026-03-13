# Security Model

`wf` executes arbitrary shell commands defined in TOML files. This makes the security of the file-loading and execution pipeline critical. This document describes the threat model, trust boundaries, and every active defence implemented.

---

## Trust Boundaries

```
┌─────────────────────────────────────────────────────────┐
│  Trusted                                                │
│    • The wf binary itself                               │
│    • The authenticated OS user running wf               │
│    • Workflow files within the configured workflows dir  │
├─────────────────────────────────────────────────────────┤
│  Untrusted / Validated                                  │
│    • CLI input (workflow names, --var values)           │
│    • TOML file content (env keys, working_dir, cmds)   │
│    • Task stdout/stderr (registered variable values)   │
└─────────────────────────────────────────────────────────┘
```

`wf` does not run as root. It does not elevate privileges. It does not open network sockets. Its attack surface is limited to file I/O and subprocess execution.

---

## Threat: Path Traversal

**Risk**: An attacker controlling the workflow name argument could use `../` sequences to load a TOML file outside the configured workflows directory, potentially loading `/etc/passwd` or another sensitive file as a workflow.

**Defence — `ValidateWorkflowName()`** (`internal/security/validate.go`):

1. Null bytes are rejected outright
2. Absolute paths are rejected
3. Each path component is checked individually — `..` and `.` are rejected before `filepath.Join` has a chance to resolve them
4. The resolved absolute path is checked to be a strict descendant of the workflows directory

```go
// Layer 4: containment check after path cleaning
resolved := filepath.Join(absWorkflows, clean+".toml")
if !strings.HasPrefix(resolved, absWorkflows+string(filepath.Separator)) {
    return fmt.Errorf("%w: resolved path %q escapes workflows directory", ErrPathTraversal, resolved)
}
```

This two-layer approach (component check + containment check) is resilient to both simple `../` and multi-level traversal like `a/b/../../../etc/shadow`.

---

## Threat: Template Injection

**Risk**: If task command strings were interpolated using Go's `text/template` engine, an attacker controlling a registered variable value could inject template directives (`{{range}}`, `{{call}}`, `{{.}}`) and escalate from variable substitution to arbitrary code execution within the template engine.

**Defence — regex-only substitution** (`internal/contextmap/template.go`):

The `text/template` engine was completely removed. Variable substitution is performed by a purpose-built regex:

```
\{\{\.([a-zA-Z0-9_.+\-\[\]=,]+)\}\}
```

This regex matches **only** `{{.varname}}` — nothing else. Template logic tokens (`{{if}}`, `{{range}}`, `{{call}}`, `{{define}}`, pipelines) do not match and are emitted verbatim into the shell command. Because they are not valid shell syntax, they cause an immediately visible shell error rather than silently executing attacker-controlled template logic.

---

## Threat: Dynamic Linker Injection

**Risk**: TOML authors can set environment variables on tasks via the `env` field. Variables like `LD_PRELOAD` and `LD_LIBRARY_PATH` cause the dynamic linker to load attacker-controlled shared libraries into child processes, converting an arbitrary-write into code execution.

**Defence — `ValidateEnvKey()` deny-list** (`internal/security/validate.go`):

```go
blocked := map[string]string{
    "LD_PRELOAD":            "dynamic-linker preload override",
    "LD_LIBRARY_PATH":       "dynamic-linker library path override",
    "LD_AUDIT":              "dynamic-linker audit module override",
    "LD_DEBUG":              "dynamic-linker debug flag",
    "LD_DEBUG_OUTPUT":       "dynamic-linker debug output redirect",
    "DYLD_INSERT_LIBRARIES": "macOS dynamic-linker insert override",
    "DYLD_LIBRARY_PATH":     "macOS dynamic-linker library path override",
    "DYLD_FRAMEWORK_PATH":   "macOS dynamic-linker framework path override",
}
```

These keys are rejected at **parse time** — the workflow fails to load before any task is executed. The list covers both Linux (`LD_*`) and macOS (`DYLD_*`) dynamic linker variables.

In addition, all env keys must match POSIX naming rules (letters, digits, underscore; no digit start) to prevent Unicode-lookalike and whitespace attacks.

---

## Threat: Working Directory Escape

**Risk**: A task could declare `working_dir = "/proc/self/fd"` or similar to access restricted kernel filesystems.

**Defence — `ValidateWorkingDir()`** (`internal/security/validate.go`):

```go
restricted := []string{"/proc", "/sys", "/dev"}
for _, prefix := range restricted {
    if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
        return fmt.Errorf("working_dir %q references restricted system path", dir, prefix)
    }
}
```

Null bytes in `working_dir` are also rejected. Existence and permission checks are delegated to the OS at execution time.

---

## Threat: Memory Exhaustion via Unbounded Output

**Risk**: A task producing gigabytes of stdout/stderr would exhaust heap memory, causing an OOM kill of the `wf` process.

**Defence — `limitedBuffer`** (`internal/executor/executor.go`):

A custom `io.Writer` that silently drops writes beyond 10 MiB:

```go
const maxCaptureBytes = 10 * 1024 * 1024

func (lb *limitedBuffer) Write(p []byte) (int, error) {
    remaining := lb.maxBytes - lb.written
    if remaining <= 0 {
        return len(p), nil    // drop silently; don't break the pipe
    }
    // ...
}
```

Returning `len(p), nil` even when dropping means the writing process (the task) does not receive a broken pipe error — it continues to run normally with truncated capture. The full output is always preserved in the log file (written by a separate `io.MultiWriter` before the limit buffer).

---

## Threat: Log File Information Disclosure

**Risk**: Per-task log files containing secrets (tokens, passwords echoed to stdout) could be readable by other users on a multi-user system.

**Defence — restrictive file permissions**:

| Resource | Mode |
|---|---|
| Log directories | `0700` (owner-only) |
| Per-task log files | `0600` (owner read/write only) |
| SQLite database | `0600` (pre-created before SQLite opens it) |

The database is pre-created with `O_CREATE|O_RDWR, 0600` before being opened by the SQLite driver. This avoids the TOCTOU race that would exist if we opened the file with SQLite first and then `chmod`-ed it.

---

## Threat: SQL Injection

**Risk**: Dynamic SQL construction with string interpolation could allow injected SQL in workflow names, task IDs, or variable values to modify or extract database records.

**Defence — parameterized queries throughout**:

Every SQL statement in `internal/storage/` uses `?` placeholders and driver-level parameter binding. No user-supplied value is interpolated into a SQL string.

---

## Threat: Variable Ownership Confusion

**Risk**: A task could register a variable with the same name as one registered by a trusted upstream task, overwriting its value and corrupting downstream interpolation.

**Defence — ownership tracking in `ContextMap`**:

Each variable is stored with its owner task ID. A second `Set()` call for the same variable name fails with an error. The registering task fails, rather than silently overwriting trusted data.

---

## Threat: Arbitrary Variable Names

**Risk**: A variable name containing shell metacharacters or template syntax could cause injection when the name is interpolated.

**Defence — `ValidateVariableName()`**:

Variable names are restricted to: letters, digits, `_`, `-`, `.`, `[`, `]`, `=`, `,`. This character set is validated at parse time. Names with other characters are rejected before the workflow runs.

---

## Audit Trail

Every state transition is written to the `audit_trail` table in the database as an **append-only** record. There is no delete or update path for audit records. This provides a tamper-evident (within the SQLite file's integrity guarantees) history of all task state changes, variable registrations, and forensic handler invocations.

---

## Security Test Suite

All defences above are covered by automated tests in `tests/security/security_test.go`. The suite includes 22 test functions covering:

- Path traversal (null byte, absolute path, `..` components, multi-level escape)
- Template injection (pipeline injection, function call injection, `range`/`if` injection)
- Output buffer limit (10 MiB cap, write return value correctness)
- Env key validation (POSIX rules, deny-list coverage)
- Working directory restriction (`/proc`, `/sys`, `/dev`)
- Variable ownership isolation
- Read-only matrix variables
- Database file permissions (0600)
- Audit trail append-only behaviour
- Variable name validation

Run the security suite:

```bash
go test -race ./tests/security/...
```
