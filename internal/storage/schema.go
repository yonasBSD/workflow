package storage

import (
	"database/sql"
	"fmt"

	"github.com/joelfokou/workflow/internal/logger"
)

// migration is a single, forward-only schema change.
type migration struct {
	version int
	sql     string
}

// migrations is the ordered list of schema migrations. Each entry is applied
// exactly once, tracked by its version number in the schema_version table.
// To add a new migration: append a new entry with the next version number.
// Never modify or delete existing entries — only add new ones.
var migrations = []migration{
	{
		version: 1,
		sql: `
---------------------
-- TABLE DEFINITIONS
---------------------

-- Workflow runs
CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    workflow_name TEXT NOT NULL,
    workflow_file TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending', 'running', 'success', 'failed', 'cancelled', 'resuming')) DEFAULT 'pending',
    start_time DATETIME NOT NULL,
    end_time DATETIME,
    duration_ms INTEGER,

    -- Statistics
    total_tasks INTEGER NOT NULL DEFAULT 0,
    tasks_success INTEGER NOT NULL DEFAULT 0,
    tasks_failed INTEGER NOT NULL DEFAULT 0,
    tasks_skipped INTEGER NOT NULL DEFAULT 0,

    -- Execution metadata
    execution_mode TEXT CHECK(execution_mode IN ('sequential', 'parallel', 'work_stealing')) NOT NULL DEFAULT 'sequential',
    max_parallel INTEGER NOT NULL DEFAULT 1,

    -- Resume tracking
    resume_count INTEGER NOT NULL DEFAULT 0,
    last_resume_time DATETIME,

    -- Tags and run-level timeout
    tags TEXT NOT NULL DEFAULT '[]',
    timeout_ms INTEGER NOT NULL DEFAULT 0,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
CREATE INDEX IF NOT EXISTS idx_runs_workflow_name ON runs(workflow_name);
CREATE INDEX IF NOT EXISTS idx_runs_start_time ON runs(start_time);
CREATE INDEX IF NOT EXISTS idx_runs_tags ON runs(tags);

-- Task execution records
CREATE TABLE IF NOT EXISTS task_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    task_name TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('pending', 'ready', 'running', 'success', 'failed', 'skipped', 'cancelled')),

    -- Execution details
    command TEXT NOT NULL,
    working_dir TEXT,
    log_path TEXT,
    exit_code INTEGER,

    -- Timing
    start_time DATETIME,
    end_time DATETIME,
    duration_ms INTEGER,

    -- Retry tracking
    attempt INTEGER NOT NULL DEFAULT 1,
    max_retries INTEGER NOT NULL DEFAULT 0,

    -- Forensic flag (excludes task from run statistics)
    is_forensic BOOLEAN NOT NULL DEFAULT 0,

    -- Matrix metadata
    is_matrix_expansion BOOLEAN NOT NULL DEFAULT 0,
    matrix_vars JSON,

    -- Conditional execution
    condition_expr TEXT,
    condition_result BOOLEAN,

    -- Error handling
    error_message TEXT,
    stack_trace TEXT,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_executions_run_id ON task_executions(run_id);
CREATE INDEX IF NOT EXISTS idx_task_executions_state ON task_executions(run_id, state);
CREATE INDEX IF NOT EXISTS idx_task_executions_task_id ON task_executions(task_id);

-- Variable registry snapshots
CREATE TABLE IF NOT EXISTS context_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    snapshot_time DATETIME NOT NULL,
    snapshot_type TEXT CHECK(snapshot_type IN ('checkpoint', 'final', 'crash')),

    variable_name TEXT NOT NULL,
    variable_value TEXT NOT NULL,
    variable_type TEXT NOT NULL CHECK(variable_type IN ('string', 'int', 'float', 'bool')),

    set_by_task TEXT NOT NULL,
    set_at DATETIME NOT NULL,
    is_read_only BOOLEAN NOT NULL DEFAULT 0,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_context_snapshots_run ON context_snapshots(run_id, snapshot_time);
CREATE INDEX IF NOT EXISTS idx_context_snapshots_var ON context_snapshots(run_id, variable_name);

-- Forensic logs (failure traps, crash dumps)
CREATE TABLE IF NOT EXISTS forensic_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    task_id TEXT,

    log_type TEXT NOT NULL CHECK(log_type IN ('trap_output', 'crash_dump', 'timeout', 'cancellation', 'system_snapshot')),

    log_data TEXT NOT NULL,

    triggered_by TEXT,
    trigger_reason TEXT,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_forensic_logs_run ON forensic_logs(run_id);
CREATE INDEX IF NOT EXISTS idx_forensic_logs_type ON forensic_logs(log_type);

-- DAG structure cache (for fast resume)
CREATE TABLE IF NOT EXISTS dag_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,

    dag_json TEXT NOT NULL,

    total_nodes INTEGER NOT NULL,
    total_levels INTEGER NOT NULL,
    has_parallel_tasks BOOLEAN NOT NULL DEFAULT 0,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE,
    UNIQUE(run_id)
);

-- Dependency tracking (for efficient resume)
CREATE TABLE IF NOT EXISTS task_dependencies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    depends_on_task_id TEXT NOT NULL,

    dependency_satisfied BOOLEAN NOT NULL DEFAULT 0,
    satisfied_at DATETIME,

    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_dependencies_lookup ON task_dependencies(run_id, task_id);

-- Audit trail (for compliance/debugging)
CREATE TABLE IF NOT EXISTS audit_trail (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    event_data JSON NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_audit_trail_run ON audit_trail(run_id, created_at);

-----------------------------------------------------------------------
-- TRIGGERS
-----------------------------------------------------------------------

CREATE TRIGGER IF NOT EXISTS update_runs_timestamp
AFTER UPDATE ON runs
FOR EACH ROW
BEGIN
    UPDATE runs SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS update_task_executions_timestamp
AFTER UPDATE ON task_executions
FOR EACH ROW
BEGIN
    UPDATE task_executions SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS update_run_stats_on_success
AFTER UPDATE OF state ON task_executions
WHEN NEW.state = 'success' AND OLD.state != 'success' AND NEW.is_forensic = 0
BEGIN
    UPDATE runs SET tasks_success = tasks_success + 1 WHERE id = NEW.run_id;
END;

CREATE TRIGGER IF NOT EXISTS update_run_stats_on_failure
AFTER UPDATE OF state ON task_executions
WHEN NEW.state = 'failed' AND OLD.state != 'failed' AND NEW.is_forensic = 0
BEGIN
    UPDATE runs SET tasks_failed = tasks_failed + 1 WHERE id = NEW.run_id;
END;

CREATE TRIGGER IF NOT EXISTS update_run_stats_on_skip
AFTER UPDATE OF state ON task_executions
WHEN NEW.state = 'skipped' AND OLD.state != 'skipped' AND NEW.is_forensic = 0
BEGIN
    UPDATE runs SET tasks_skipped = tasks_skipped + 1 WHERE id = NEW.run_id;
END;
`,
	},
}

// runMigrations creates the schema_version table if necessary, then applies
// every migration whose version is greater than the current schema version.
// Migrations are applied one at a time inside individual transactions so that
// a failure leaves the database at the last successfully applied version.
func runMigrations(db *sql.DB) error {
	// Bootstrap: ensure the version-tracking table exists.
	const bootstrap = `
CREATE TABLE IF NOT EXISTS schema_version (
    version  INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`
	if _, err := db.Exec(bootstrap); err != nil {
		return fmt.Errorf("migrations: bootstrap: %w", err)
	}

	// Read the highest applied version.
	var current int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("migrations: read version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("migrations: begin v%d: %w", m.version, err)
		}

		if _, err := tx.Exec(m.sql); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("migrations: apply v%d: %w", m.version, err)
		}

		if _, err := tx.Exec(
			`INSERT INTO schema_version (version) VALUES (?)`, m.version,
		); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("migrations: record v%d: %w", m.version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migrations: commit v%d: %w", m.version, err)
		}
		logger.Debug("schema migration applied", "version", m.version)
	}

	return nil
}
