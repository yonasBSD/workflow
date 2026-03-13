package storage

import (
	"database/sql"
	"encoding/json"
	"time"
)

// ─── Enums ────────────────────────────────────────────────────────────────────

type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunSuccess   RunStatus = "success"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
	RunResuming  RunStatus = "resuming"
)

type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskReady     TaskStatus = "ready"
	TaskRunning   TaskStatus = "running"
	TaskSuccess   TaskStatus = "success"
	TaskFailed    TaskStatus = "failed"
	TaskSkipped   TaskStatus = "skipped"
	TaskCancelled TaskStatus = "cancelled"
)

type SnapshotType string

const (
	SnapshotCheckpoint SnapshotType = "checkpoint"
	SnapshotFinal      SnapshotType = "final"
	SnapshotCrash      SnapshotType = "crash"
)

type ForensicLogType string

const (
	ForensicTrapOutput     ForensicLogType = "trap_output"
	ForensicCrashDump      ForensicLogType = "crash_dump"
	ForensicTimeout        ForensicLogType = "timeout"
	ForensicCancellation   ForensicLogType = "cancellation"
	ForensicSystemSnapshot ForensicLogType = "system_snapshot"
)

type ExecutionMode string

const (
	ExecutionSequential   ExecutionMode = "sequential"
	ExecutionParallel     ExecutionMode = "parallel"
	ExecutionWorkStealing ExecutionMode = "work_stealing"
)

// ─── Core Models ──────────────────────────────────────────────────────────────

type Run struct {
	ID             string        `db:"id"`
	WorkflowName   string        `db:"workflow_name"`
	WorkflowFile   string        `db:"workflow_file"`
	Status         RunStatus     `db:"status"`
	StartTime      time.Time     `db:"start_time"`
	EndTime        sql.NullTime  `db:"end_time"`
	DurationMs     sql.NullInt64 `db:"duration_ms"`
	TotalTasks     int           `db:"total_tasks"`
	TasksSuccess   int           `db:"tasks_success"`
	TasksFailed    int           `db:"tasks_failed"`
	TasksSkipped   int           `db:"tasks_skipped"`
	ExecutionMode  ExecutionMode `db:"execution_mode"`
	MaxParallel    int           `db:"max_parallel"`
	ResumeCount    int           `db:"resume_count"`
	LastResumeTime sql.NullTime  `db:"last_resume_time"`
	CreatedAt      time.Time     `db:"created_at"`
	UpdatedAt      time.Time     `db:"updated_at"`
	// Tags is a JSON-encoded string array, e.g. '["production","nightly"]'
	Tags string `db:"tags"`
	// TimeoutMs is the run-level timeout in milliseconds; 0 means no limit.
	TimeoutMs int64 `db:"timeout_ms"`
}

// RunTags decodes the JSON tag array stored in Tags.
func (r *Run) RunTags() []string {
	var tags []string
	if r.Tags == "" || r.Tags == "[]" {
		return tags
	}
	json.Unmarshal([]byte(r.Tags), &tags) //nolint:errcheck
	return tags
}

type TaskExecution struct {
	ID                string         `db:"id"`
	RunID             string         `db:"run_id"`
	TaskID            string         `db:"task_id"`
	TaskName          string         `db:"task_name"`
	State             TaskStatus     `db:"state"`
	Command           string         `db:"command"`
	WorkingDir        sql.NullString `db:"working_dir"`
	LogPath           sql.NullString `db:"log_path"`
	ExitCode          sql.NullInt64  `db:"exit_code"`
	StartTime         sql.NullTime   `db:"start_time"`
	EndTime           sql.NullTime   `db:"end_time"`
	DurationMs        sql.NullInt64  `db:"duration_ms"`
	Attempt           int            `db:"attempt"`
	MaxRetries        int            `db:"max_retries"`
	IsForensic        bool           `db:"is_forensic"`
	IsMatrixExpansion bool           `db:"is_matrix_expansion"`
	MatrixVars        sql.NullString `db:"matrix_vars"` // JSON: {"env":"dev","region":"us-east"}
	ConditionExpr     sql.NullString `db:"condition_expr"`
	ConditionResult   sql.NullBool   `db:"condition_result"`
	ErrorMessage      sql.NullString `db:"error_message"`
	StackTrace        sql.NullString `db:"stack_trace"`
	CreatedAt         time.Time      `db:"created_at"`
	UpdatedAt         time.Time      `db:"updated_at"`
}

type ContextSnapshot struct {
	ID            int64        `db:"id"`
	RunID         string       `db:"run_id"`
	SnapshotTime  time.Time    `db:"snapshot_time"`
	SnapshotType  SnapshotType `db:"snapshot_type"`
	VariableName  string       `db:"variable_name"`
	VariableValue string       `db:"variable_value"`
	VariableType  string       `db:"variable_type"`
	SetByTask     string       `db:"set_by_task"`
	SetAt         time.Time    `db:"set_at"`
	IsReadOnly    bool         `db:"is_read_only"`
	CreatedAt     time.Time    `db:"created_at"`
}

type ForensicLog struct {
	ID            int64           `db:"id"`
	RunID         string          `db:"run_id"`
	TaskID        sql.NullString  `db:"task_id"`
	LogType       ForensicLogType `db:"log_type"`
	LogData       string          `db:"log_data"`
	TriggeredBy   sql.NullString  `db:"triggered_by"`
	TriggerReason sql.NullString  `db:"trigger_reason"`
	CreatedAt     time.Time       `db:"created_at"`
}

type DAGCache struct {
	ID          int64     `db:"id"`
	RunID       string    `db:"run_id"`
	DAGJSON     string    `db:"dag_json"`
	TotalNodes  int       `db:"total_nodes"`
	TotalLevels int       `db:"total_levels"`
	HasParallel bool      `db:"has_parallel"`
	CreatedAt   time.Time `db:"created_at"`
}

type TaskDependency struct {
	ID                  int64        `db:"id"`
	RunID               string       `db:"run_id"`
	TaskID              string       `db:"task_id"`
	DependsOnTaskID     string       `db:"depends_on_task_id"`
	DependencySatisfied bool         `db:"dependency_satisfied"`
	SatisfiedAt         sql.NullTime `db:"satisfied_at"`
}

type AuditTrailEntry struct {
	ID        int64     `db:"id"`
	RunID     string    `db:"run_id"`
	EventType string    `db:"event_type"`
	EventData string    `db:"event_data"` // JSON
	CreatedAt time.Time `db:"created_at"`
}

// ─── Filter / Option Types ────────────────────────────────────────────────────

// RunFilters controls which runs are returned by ListRuns.
// Zero values are ignored (no filter applied).
type RunFilters struct {
	WorkflowName string
	Status       RunStatus
	Tag          string // exact tag match inside the JSON array
	StartAfter   time.Time
	StartBefore  time.Time
	Limit        int // 0 = no limit
	Offset       int
}

// TaskFilters controls which task executions are returned by ListTaskExecutions.
type TaskFilters struct {
	RunID    string
	TaskID   string
	TaskName string
	State    TaskStatus
	Limit    int
	Offset   int
}

// ContextSnapshotFilters controls which context snapshots are returned.
type ContextSnapshotFilters struct {
	RunID        string
	VariableName string
	SnapshotType SnapshotType
	Limit        int
	Offset       int
}

// ForensicLogFilters controls which forensic logs are returned.
type ForensicLogFilters struct {
	RunID   string
	TaskID  string
	LogType ForensicLogType
	Limit   int
	Offset  int
}

// AuditTrailFilters controls which audit trail entries are returned.
type AuditTrailFilters struct {
	RunID     string
	EventType string
	Limit     int
	Offset    int
}
