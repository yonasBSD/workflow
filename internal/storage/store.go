package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/segmentio/ksuid"
	"github.com/silocorp/workflow/internal/logger"
	_ "modernc.org/sqlite"
)

// ErrNotFound is returned by Get* functions when the requested record does not exist.
var ErrNotFound = errors.New("store: record not found")

// ─── Store ────────────────────────────────────────────────────────────────────

// Store is the single access point for all database operations.
// It is safe for concurrent use.
type Store struct {
	db *sql.DB
}

// New opens (or creates) a SQLite database at dbPath, applies all PRAGMA
// settings for safe concurrent use, runs the schema migration, and returns a
// ready-to-use Store.
//
// If the database file does not yet exist it is created with mode 0600 so that
// only the owning user can read it.  The database may contain task output,
// context variable values, and audit trails — all potentially sensitive.
func New(dbPath string) (*Store, error) {
	// Pre-create the file with restrictive permissions before SQLite opens it.
	// If the file already exists we leave its permissions untouched.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
			return nil, fmt.Errorf("store: create db dir: %w", err)
		}
		f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, fmt.Errorf("store: create db file: %w", err)
		}
		f.Close()
	}

	dsn := dbPath +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(" + journalMode() + ")" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=cache_size(-16000)" + // ~16 MB page cache
		"&_pragma=page_size(4096)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}

	// Single connection: WAL handles cross-process concurrent reads; within
	// a single process the pool serialises access naturally.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // connections last for the process lifetime

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	logger.Debug("database opened", "path", dbPath)
	return &Store{db: db}, nil
}

// Ping checks database reachability.
func (s *Store) Ping() error { return s.db.Ping() }

// Close closes the underlying database connection. Switching to DELETE journal
// mode before closing checkpoints the WAL and removes the sidecar files,
// preventing "file in use" errors during test temp-dir cleanup on Windows.
func (s *Store) Close() error {
	_, _ = s.db.Exec("PRAGMA journal_mode=DELETE")
	return s.db.Close()
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func newID() string { return ksuid.New().String() }

// notFoundOrErr returns ErrNotFound when err is sql.ErrNoRows, otherwise err.
func notFoundOrErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// queryBuilder appends WHERE / ORDER BY / LIMIT / OFFSET clauses to a base
// query using safe parameterised placeholders.
type queryBuilder struct {
	where  []string
	args   []any
	order  string
	limit  int
	offset int
}

func (q *queryBuilder) add(expr string, val any) {
	q.where = append(q.where, expr)
	q.args = append(q.args, val)
}

func (q *queryBuilder) build(base string) (string, []any) {
	sb := strings.Builder{}
	sb.WriteString(base)
	if len(q.where) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(q.where, " AND "))
	}
	if q.order != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(q.order)
	}
	if q.limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", q.limit))
	}
	if q.offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %d", q.offset))
	}
	return sb.String(), q.args
}

// ─── Scan helpers ─────────────────────────────────────────────────────────────

func scanRun(r interface{ Scan(...any) error }) (*Run, error) {
	run := &Run{}
	err := r.Scan(
		&run.ID, &run.WorkflowName, &run.WorkflowFile, &run.Status,
		&run.StartTime, &run.EndTime, &run.DurationMs,
		&run.TotalTasks, &run.TasksSuccess, &run.TasksFailed, &run.TasksSkipped,
		&run.ExecutionMode, &run.MaxParallel,
		&run.ResumeCount, &run.LastResumeTime,
		&run.CreatedAt, &run.UpdatedAt,
		&run.Tags, &run.TimeoutMs,
	)
	return run, err
}

func scanTaskExecution(r interface{ Scan(...any) error }) (*TaskExecution, error) {
	t := &TaskExecution{}
	err := r.Scan(
		&t.ID, &t.RunID, &t.TaskID, &t.TaskName, &t.State, &t.Command,
		&t.WorkingDir, &t.LogPath, &t.ExitCode,
		&t.StartTime, &t.EndTime, &t.DurationMs,
		&t.Attempt, &t.MaxRetries,
		&t.IsForensic, &t.IsMatrixExpansion, &t.MatrixVars,
		&t.ConditionExpr, &t.ConditionResult,
		&t.ErrorMessage, &t.StackTrace,
		&t.CreatedAt, &t.UpdatedAt,
	)
	return t, err
}

func scanContextSnapshot(r interface{ Scan(...any) error }) (*ContextSnapshot, error) {
	cs := &ContextSnapshot{}
	err := r.Scan(
		&cs.ID, &cs.RunID, &cs.SnapshotTime, &cs.SnapshotType,
		&cs.VariableName, &cs.VariableValue, &cs.VariableType,
		&cs.SetByTask, &cs.SetAt, &cs.IsReadOnly,
		&cs.CreatedAt,
	)
	return cs, err
}

func scanForensicLog(r interface{ Scan(...any) error }) (*ForensicLog, error) {
	fl := &ForensicLog{}
	err := r.Scan(
		&fl.ID, &fl.RunID, &fl.TaskID, &fl.LogType, &fl.LogData,
		&fl.TriggeredBy, &fl.TriggerReason, &fl.CreatedAt,
	)
	return fl, err
}

func scanTaskDependency(r interface{ Scan(...any) error }) (*TaskDependency, error) {
	td := &TaskDependency{}
	err := r.Scan(
		&td.ID, &td.RunID, &td.TaskID, &td.DependsOnTaskID,
		&td.DependencySatisfied, &td.SatisfiedAt,
	)
	return td, err
}

func scanAuditEntry(r interface{ Scan(...any) error }) (*AuditTrailEntry, error) {
	a := &AuditTrailEntry{}
	err := r.Scan(&a.ID, &a.RunID, &a.EventType, &a.EventData, &a.CreatedAt)
	return a, err
}

// ─── Runs ─────────────────────────────────────────────────────────────────────

// CreateRun inserts a new workflow run and returns its generated ID.
func (s *Store) CreateRun(run *Run) (string, error) {
	id := newID()
	tags := run.Tags
	if tags == "" {
		tags = "[]"
	}
	_, err := s.db.Exec(queryInsertRun,
		id,
		run.WorkflowName,
		run.WorkflowFile,
		run.Status,
		run.StartTime,
		run.TotalTasks,
		run.ExecutionMode,
		run.MaxParallel,
		tags,
		run.TimeoutMs,
	)
	if err != nil {
		return "", fmt.Errorf("store: CreateRun: %w", err)
	}
	logger.Debug("run created", "run_id", id, "workflow", run.WorkflowName, "mode", string(run.ExecutionMode))
	return id, nil
}

// UpdateRun updates the mutable fields of an existing run.
// Task statistics (tasks_success, tasks_failed, tasks_skipped) are managed
// exclusively by database triggers and must not be set here.
func (s *Store) UpdateRun(run *Run) error {
	_, err := s.db.Exec(queryUpdateRun,
		run.Status,
		run.EndTime,
		run.DurationMs,
		run.ResumeCount,
		run.LastResumeTime,
		run.ID,
	)
	if err != nil {
		return fmt.Errorf("store: UpdateRun: %w", err)
	}
	logger.Debug("run updated", "run_id", run.ID, "status", string(run.Status))
	return nil
}

// GetRun retrieves a single run by ID. Returns ErrNotFound if absent.
func (s *Store) GetRun(id string) (*Run, error) {
	row := s.db.QueryRow(querySelectRun, id)
	run, err := scanRun(row)
	if err != nil {
		return nil, fmt.Errorf("store: GetRun: %w", notFoundOrErr(err))
	}
	return run, nil
}

// ListRuns returns runs matching the supplied filters, ordered by start_time
// descending (most recent first).
func (s *Store) ListRuns(f RunFilters) ([]*Run, error) {
	qb := queryBuilder{order: "start_time DESC", limit: f.Limit, offset: f.Offset}

	if f.WorkflowName != "" {
		qb.add("workflow_name = ?", f.WorkflowName)
	}
	if f.Status != "" {
		qb.add("status = ?", string(f.Status))
	}
	if f.Tag != "" {
		// Exact match inside the JSON array using SQLite json_each().
		qb.add("EXISTS (SELECT 1 FROM json_each(tags) WHERE value = ?)", f.Tag)
	}
	if !f.StartAfter.IsZero() {
		qb.add("start_time > ?", f.StartAfter)
	}
	if !f.StartBefore.IsZero() {
		qb.add("start_time < ?", f.StartBefore)
	}

	query, args := qb.build(querySelectRunsBase)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: ListRuns: %w", err)
	}
	defer rows.Close()

	var runs []*Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("store: ListRuns scan: %w", err)
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// ─── Task Executions ──────────────────────────────────────────────────────────

// CreateTaskExecution inserts a new task execution record and returns its
// auto-incremented ID as a string.
func (s *Store) CreateTaskExecution(task *TaskExecution) (string, error) {
	res, err := s.db.Exec(queryInsertTaskExecution,
		task.RunID,
		task.TaskID,
		task.TaskName,
		task.State,
		task.Command,
		task.WorkingDir,
		task.MaxRetries,
		task.IsForensic,
		task.IsMatrixExpansion,
		task.MatrixVars,
		task.ConditionExpr,
		task.ConditionResult,
	)
	if err != nil {
		return "", fmt.Errorf("store: CreateTaskExecution: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("store: CreateTaskExecution last id: %w", err)
	}
	logger.Debug("task execution created",
		"run_id", task.RunID, "task", task.TaskID, "task_name", task.TaskName)
	return fmt.Sprintf("%d", id), nil
}

// UpdateTaskExecution updates the runtime-mutable fields of a task execution.
func (s *Store) UpdateTaskExecution(task *TaskExecution) error {
	_, err := s.db.Exec(queryUpdateTaskExecution,
		task.State,
		task.LogPath,
		task.ExitCode,
		task.StartTime,
		task.EndTime,
		task.DurationMs,
		task.Attempt,
		task.ErrorMessage,
		task.StackTrace,
		task.ConditionResult,
		task.ID,
	)
	if err != nil {
		return fmt.Errorf("store: UpdateTaskExecution: %w", err)
	}
	logger.Debug("task execution updated",
		"run_id", task.RunID, "task", task.TaskID, "state", string(task.State))
	return nil
}

// GetTaskExecution retrieves a single task execution by its row ID.
// Returns ErrNotFound if absent.
func (s *Store) GetTaskExecution(id string) (*TaskExecution, error) {
	row := s.db.QueryRow(querySelectTaskExecution, id)
	task, err := scanTaskExecution(row)
	if err != nil {
		return nil, fmt.Errorf("store: GetTaskExecution: %w", notFoundOrErr(err))
	}
	return task, nil
}

// ListTaskExecutions returns task executions matching the supplied filters,
// ordered by created_at ascending.
func (s *Store) ListTaskExecutions(f TaskFilters) ([]*TaskExecution, error) {
	qb := queryBuilder{order: "created_at ASC", limit: f.Limit, offset: f.Offset}

	if f.RunID != "" {
		qb.add("run_id = ?", f.RunID)
	}
	if f.TaskID != "" {
		qb.add("task_id = ?", f.TaskID)
	}
	if f.TaskName != "" {
		qb.add("task_name = ?", f.TaskName)
	}
	if f.State != "" {
		qb.add("state = ?", string(f.State))
	}

	query, args := qb.build(querySelectTaskExecutionsBase)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: ListTaskExecutions: %w", err)
	}
	defer rows.Close()

	var tasks []*TaskExecution
	for rows.Next() {
		task, err := scanTaskExecution(rows)
		if err != nil {
			return nil, fmt.Errorf("store: ListTaskExecutions scan: %w", err)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// ─── Context Snapshots ────────────────────────────────────────────────────────

// CreateContextSnapshot inserts a single variable snapshot entry.
func (s *Store) CreateContextSnapshot(cs *ContextSnapshot) error {
	snapshotTime := cs.SnapshotTime
	if snapshotTime.IsZero() {
		snapshotTime = time.Now().UTC()
	}
	_, err := s.db.Exec(queryInsertContextSnapshot,
		cs.RunID,
		snapshotTime,
		cs.SnapshotType,
		cs.VariableName,
		cs.VariableValue,
		cs.VariableType,
		cs.SetByTask,
		cs.SetAt,
		cs.IsReadOnly,
	)
	if err != nil {
		return fmt.Errorf("store: CreateContextSnapshot: %w", err)
	}
	return nil
}

// CreateContextSnapshotBatch persists multiple variable snapshots for a run
// inside a single transaction. This is the preferred path for checkpoint/final
// saves.
func (s *Store) CreateContextSnapshotBatch(snapshots []*ContextSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: CreateContextSnapshotBatch begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.Prepare(queryInsertContextSnapshot)
	if err != nil {
		return fmt.Errorf("store: CreateContextSnapshotBatch prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, cs := range snapshots {
		snapshotTime := cs.SnapshotTime
		if snapshotTime.IsZero() {
			snapshotTime = now
		}
		if _, err := stmt.Exec(
			cs.RunID, snapshotTime, cs.SnapshotType,
			cs.VariableName, cs.VariableValue, cs.VariableType,
			cs.SetByTask, cs.SetAt, cs.IsReadOnly,
		); err != nil {
			return fmt.Errorf("store: CreateContextSnapshotBatch exec: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: CreateContextSnapshotBatch commit: %w", err)
	}
	return nil
}

// ListContextSnapshots returns context snapshots matching the supplied filters,
// ordered by snapshot_time descending.
func (s *Store) ListContextSnapshots(f ContextSnapshotFilters) ([]*ContextSnapshot, error) {
	qb := queryBuilder{order: "snapshot_time DESC", limit: f.Limit, offset: f.Offset}

	if f.RunID != "" {
		qb.add("run_id = ?", f.RunID)
	}
	if f.VariableName != "" {
		qb.add("variable_name = ?", f.VariableName)
	}
	if f.SnapshotType != "" {
		qb.add("snapshot_type = ?", string(f.SnapshotType))
	}

	query, args := qb.build(querySelectContextSnapshotsBase)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: ListContextSnapshots: %w", err)
	}
	defer rows.Close()

	var out []*ContextSnapshot
	for rows.Next() {
		cs, err := scanContextSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("store: ListContextSnapshots scan: %w", err)
		}
		out = append(out, cs)
	}
	return out, rows.Err()
}

// ─── Forensic Logs ────────────────────────────────────────────────────────────

// CreateForensicLog inserts a forensic log entry.
func (s *Store) CreateForensicLog(fl *ForensicLog) error {
	_, err := s.db.Exec(queryInsertForensicLog,
		fl.RunID, fl.TaskID, fl.LogType, fl.LogData,
		fl.TriggeredBy, fl.TriggerReason,
	)
	if err != nil {
		return fmt.Errorf("store: CreateForensicLog: %w", err)
	}
	return nil
}

// ListForensicLogs returns forensic logs matching the supplied filters,
// ordered by created_at descending.
func (s *Store) ListForensicLogs(f ForensicLogFilters) ([]*ForensicLog, error) {
	qb := queryBuilder{order: "created_at DESC", limit: f.Limit, offset: f.Offset}

	if f.RunID != "" {
		qb.add("run_id = ?", f.RunID)
	}
	if f.TaskID != "" {
		qb.add("task_id = ?", f.TaskID)
	}
	if f.LogType != "" {
		qb.add("log_type = ?", string(f.LogType))
	}

	query, args := qb.build(querySelectForensicLogsBase)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: ListForensicLogs: %w", err)
	}
	defer rows.Close()

	var logs []*ForensicLog
	for rows.Next() {
		fl, err := scanForensicLog(rows)
		if err != nil {
			return nil, fmt.Errorf("store: ListForensicLogs scan: %w", err)
		}
		logs = append(logs, fl)
	}
	return logs, rows.Err()
}

// ─── DAG Cache ────────────────────────────────────────────────────────────────

// UpsertDAGCache inserts or replaces the DAG cache entry for a run.
// (UNIQUE constraint on run_id triggers the update path on conflict.)
func (s *Store) UpsertDAGCache(cache *DAGCache) error {
	_, err := s.db.Exec(queryUpsertDAGCache,
		cache.RunID,
		cache.DAGJSON,
		cache.TotalNodes,
		cache.TotalLevels,
		cache.HasParallel,
	)
	if err != nil {
		return fmt.Errorf("store: UpsertDAGCache: %w", err)
	}
	return nil
}

// GetDAGCache retrieves the cached DAG for a run.
// Returns ErrNotFound if absent.
func (s *Store) GetDAGCache(runID string) (*DAGCache, error) {
	row := s.db.QueryRow(querySelectDAGCache, runID)
	dc := &DAGCache{}
	err := row.Scan(
		&dc.ID, &dc.RunID, &dc.DAGJSON,
		&dc.TotalNodes, &dc.TotalLevels, &dc.HasParallel,
		&dc.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: GetDAGCache: %w", notFoundOrErr(err))
	}
	return dc, nil
}

// ─── Task Dependencies ────────────────────────────────────────────────────────

// DeleteTaskDependenciesForRun removes all dependency rows associated with a
// run. Called before re-inserting a rebuilt dependency graph on resume, because
// task_dependencies has no unique constraint and duplicate rows would corrupt
// wf-inspect and wf-diff output.
func (s *Store) DeleteTaskDependenciesForRun(runID string) error {
	_, err := s.db.Exec(`DELETE FROM task_dependencies WHERE run_id = ?`, runID)
	if err != nil {
		return fmt.Errorf("store: DeleteTaskDependenciesForRun: %w", err)
	}
	return nil
}

// CreateTaskDependency records that taskID depends on dependsOnTaskID within
// a run.
func (s *Store) CreateTaskDependency(td *TaskDependency) error {
	_, err := s.db.Exec(queryInsertTaskDependency,
		td.RunID, td.TaskID, td.DependsOnTaskID,
	)
	if err != nil {
		return fmt.Errorf("store: CreateTaskDependency: %w", err)
	}
	return nil
}

// CreateTaskDependencyBatch bulk-inserts task dependencies for a run in a
// single transaction.
func (s *Store) CreateTaskDependencyBatch(deps []*TaskDependency) error {
	if len(deps) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: CreateTaskDependencyBatch begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.Prepare(queryInsertTaskDependency)
	if err != nil {
		return fmt.Errorf("store: CreateTaskDependencyBatch prepare: %w", err)
	}
	defer stmt.Close()

	for _, d := range deps {
		if _, err := stmt.Exec(d.RunID, d.TaskID, d.DependsOnTaskID); err != nil {
			return fmt.Errorf("store: CreateTaskDependencyBatch exec: %w", err)
		}
	}

	return tx.Commit()
}

// MarkDependencySatisfied marks a specific task dependency as satisfied.
func (s *Store) MarkDependencySatisfied(runID, taskID, dependsOnTaskID string) error {
	_, err := s.db.Exec(queryMarkDependencySatisfied, runID, taskID, dependsOnTaskID)
	if err != nil {
		return fmt.Errorf("store: MarkDependencySatisfied: %w", err)
	}
	return nil
}

// ListTaskDependencies returns all dependency rows for a specific task in a run.
func (s *Store) ListTaskDependencies(runID, taskID string) ([]*TaskDependency, error) {
	rows, err := s.db.Query(querySelectTaskDependencies, runID, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: ListTaskDependencies: %w", err)
	}
	defer rows.Close()

	var deps []*TaskDependency
	for rows.Next() {
		td, err := scanTaskDependency(rows)
		if err != nil {
			return nil, fmt.Errorf("store: ListTaskDependencies scan: %w", err)
		}
		deps = append(deps, td)
	}
	return deps, rows.Err()
}

// ListPendingDependencies returns all unsatisfied dependencies for a run.
// Useful for determining which tasks are blocked during a resume.
func (s *Store) ListPendingDependencies(runID string) ([]*TaskDependency, error) {
	rows, err := s.db.Query(querySelectPendingDependencies, runID)
	if err != nil {
		return nil, fmt.Errorf("store: ListPendingDependencies: %w", err)
	}
	defer rows.Close()

	var deps []*TaskDependency
	for rows.Next() {
		td, err := scanTaskDependency(rows)
		if err != nil {
			return nil, fmt.Errorf("store: ListPendingDependencies scan: %w", err)
		}
		deps = append(deps, td)
	}
	return deps, rows.Err()
}

// ListAllTaskDependencies returns every dependency row for a run.
// Used by the export command to produce a complete run archive.
func (s *Store) ListAllTaskDependencies(runID string) ([]*TaskDependency, error) {
	rows, err := s.db.Query(querySelectAllTaskDepsForRun, runID)
	if err != nil {
		return nil, fmt.Errorf("store: ListAllTaskDependencies: %w", err)
	}
	defer rows.Close()

	var deps []*TaskDependency
	for rows.Next() {
		td, err := scanTaskDependency(rows)
		if err != nil {
			return nil, fmt.Errorf("store: ListAllTaskDependencies scan: %w", err)
		}
		deps = append(deps, td)
	}
	return deps, rows.Err()
}

// ─── Audit Trail ─────────────────────────────────────────────────────────────

// CreateAuditTrailEntry appends an event to the audit log.
func (s *Store) CreateAuditTrailEntry(entry *AuditTrailEntry) error {
	_, err := s.db.Exec(queryInsertAuditTrailEntry,
		entry.RunID, entry.EventType, entry.EventData,
	)
	if err != nil {
		return fmt.Errorf("store: CreateAuditTrailEntry: %w", err)
	}
	return nil
}

// ListAuditTrail returns audit trail entries matching the supplied filters,
// ordered by created_at ascending.
func (s *Store) ListAuditTrail(f AuditTrailFilters) ([]*AuditTrailEntry, error) {
	qb := queryBuilder{order: "created_at ASC", limit: f.Limit, offset: f.Offset}

	if f.RunID != "" {
		qb.add("run_id = ?", f.RunID)
	}
	if f.EventType != "" {
		qb.add("event_type = ?", f.EventType)
	}

	query, args := qb.build(querySelectAuditTrailBase)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: ListAuditTrail: %w", err)
	}
	defer rows.Close()

	var entries []*AuditTrailEntry
	for rows.Next() {
		a, err := scanAuditEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("store: ListAuditTrail scan: %w", err)
		}
		entries = append(entries, a)
	}
	return entries, rows.Err()
}

// ─── Maintenance ──────────────────────────────────────────────────────────────

// DeleteRunsBefore hard-deletes all runs (and their cascaded child records) with
// a start_time before the given cutoff. Returns the number of runs deleted.
//
// Because ON DELETE CASCADE handles the child tables, only runs need explicit
// deletion; the single transaction keeps it atomic.
func (s *Store) DeleteRunsBefore(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM runs WHERE start_time < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("store: DeleteRunsBefore: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: DeleteRunsBefore rows affected: %w", err)
	}
	return n, nil
}

// DBSizeBytes returns the size of the SQLite database file in bytes.
// It queries the page_count and page_size pragmas rather than the filesystem
// so that it works regardless of WAL mode journaling.
func (s *Store) DBSizeBytes() (int64, error) {
	var pageCount, pageSize int64
	if err := s.db.QueryRow("PRAGMA page_count").Scan(&pageCount); err != nil {
		return 0, fmt.Errorf("store: DBSizeBytes page_count: %w", err)
	}
	if err := s.db.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, fmt.Errorf("store: DBSizeBytes page_size: %w", err)
	}
	return pageCount * pageSize, nil
}

// RunSuccessRate returns the fraction of completed runs (success+failed+cancelled)
// that succeeded in the last n days. Returns -1 if there are no completed runs.
func (s *Store) RunSuccessRate(days int) (float64, error) {
	const q = `
SELECT
    COUNT(*) FILTER (WHERE status = 'success') AS success_count,
    COUNT(*) FILTER (WHERE status IN ('success','failed','cancelled')) AS total_count
FROM runs
WHERE start_time >= datetime('now', ?)
`
	interval := fmt.Sprintf("-%d days", days)
	var successCount, totalCount int64
	if err := s.db.QueryRow(q, interval).Scan(&successCount, &totalCount); err != nil {
		return -1, fmt.Errorf("store: RunSuccessRate: %w", err)
	}
	if totalCount == 0 {
		return -1, nil
	}
	return float64(successCount) / float64(totalCount), nil
}

// StaleRunCount returns the number of runs still in 'running' or 'resuming'
// state older than staleMinutes minutes. These are likely orphaned processes.
func (s *Store) StaleRunCount(staleMinutes int) (int64, error) {
	const q = `
SELECT COUNT(*) FROM runs
WHERE status IN ('running','resuming')
AND start_time < datetime('now', ?)
`
	interval := fmt.Sprintf("-%d minutes", staleMinutes)
	var count int64
	if err := s.db.QueryRow(q, interval).Scan(&count); err != nil {
		return 0, fmt.Errorf("store: StaleRunCount: %w", err)
	}
	return count, nil
}
