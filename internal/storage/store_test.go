package storage_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/silocorp/workflow/internal/logger"
	"github.com/silocorp/workflow/internal/storage"
)

func TestMain(m *testing.M) {
	if err := logger.Init(logger.Config{Level: "error", Format: "console"}); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// openStore creates an isolated SQLite database in a temp directory.
func openStore(t *testing.T) *storage.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// sampleRun builds a minimal Run ready for CreateRun.
func sampleRun(name string) *storage.Run {
	return &storage.Run{
		WorkflowName:  name,
		WorkflowFile:  "/tmp/" + name + ".toml",
		Status:        storage.RunRunning,
		StartTime:     time.Now().UTC().Truncate(time.Second),
		TotalTasks:    3,
		ExecutionMode: storage.ExecutionSequential,
		MaxParallel:   1,
		Tags:          `["ci","test"]`,
	}
}

// ─── Run CRUD ─────────────────────────────────────────────────────────────────

func TestCreateAndGetRun(t *testing.T) {
	s := openStore(t)
	run := sampleRun("create-get")

	id, err := s.CreateRun(run)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty run ID")
	}

	got, err := s.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.WorkflowName != run.WorkflowName {
		t.Errorf("WorkflowName: want %q, got %q", run.WorkflowName, got.WorkflowName)
	}
	if got.Status != storage.RunRunning {
		t.Errorf("Status: want %s, got %s", storage.RunRunning, got.Status)
	}
	if got.TotalTasks != 3 {
		t.Errorf("TotalTasks: want 3, got %d", got.TotalTasks)
	}
}

func TestGetRun_NotFound(t *testing.T) {
	s := openStore(t)
	_, err := s.GetRun("nonexistent-run-id")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateRun(t *testing.T) {
	s := openStore(t)
	id, err := s.CreateRun(sampleRun("update"))
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	run, _ := s.GetRun(id)
	run.Status = storage.RunSuccess
	run.EndTime = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	run.DurationMs = sql.NullInt64{Int64: 500, Valid: true}

	if err := s.UpdateRun(run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	got, _ := s.GetRun(id)
	if got.Status != storage.RunSuccess {
		t.Errorf("Status: want %s, got %s", storage.RunSuccess, got.Status)
	}
	if !got.EndTime.Valid {
		t.Error("EndTime should be set after update")
	}
	if got.DurationMs.Int64 != 500 {
		t.Errorf("DurationMs: want 500, got %d", got.DurationMs.Int64)
	}
}

func TestListRuns_NoFilter(t *testing.T) {
	s := openStore(t)

	for _, name := range []string{"wf-a", "wf-b", "wf-c"} {
		if _, err := s.CreateRun(sampleRun(name)); err != nil {
			t.Fatalf("CreateRun(%s): %v", name, err)
		}
	}

	runs, err := s.ListRuns(storage.RunFilters{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Errorf("expected 3 runs, got %d", len(runs))
	}
}

func TestListRuns_FilterByWorkflowName(t *testing.T) {
	s := openStore(t)

	if _, err := s.CreateRun(sampleRun("alpha")); err != nil {
		t.Fatalf("CreateRun alpha: %v", err)
	}
	if _, err := s.CreateRun(sampleRun("beta")); err != nil {
		t.Fatalf("CreateRun beta: %v", err)
	}

	runs, err := s.ListRuns(storage.RunFilters{WorkflowName: "alpha"})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("expected 1 run for workflow 'alpha', got %d", len(runs))
	}
	if runs[0].WorkflowName != "alpha" {
		t.Errorf("unexpected workflow name: %s", runs[0].WorkflowName)
	}
}

func TestListRuns_FilterByStatus(t *testing.T) {
	s := openStore(t)

	id, _ := s.CreateRun(sampleRun("status-filter"))
	run, _ := s.GetRun(id)
	run.Status = storage.RunFailed
	s.UpdateRun(run) //nolint:errcheck

	if _, err := s.CreateRun(sampleRun("still-running")); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	failed, err := s.ListRuns(storage.RunFilters{Status: storage.RunFailed})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(failed) != 1 {
		t.Errorf("expected 1 failed run, got %d", len(failed))
	}
}

func TestListRuns_FilterByTag(t *testing.T) {
	s := openStore(t)

	tagged := sampleRun("tagged")
	tagged.Tags = `["nightly","prod"]`
	if _, err := s.CreateRun(tagged); err != nil {
		t.Fatalf("CreateRun tagged: %v", err)
	}

	untagged := sampleRun("untagged")
	untagged.Tags = `["dev"]`
	if _, err := s.CreateRun(untagged); err != nil {
		t.Fatalf("CreateRun untagged: %v", err)
	}

	runs, err := s.ListRuns(storage.RunFilters{Tag: "nightly"})
	if err != nil {
		t.Fatalf("ListRuns tag filter: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("expected 1 run tagged 'nightly', got %d", len(runs))
	}
}

func TestListRuns_Limit(t *testing.T) {
	s := openStore(t)

	for i := 0; i < 5; i++ {
		if _, err := s.CreateRun(sampleRun("limit-test")); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
	}

	runs, err := s.ListRuns(storage.RunFilters{Limit: 2})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("expected 2 runs with Limit=2, got %d", len(runs))
	}
}

// ─── TaskExecution CRUD ───────────────────────────────────────────────────────

func sampleTaskExec(runID string) *storage.TaskExecution {
	return &storage.TaskExecution{
		RunID:    runID,
		TaskID:   "task-a",
		TaskName: "Task A",
		State:    storage.TaskPending,
		Command:  "echo hello",
	}
}

func TestCreateAndListTaskExecutions(t *testing.T) {
	s := openStore(t)

	runID, _ := s.CreateRun(sampleRun("task-exec-test"))

	te := sampleTaskExec(runID)
	teID, err := s.CreateTaskExecution(te)
	if err != nil {
		t.Fatalf("CreateTaskExecution: %v", err)
	}
	if teID == "" {
		t.Fatal("expected non-empty task execution ID")
	}

	tasks, err := s.ListTaskExecutions(storage.TaskFilters{RunID: runID})
	if err != nil {
		t.Fatalf("ListTaskExecutions: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task execution, got %d", len(tasks))
	}
	if tasks[0].TaskID != "task-a" {
		t.Errorf("TaskID: want task-a, got %s", tasks[0].TaskID)
	}
}

func TestUpdateTaskExecution(t *testing.T) {
	s := openStore(t)
	runID, _ := s.CreateRun(sampleRun("task-update"))
	teID, _ := s.CreateTaskExecution(sampleTaskExec(runID))

	tasks, _ := s.ListTaskExecutions(storage.TaskFilters{RunID: runID})
	if len(tasks) == 0 {
		t.Fatal("no task executions found")
	}
	te := tasks[0]
	te.State = storage.TaskSuccess
	te.ExitCode = sql.NullInt64{Int64: 0, Valid: true}
	te.LogPath = sql.NullString{String: "/tmp/test.log", Valid: true}
	te.StartTime = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	te.EndTime = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	te.DurationMs = sql.NullInt64{Int64: 42, Valid: true}
	te.Attempt = 1
	_ = teID

	if err := s.UpdateTaskExecution(te); err != nil {
		t.Fatalf("UpdateTaskExecution: %v", err)
	}

	updated, _ := s.ListTaskExecutions(storage.TaskFilters{RunID: runID, State: storage.TaskSuccess})
	if len(updated) != 1 {
		t.Fatalf("expected 1 successful task execution, got %d", len(updated))
	}
	if !updated[0].LogPath.Valid || updated[0].LogPath.String != "/tmp/test.log" {
		t.Errorf("LogPath not persisted: %v", updated[0].LogPath)
	}
}

func TestListTaskExecutions_FilterByState(t *testing.T) {
	s := openStore(t)
	runID, _ := s.CreateRun(sampleRun("filter-state"))

	// Create two tasks in different states.
	te1 := sampleTaskExec(runID)
	te1.State = storage.TaskSuccess
	te1.TaskID = "t1"
	s.CreateTaskExecution(te1) //nolint:errcheck

	te2 := sampleTaskExec(runID)
	te2.State = storage.TaskFailed
	te2.TaskID = "t2"
	id2, _ := s.CreateTaskExecution(te2)

	// Update te2 to failed.
	tasks, _ := s.ListTaskExecutions(storage.TaskFilters{RunID: runID, TaskID: "t2"})
	if len(tasks) > 0 {
		tasks[0].State = storage.TaskFailed
		tasks[0].ExitCode = sql.NullInt64{Int64: 1, Valid: true}
		tasks[0].ErrorMessage = sql.NullString{String: "exit status 1", Valid: true}
		_ = id2
		s.UpdateTaskExecution(tasks[0]) //nolint:errcheck
	}

	failed, err := s.ListTaskExecutions(storage.TaskFilters{RunID: runID, State: storage.TaskFailed})
	if err != nil {
		t.Fatalf("ListTaskExecutions: %v", err)
	}
	if len(failed) != 1 {
		t.Errorf("expected 1 failed task, got %d", len(failed))
	}
}

// ─── Migration idempotency ─────────────────────────────────────────────────────

// TestMigrationIdempotency opens the same DB twice to confirm migrations are
// applied exactly once and do not fail on a pre-existing schema.
func TestMigrationIdempotency(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "idempotent.db")

	s1, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	s1.Close()

	s2, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("second open (migrations should be idempotent): %v", err)
	}
	defer s2.Close()

	// Basic sanity: we can still create a run after second open.
	if _, err := s2.CreateRun(sampleRun("idempotent")); err != nil {
		t.Fatalf("CreateRun after reopen: %v", err)
	}
}

// ─── Ping ─────────────────────────────────────────────────────────────────────

func TestPing(t *testing.T) {
	s := openStore(t)
	if err := s.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// ─── RunTags helper ───────────────────────────────────────────────────────────

func TestRunTags(t *testing.T) {
	run := &storage.Run{Tags: `["ci","prod","nightly"]`}
	tags := run.RunTags()
	if len(tags) != 3 {
		t.Errorf("expected 3 tags, got %d: %v", len(tags), tags)
	}
	if tags[0] != "ci" || tags[1] != "prod" || tags[2] != "nightly" {
		t.Errorf("unexpected tags: %v", tags)
	}
}

func TestRunTags_Empty(t *testing.T) {
	run := &storage.Run{Tags: "[]"}
	if tags := run.RunTags(); len(tags) != 0 {
		t.Errorf("expected empty tags, got %v", tags)
	}
}
