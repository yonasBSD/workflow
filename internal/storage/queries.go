package storage

// ─── Runs ─────────────────────────────────────────────────────────────────────

const queryInsertRun = `
INSERT INTO runs (
    id, workflow_name, workflow_file, status, start_time,
    total_tasks, execution_mode, max_parallel,
    tags, timeout_ms
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const queryUpdateRun = `
UPDATE runs SET
    status           = ?,
    end_time         = ?,
    duration_ms      = ?,
    resume_count     = ?,
    last_resume_time = ?
WHERE id = ?`

const querySelectRun = `
SELECT
    id, workflow_name, workflow_file, status,
    start_time, end_time, duration_ms,
    total_tasks, tasks_success, tasks_failed, tasks_skipped,
    execution_mode, max_parallel,
    resume_count, last_resume_time,
    created_at, updated_at,
    tags, timeout_ms
FROM runs
WHERE id = ?`

const querySelectRunsBase = `
SELECT
    id, workflow_name, workflow_file, status,
    start_time, end_time, duration_ms,
    total_tasks, tasks_success, tasks_failed, tasks_skipped,
    execution_mode, max_parallel,
    resume_count, last_resume_time,
    created_at, updated_at,
    tags, timeout_ms
FROM runs`

// querySelectAllTaskDepsForRun fetches every dependency row for a run (for export).
const querySelectAllTaskDepsForRun = `
SELECT id, run_id, task_id, depends_on_task_id, dependency_satisfied, satisfied_at
FROM task_dependencies
WHERE run_id = ?`

// ─── Task Executions ──────────────────────────────────────────────────────────

const queryInsertTaskExecution = `
INSERT INTO task_executions (
    run_id, task_id, task_name, state, command,
    working_dir, max_retries,
    is_forensic, is_matrix_expansion, matrix_vars,
    condition_expr, condition_result
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const queryUpdateTaskExecution = `
UPDATE task_executions SET
    state         = ?,
    log_path      = ?,
    exit_code     = ?,
    start_time    = ?,
    end_time      = ?,
    duration_ms   = ?,
    attempt       = ?,
    error_message = ?,
    stack_trace   = ?,
    condition_result = ?
WHERE id = ?`

const querySelectTaskExecution = `
SELECT
    id, run_id, task_id, task_name, state, command,
    working_dir, log_path, exit_code,
    start_time, end_time, duration_ms,
    attempt, max_retries,
    is_forensic, is_matrix_expansion, matrix_vars,
    condition_expr, condition_result,
    error_message, stack_trace,
    created_at, updated_at
FROM task_executions
WHERE id = ?`

const querySelectTaskExecutionsBase = `
SELECT
    id, run_id, task_id, task_name, state, command,
    working_dir, log_path, exit_code,
    start_time, end_time, duration_ms,
    attempt, max_retries,
    is_forensic, is_matrix_expansion, matrix_vars,
    condition_expr, condition_result,
    error_message, stack_trace,
    created_at, updated_at
FROM task_executions`

// ─── Context Snapshots ────────────────────────────────────────────────────────

const queryInsertContextSnapshot = `
INSERT INTO context_snapshots (
    run_id, snapshot_time, snapshot_type,
    variable_name, variable_value, variable_type,
    set_by_task, set_at, is_read_only
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

const querySelectContextSnapshotsBase = `
SELECT
    id, run_id, snapshot_time, snapshot_type,
    variable_name, variable_value, variable_type,
    set_by_task, set_at, is_read_only,
    created_at
FROM context_snapshots`

// ─── Forensic Logs ────────────────────────────────────────────────────────────

const queryInsertForensicLog = `
INSERT INTO forensic_logs (
    run_id, task_id, log_type, log_data, triggered_by, trigger_reason
) VALUES (?, ?, ?, ?, ?, ?)`

const querySelectForensicLogsBase = `
SELECT
    id, run_id, task_id, log_type, log_data,
    triggered_by, trigger_reason, created_at
FROM forensic_logs`

// ─── DAG Cache ────────────────────────────────────────────────────────────────

const queryUpsertDAGCache = `
INSERT INTO dag_cache (run_id, dag_json, total_nodes, total_levels, has_parallel_tasks)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
    dag_json     = excluded.dag_json,
    total_nodes  = excluded.total_nodes,
    total_levels = excluded.total_levels,
    has_parallel_tasks = excluded.has_parallel_tasks`

const querySelectDAGCache = `
SELECT id, run_id, dag_json, total_nodes, total_levels, has_parallel_tasks, created_at
FROM dag_cache
WHERE run_id = ?`

// ─── Task Dependencies ────────────────────────────────────────────────────────

const queryInsertTaskDependency = `
INSERT INTO task_dependencies (run_id, task_id, depends_on_task_id)
VALUES (?, ?, ?)`

const queryMarkDependencySatisfied = `
UPDATE task_dependencies
SET dependency_satisfied = 1, satisfied_at = CURRENT_TIMESTAMP
WHERE run_id = ? AND task_id = ? AND depends_on_task_id = ?`

const querySelectTaskDependencies = `
SELECT id, run_id, task_id, depends_on_task_id, dependency_satisfied, satisfied_at
FROM task_dependencies
WHERE run_id = ? AND task_id = ?`

const querySelectPendingDependencies = `
SELECT id, run_id, task_id, depends_on_task_id, dependency_satisfied, satisfied_at
FROM task_dependencies
WHERE run_id = ? AND dependency_satisfied = 0`

// ─── Audit Trail ─────────────────────────────────────────────────────────────

const queryInsertAuditTrailEntry = `
INSERT INTO audit_trail (run_id, event_type, event_data)
VALUES (?, ?, ?)`

const querySelectAuditTrailBase = `
SELECT id, run_id, event_type, event_data, created_at
FROM audit_trail`
