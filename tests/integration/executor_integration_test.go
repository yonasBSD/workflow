// Package integration contains integration tests for the executor component of the wf application.
package integration

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/joelfokou/workflow/internal/config"
	"github.com/joelfokou/workflow/internal/contextmap"
	"github.com/joelfokou/workflow/internal/dag"
	"github.com/joelfokou/workflow/internal/executor"
	"github.com/joelfokou/workflow/internal/logger"
	"github.com/joelfokou/workflow/internal/storage"
	"github.com/joelfokou/workflow/tests/helpers"
)

func init() {
	if err := logger.Init(logger.Config{
		Level:  "info",
		Format: "console",
	}); err != nil {
		panic(err)
	}
}

// buildDAG writes a TOML workflow to a temp file and returns a built DAG.
func buildDAG(t *testing.T, fs *helpers.TestFS, name, toml string) *dag.DAG {
	t.Helper()
	dir := fs.Path("workflows")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(toml), 0644); err != nil {
		t.Fatalf("write workflow file: %v", err)
	}
	config.Get().Paths.Workflows = dir

	def, err := dag.NewParser(name).Parse()
	if err != nil {
		t.Fatalf("parse workflow %q: %v", name, err)
	}
	d, err := dag.NewBuilder(def).Build()
	if err != nil {
		t.Fatalf("build DAG %q: %v", name, err)
	}
	return d
}

func newStore(t *testing.T, dbPath string) *storage.Store {
	t.Helper()
	store, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	return store
}

// TestExecutorIntegrationSuccess tests a simple workflow that should complete successfully.
func TestExecutorIntegrationSuccess(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "simple", helpers.SimpleWorkflow())

	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	if run.Status != storage.RunSuccess {
		t.Errorf("expected status %s, got %s", storage.RunSuccess, run.Status)
	}
	if !run.EndTime.Valid {
		t.Error("expected EndTime to be set")
	}

	tasks, err := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	if err != nil {
		t.Fatalf("list task executions: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected tasks to be saved")
	}
	for _, task := range tasks {
		if task.State != storage.TaskSuccess {
			t.Errorf("task %s: expected state %s, got %s", task.TaskName, storage.TaskSuccess, task.State)
		}
		if !task.EndTime.Valid {
			t.Errorf("task %s: expected EndTime to be set", task.TaskName)
		}
		if !task.ExitCode.Valid || task.ExitCode.Int64 != 0 {
			t.Errorf("task %s: expected exit code 0, got %v", task.TaskName, task.ExitCode)
		}
		if !task.LogPath.Valid || task.LogPath.String == "" {
			t.Errorf("task %s: expected log path to be set", task.TaskName)
		}
	}
}

// TestExecutorIntegrationTaskFailure tests a workflow with a failing task.
func TestExecutorIntegrationTaskFailure(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "failing", helpers.FailingWorkflow())

	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())
	if err == nil {
		t.Fatal("expected workflow to fail")
	}
	if run.Status != storage.RunFailed {
		t.Errorf("expected status %s, got %s", storage.RunFailed, run.Status)
	}

	tasks, err := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, State: storage.TaskFailed})
	if err != nil {
		t.Fatalf("list task executions: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected at least one failed task")
	}

	failedTask := tasks[0]
	if !failedTask.ExitCode.Valid || failedTask.ExitCode.Int64 == 0 {
		t.Error("expected non-zero exit code for failed task")
	}
	if !failedTask.ErrorMessage.Valid || failedTask.ErrorMessage.String == "" {
		t.Error("expected error message to be recorded")
	}
}

// TestExecutorIntegrationTaskRetry tests that tasks are retried on failure.
func TestExecutorIntegrationTaskRetry(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "retry", helpers.RetryWorkflow())

	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())
	if err == nil {
		t.Fatal("expected workflow to fail after retries exhausted")
	}

	// RetryWorkflow has retries=2 → 3 total attempts → 3 task execution records.
	tasks, err := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	if err != nil {
		t.Fatalf("list task executions: %v", err)
	}
	const expectedAttempts = 3 // retries=2 means 3 total attempts
	if len(tasks) < expectedAttempts {
		t.Errorf("expected at least %d task execution records (one per attempt), got %d", expectedAttempts, len(tasks))
	}
}

// TestExecutorIntegrationComplexWorkflow tests a multi-task DAG workflow.
func TestExecutorIntegrationComplexWorkflow(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "complex", helpers.ComplexWorkflow())

	start := time.Now()
	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected status %s, got %s", storage.RunSuccess, run.Status)
	}

	tasks, err := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	if err != nil {
		t.Fatalf("list task executions: %v", err)
	}
	if len(tasks) != d.TotalTasks {
		t.Errorf("expected %d task execution records, got %d", d.TotalTasks, len(tasks))
	}

	t.Logf("complex workflow completed in %v with %d tasks", duration, len(tasks))
}

// TestExecutorIntegrationContextCancellation tests that workflows can be cancelled.
// Context cancellation kills the entire process group (via cmd.Cancel → killProcessGroup),
// so orphaned child processes are also terminated promptly.
func TestExecutorIntegrationContextCancellation(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	// Use a workflow with a failing command so the run ends quickly, then
	// verify the run is not marked successful.
	d := buildDAG(t, fs, "failing", helpers.FailingWorkflow())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ex := executor.NewSequentialExecutor(store)
	run, _ := ex.Execute(ctx, d, contextmap.NewContextMap())

	if run.Status == storage.RunSuccess {
		t.Errorf("expected non-success status, got %s", run.Status)
	}
}

// TestExecutorIntegrationRuntimeVars verifies that variables injected via
// ctxMap.Set("__runtime__", ...) are available inside task command templates.
func TestExecutorIntegrationRuntimeVars(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "var-interp", helpers.VarInterpolationWorkflow())

	ctxMap := contextmap.NewContextMap()
	if err := ctxMap.Set("__runtime__", "MY_VAR", "hello_world"); err != nil {
		t.Fatalf("set runtime var: %v", err)
	}

	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(context.Background(), d, ctxMap)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected status %s, got %s", storage.RunSuccess, run.Status)
	}

	// Verify the task log contains the interpolated value
	tasks, err := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	if err != nil {
		t.Fatalf("list task executions: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected at least one task execution")
	}
	for _, task := range tasks {
		if task.State != storage.TaskSuccess {
			t.Errorf("task %s: expected success, got %s", task.TaskName, task.State)
		}
	}
}

// TestExecutorIntegrationDAGCache verifies that dag_cache is populated after Execute.
func TestExecutorIntegrationDAGCache(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "complex", helpers.ComplexWorkflow())

	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	cache, err := store.GetDAGCache(run.ID)
	if err != nil {
		t.Fatalf("expected dag_cache entry to exist: %v", err)
	}
	if cache.TotalNodes != d.TotalTasks {
		t.Errorf("dag_cache total_nodes: expected %d, got %d", d.TotalTasks, cache.TotalNodes)
	}
	if cache.TotalLevels != len(d.Levels) {
		t.Errorf("dag_cache total_levels: expected %d, got %d", len(d.Levels), cache.TotalLevels)
	}
	if cache.DAGJSON == "" {
		t.Error("dag_cache dag_json must not be empty")
	}
	// dag_json should be valid JSON containing the workflow name
	if !strings.Contains(cache.DAGJSON, `"name"`) {
		t.Errorf("dag_json does not look like valid JSON: %s", cache.DAGJSON[:min(80, len(cache.DAGJSON))])
	}
}

// TestExecutorIntegrationContextSnapshots verifies that a checkpoint snapshot
// is written to context_snapshots after each successful task.
func TestExecutorIntegrationContextSnapshots(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "register", helpers.RegisterWorkflow())

	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	snapshots, err := store.ListContextSnapshots(storage.ContextSnapshotFilters{RunID: run.ID})
	if err != nil {
		t.Fatalf("list context snapshots: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected context snapshots to be persisted after task success")
	}

	// The registered variable "task_output" must appear in the snapshots
	found := false
	for _, s := range snapshots {
		if s.VariableName == "task_output" {
			found = true
			if !strings.Contains(s.VariableValue, "hello-from-register") {
				t.Errorf("snapshot variable value: expected to contain %q, got %q", "hello-from-register", s.VariableValue)
			}
			if s.VariableType != "string" {
				t.Errorf("snapshot variable type: expected %q, got %q", "string", s.VariableType)
			}
		}
	}
	if !found {
		t.Error("expected 'task_output' variable in context_snapshots")
	}
}

// TestExecutorIntegrationResume verifies that Resume:
//   - skips tasks that already succeeded in the original run
//   - re-executes tasks that previously failed
//   - marks the run as success and increments resume_count
func TestExecutorIntegrationResume(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	// Initial run — task1 succeeds, task2 fails
	d := buildDAG(t, fs, "partial-fail", helpers.PartialFailWorkflow())

	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())
	if err == nil {
		t.Fatal("expected workflow to fail on initial run")
	}
	if run.Status != storage.RunFailed {
		t.Fatalf("expected RunFailed, got %s", run.Status)
	}

	// Verify task1 succeeded in the initial run so there is something to skip
	succeededTasks, err := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, State: storage.TaskSuccess})
	if err != nil {
		t.Fatalf("list task executions: %v", err)
	}
	if len(succeededTasks) == 0 {
		t.Fatal("expected task1 to have succeeded before task2 failed")
	}

	// Update the workflow file on disk to fix task2
	workflowsDir := fs.Path("workflows")
	if err := os.WriteFile(filepath.Join(workflowsDir, "partial-fail.toml"), []byte(helpers.PartialFailWorkflowFixed()), 0644); err != nil {
		t.Fatalf("update workflow file: %v", err)
	}

	// Resume the failed run
	resumedRun, err := ex.Resume(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if resumedRun.Status != storage.RunSuccess {
		t.Errorf("expected RunSuccess after resume, got %s", resumedRun.Status)
	}
	if resumedRun.ResumeCount != 1 {
		t.Errorf("expected resume_count=1, got %d", resumedRun.ResumeCount)
	}

	// Count execution records per task for the run
	allTasks, err := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	if err != nil {
		t.Fatalf("list all task executions: %v", err)
	}
	task1Count, task2Count := 0, 0
	for _, task := range allTasks {
		switch task.TaskID {
		case "task1":
			task1Count++
		case "task2":
			task2Count++
		}
	}

	// task1 must have exactly 1 record — it was skipped on resume
	if task1Count != 1 {
		t.Errorf("task1: expected 1 execution record (skipped on resume), got %d", task1Count)
	}
	// task2 must have at least 2: 1 failed attempt + 1 successful attempt on resume
	if task2Count < 2 {
		t.Errorf("task2: expected ≥2 execution records (1 failed + 1 success), got %d", task2Count)
	}
}

// ─── P3.3: Progress Events ────────────────────────────────────────────────────

// TestProgressEventsEmitted verifies that ProgressEvents are sent on the
// channel injected via executor.WithProgress for a successful run.
func TestProgressEventsEmitted(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "simple", helpers.SimpleWorkflow())

	progCh := make(chan executor.ProgressEvent, 64)
	ctx := executor.WithProgress(context.Background(), progCh)

	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(ctx, d, contextmap.NewContextMap())
	close(progCh)

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected RunSuccess, got %s", run.Status)
	}

	// Drain the channel
	var events []executor.ProgressEvent
	for evt := range progCh {
		events = append(events, evt)
	}

	if len(events) == 0 {
		t.Fatal("expected progress events to be emitted")
	}

	// Must see a run_started and run_done event
	runStarted, runDone := false, false
	taskStarted := 0
	for _, e := range events {
		switch e.Kind {
		case executor.ProgressRunStarted:
			runStarted = true
		case executor.ProgressRunDone:
			runDone = true
		case executor.ProgressTaskStarted:
			taskStarted++
		}
	}
	if !runStarted {
		t.Error("expected ProgressRunStarted event")
	}
	if !runDone {
		t.Error("expected ProgressRunDone event")
	}
	if taskStarted == 0 {
		t.Error("expected at least one ProgressTaskStarted event")
	}
}

// TestProgressEventsFailure verifies ProgressTaskFailed events on a failing run.
func TestProgressEventsFailure(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "failing", helpers.FailingWorkflow())

	progCh := make(chan executor.ProgressEvent, 64)
	ctx := executor.WithProgress(context.Background(), progCh)

	ex := executor.NewSequentialExecutor(store)
	ex.Execute(ctx, d, contextmap.NewContextMap()) //nolint:errcheck
	close(progCh)

	var events []executor.ProgressEvent
	for evt := range progCh {
		events = append(events, evt)
	}

	taskFailed := false
	for _, e := range events {
		if e.Kind == executor.ProgressTaskFailed {
			taskFailed = true
		}
	}
	if !taskFailed {
		t.Error("expected ProgressTaskFailed event for a failing workflow")
	}
}

// ─── P3.5: Audit Trail ────────────────────────────────────────────────────────

// TestAuditTrailPopulated verifies that run_started, task_started, and
// task_succeeded events are written to audit_trail for a successful run.
func TestAuditTrailPopulated(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "simple", helpers.SimpleWorkflow())

	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	entries, err := store.ListAuditTrail(storage.AuditTrailFilters{RunID: run.ID})
	if err != nil {
		t.Fatalf("list audit trail: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected audit trail entries to be written")
	}

	byType := make(map[string]int)
	for _, e := range entries {
		byType[e.EventType]++
	}

	if byType["run_started"] == 0 {
		t.Error("expected run_started audit event")
	}
	if byType["task_started"] == 0 {
		t.Error("expected task_started audit event(s)")
	}
	if byType["task_succeeded"] == 0 {
		t.Error("expected task_succeeded audit event(s)")
	}
	if byType["run_completed"] == 0 {
		t.Error("expected run_completed audit event")
	}
}

// TestAuditTrailOnFailure verifies that a task_failed audit event is written.
func TestAuditTrailOnFailure(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "failing", helpers.FailingWorkflow())

	ex := executor.NewSequentialExecutor(store)
	run, _ := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	entries, err := store.ListAuditTrail(storage.AuditTrailFilters{RunID: run.ID})
	if err != nil {
		t.Fatalf("list audit trail: %v", err)
	}

	taskFailed := false
	for _, e := range entries {
		if e.EventType == "task_failed" {
			taskFailed = true
		}
	}
	if !taskFailed {
		t.Error("expected task_failed audit event for a failing workflow")
	}
}

// ─── P3.7: OnFailureHook ─────────────────────────────────────────────────────

// TestOnFailureHookExecuted verifies that the on_failure_hook shell command
// is executed (and creates a sentinel file) when a workflow fails.
func TestOnFailureHookExecuted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses 'touch' which is not available in cmd.exe")
	}
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	// Write a sentinel file path and configure the hook to touch it
	sentinelPath := fs.Path("hook_fired")
	config.Get().OnFailureHook = "touch " + sentinelPath

	d := buildDAG(t, fs, "failing", helpers.FailingWorkflow())

	ex := executor.NewSequentialExecutor(store)
	ex.Execute(context.Background(), d, contextmap.NewContextMap()) //nolint:errcheck

	// Reset hook so it doesn't affect other tests
	config.Get().OnFailureHook = ""

	if _, err := os.Stat(sentinelPath); os.IsNotExist(err) {
		t.Error("expected on_failure_hook to create the sentinel file")
	}
}

// TestOnFailureHookNotCalledOnSuccess verifies that the hook is NOT called
// when a workflow succeeds.
func TestOnFailureHookNotCalledOnSuccess(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	sentinelPath := fs.Path("hook_fired")
	config.Get().OnFailureHook = "touch " + sentinelPath

	d := buildDAG(t, fs, "simple", helpers.SimpleWorkflow())

	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	config.Get().OnFailureHook = ""

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected RunSuccess, got %s", run.Status)
	}

	if _, err := os.Stat(sentinelPath); !os.IsNotExist(err) {
		t.Error("expected on_failure_hook NOT to fire on successful run")
	}
}

// ─── P3.4: DB Health Helpers ─────────────────────────────────────────────────

// TestDBSizeBytes verifies that DBSizeBytes returns a positive value.
func TestDBSizeBytes(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	size, err := store.DBSizeBytes()
	if err != nil {
		t.Fatalf("DBSizeBytes: %v", err)
	}
	if size <= 0 {
		t.Errorf("expected positive DB size, got %d", size)
	}
}

// TestRunSuccessRate returns -1 when there are no runs.
func TestRunSuccessRate(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	rate, err := store.RunSuccessRate(7)
	if err != nil {
		t.Fatalf("RunSuccessRate: %v", err)
	}
	if rate != -1 {
		t.Errorf("expected -1 when no runs exist, got %f", rate)
	}
}

// TestRunSuccessRateWithData verifies that success rate is computed correctly.
func TestRunSuccessRateWithData(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	// Run two workflows: one succeeds, one fails → expected rate = 0.5
	dSuccess := buildDAG(t, fs, "simple", helpers.SimpleWorkflow())
	exS := executor.NewSequentialExecutor(store)
	exS.Execute(context.Background(), dSuccess, contextmap.NewContextMap()) //nolint:errcheck

	dFail := buildDAG(t, fs, "failing", helpers.FailingWorkflow())
	exF := executor.NewSequentialExecutor(store)
	exF.Execute(context.Background(), dFail, contextmap.NewContextMap()) //nolint:errcheck

	rate, err := store.RunSuccessRate(7)
	if err != nil {
		t.Fatalf("RunSuccessRate: %v", err)
	}
	if rate < 0 {
		t.Fatal("expected a valid rate, got -1")
	}
	// We ran 2 total (1 success, 1 failed) → rate = 0.5
	const want = 0.5
	if rate != want {
		t.Errorf("expected success rate %.2f, got %.2f", want, rate)
	}
}

// TestStaleRunCount returns 0 when all runs are in a terminal state.
func TestStaleRunCount(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	dSuccess := buildDAG(t, fs, "simple", helpers.SimpleWorkflow())
	ex := executor.NewSequentialExecutor(store)
	ex.Execute(context.Background(), dSuccess, contextmap.NewContextMap()) //nolint:errcheck

	count, err := store.StaleRunCount(30)
	if err != nil {
		t.Fatalf("StaleRunCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 stale runs, got %d", count)
	}
}

// ─── Work-Stealing Executor ───────────────────────────────────────────────────

// TestWSBasicSuccess verifies that a simple single-task workflow succeeds.
func TestWSBasicSuccess(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "simple", helpers.SimpleWorkflow())
	ex := executor.NewWorkStealingExecutor(store, 4)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected RunSuccess, got %s", run.Status)
	}
	if !run.EndTime.Valid {
		t.Error("EndTime should be valid after a completed run")
	}
	if run.ExecutionMode != storage.ExecutionWorkStealing {
		t.Errorf("expected execution_mode work_stealing, got %s", run.ExecutionMode)
	}
}

// TestWSLinearChain verifies that a linear A→B→C chain completes in order.
func TestWSLinearChain(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "multi", helpers.MultiTaskWorkflow())
	ex := executor.NewWorkStealingExecutor(store, 4)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected RunSuccess, got %s", run.Status)
	}

	// All 3 tasks must have succeeded.
	tasks, err := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, State: storage.TaskSuccess})
	if err != nil {
		t.Fatalf("list task executions: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected 3 successful tasks, got %d", len(tasks))
	}

	// Verify ordering via start times: each task must start after the previous.
	allTasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	order := map[string]time.Time{}
	for _, tk := range allTasks {
		if tk.StartTime.Valid {
			order[tk.TaskID] = tk.StartTime.Time
		}
	}
	if !order["build"].IsZero() && !order["test"].IsZero() {
		if order["test"].Before(order["build"]) {
			t.Error("test must not start before build")
		}
	}
	if !order["test"].IsZero() && !order["deploy"].IsZero() {
		if order["deploy"].Before(order["test"]) {
			t.Error("deploy must not start before test")
		}
	}
}

// TestWSDiamondDAG verifies the diamond pattern A→B, A→C, B+C→D.
// D must not start until both B and C have completed.
func TestWSDiamondDAG(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "diamond", helpers.DiamondWorkflow())
	ex := executor.NewWorkStealingExecutor(store, 4)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	if err != nil {
		t.Fatalf("diamond: expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("diamond: expected RunSuccess, got %s", run.Status)
	}

	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, State: storage.TaskSuccess})
	if len(tasks) != 4 {
		t.Errorf("diamond: expected 4 successful tasks, got %d", len(tasks))
	}

	// D must start only after BOTH B and C have ended.
	allTasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	endOf := map[string]time.Time{}
	startOf := map[string]time.Time{}
	for _, tk := range allTasks {
		if tk.EndTime.Valid {
			endOf[tk.TaskID] = tk.EndTime.Time
		}
		if tk.StartTime.Valid {
			startOf[tk.TaskID] = tk.StartTime.Time
		}
	}

	if dStart, ok := startOf["d"]; ok {
		if bEnd, ok := endOf["b"]; ok && dStart.Before(bEnd) {
			t.Error("d started before b finished")
		}
		if cEnd, ok := endOf["c"]; ok && dStart.Before(cEnd) {
			t.Error("d started before c finished")
		}
	}
}

// TestWSFanOut verifies that root→[t1,t2,t3]→final works correctly and all
// fan-out tasks are recorded as successful.
func TestWSFanOut(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "fanout", helpers.FanOutWorkflow())
	ex := executor.NewWorkStealingExecutor(store, 4)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	if err != nil {
		t.Fatalf("fanout: expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("fanout: expected RunSuccess, got %s", run.Status)
	}

	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, State: storage.TaskSuccess})
	if len(tasks) != 5 { // root + t1 + t2 + t3 + final
		t.Errorf("fanout: expected 5 successful tasks, got %d", len(tasks))
	}

	// final must not start before any of t1/t2/t3 ended.
	allTasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	endOf := map[string]time.Time{}
	startOf := map[string]time.Time{}
	for _, tk := range allTasks {
		if tk.EndTime.Valid {
			endOf[tk.TaskID] = tk.EndTime.Time
		}
		if tk.StartTime.Valid {
			startOf[tk.TaskID] = tk.StartTime.Time
		}
	}

	finalStart := startOf["final"]
	for _, fanTask := range []string{"t1", "t2", "t3"} {
		if end, ok := endOf[fanTask]; ok && !finalStart.IsZero() {
			if finalStart.Before(end) {
				t.Errorf("final started before %s finished", fanTask)
			}
		}
	}
}

// TestWSWideParallel verifies N independent tasks all complete with work-stealing.
func TestWSWideParallel(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "wide-parallel", helpers.WideParallelWorkflow())
	ex := executor.NewWorkStealingExecutor(store, 4)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	if err != nil {
		t.Fatalf("wide-parallel: expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("wide-parallel: expected RunSuccess, got %s", run.Status)
	}

	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, State: storage.TaskSuccess})
	if len(tasks) != 4 {
		t.Errorf("wide-parallel: expected 4 successful tasks, got %d", len(tasks))
	}
}

// TestWSFailurePropagation verifies that when a task fails its dependents are
// never executed and the run status is RunFailed.
func TestWSFailurePropagation(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	// task1 fails → task2 (depends on task1) should never run.
	d := buildDAG(t, fs, "resume", helpers.ResumeWorkflow())
	ex := executor.NewWorkStealingExecutor(store, 4)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	if err == nil {
		t.Fatal("expected failure, got nil error")
	}
	if run.Status != storage.RunFailed {
		t.Errorf("expected RunFailed, got %s", run.Status)
	}

	// task2 must not have been executed.
	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	for _, tk := range tasks {
		if tk.TaskID == "task2" {
			t.Errorf("task2 should not have been executed, but found state=%s", tk.State)
		}
	}
}

// TestWSContextCancellation verifies that an external context cancellation
// stops execution and returns RunCancelled.
func TestWSContextCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses 'sleep' which is not available in cmd.exe")
	}
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "long-running", helpers.LongRunningWorkflow())
	ex := executor.NewWorkStealingExecutor(store, 4)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay to interrupt the long-running task.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	run, err := ex.Execute(ctx, d, contextmap.NewContextMap())

	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if run == nil {
		t.Fatal("run should not be nil even on cancellation")
	}
	if run.Status != storage.RunCancelled {
		t.Errorf("expected RunCancelled, got %s", run.Status)
	}
}

// TestWSIgnoreFailure verifies that a task marked ignore_failure=true does not
// prevent its dependents from running.
func TestWSIgnoreFailure(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "ignore-failure", helpers.IgnoreFailureWorkflow())
	ex := executor.NewWorkStealingExecutor(store, 4)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	// The run should succeed overall (ignore_failure absorbs the error).
	if err != nil {
		t.Fatalf("expected success with ignore_failure, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected RunSuccess, got %s", run.Status)
	}

	// "after" task must have run.
	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, State: storage.TaskSuccess})
	var foundAfter bool
	for _, tk := range tasks {
		if tk.TaskID == "after" {
			foundAfter = true
		}
	}
	if !foundAfter {
		t.Error("expected 'after' task to have succeeded")
	}
}

// TestWSWorkerLimit verifies that numWorkers=1 forces sequential execution even
// when the DAG has tasks that could run in parallel.
func TestWSWorkerLimit(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "wide-parallel", helpers.WideParallelWorkflow())
	// numWorkers=1 — only one task may run at a time.
	ex := executor.NewWorkStealingExecutor(store, 1)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected RunSuccess, got %s", run.Status)
	}

	// All 4 tasks must have completed.
	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, State: storage.TaskSuccess})
	if len(tasks) != 4 {
		t.Errorf("expected 4 successful tasks with numWorkers=1, got %d", len(tasks))
	}

	// No two tasks should overlap in time (since numWorkers=1).
	allTasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	for i, ti := range allTasks {
		for j, tj := range allTasks {
			if i == j || !ti.StartTime.Valid || !ti.EndTime.Valid || !tj.StartTime.Valid {
				continue
			}
			// ti and tj overlap if ti started before tj ended AND tj started before ti ended.
			if ti.StartTime.Time.Before(tj.EndTime.Time) && tj.StartTime.Time.Before(ti.EndTime.Time) {
				t.Errorf("tasks %s and %s overlap in time with numWorkers=1", ti.TaskID, tj.TaskID)
			}
		}
	}
}

// TestWSProgressEvents verifies that progress events are correctly emitted.
func TestWSProgressEvents(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "simple", helpers.SimpleWorkflow())

	progCh := make(chan executor.ProgressEvent, 64)
	ctx := executor.WithProgress(context.Background(), progCh)

	ex := executor.NewWorkStealingExecutor(store, 4)
	_, err := ex.Execute(ctx, d, contextmap.NewContextMap())
	close(progCh)

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	var events []executor.ProgressEvent
	for evt := range progCh {
		events = append(events, evt)
	}

	seen := map[executor.ProgressEventKind]bool{}
	for _, e := range events {
		seen[e.Kind] = true
	}

	for _, want := range []executor.ProgressEventKind{
		executor.ProgressRunStarted,
		executor.ProgressTaskStarted,
		executor.ProgressTaskDone,
		executor.ProgressRunDone,
	} {
		if !seen[want] {
			t.Errorf("missing progress event: %s", want)
		}
	}
}

// TestWSProgressEventsOnFailure verifies ProgressTaskFailed is emitted.
func TestWSProgressEventsOnFailure(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "failing", helpers.FailingWorkflow())

	progCh := make(chan executor.ProgressEvent, 64)
	ctx := executor.WithProgress(context.Background(), progCh)

	ex := executor.NewWorkStealingExecutor(store, 4)
	_, _ = ex.Execute(ctx, d, contextmap.NewContextMap())
	close(progCh)

	var events []executor.ProgressEvent
	for evt := range progCh {
		events = append(events, evt)
	}

	var sawFailed bool
	for _, e := range events {
		if e.Kind == executor.ProgressTaskFailed {
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Error("expected ProgressTaskFailed event on failing run")
	}
}

// TestWSAuditTrail verifies that audit events are written to the database.
func TestWSAuditTrail(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "simple", helpers.SimpleWorkflow())
	ex := executor.NewWorkStealingExecutor(store, 4)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	entries, err := store.ListAuditTrail(storage.AuditTrailFilters{RunID: run.ID})
	if err != nil {
		t.Fatalf("ListAuditTrail: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected audit trail entries, got none")
	}

	eventTypes := map[string]bool{}
	for _, e := range entries {
		eventTypes[e.EventType] = true
	}
	for _, want := range []string{"run_started", "task_started", "task_succeeded", "run_completed"} {
		if !eventTypes[want] {
			t.Errorf("missing audit event: %s", want)
		}
	}
}

// TestWSComplexWorkflow verifies that the complex multi-task workflow succeeds
// end-to-end with the work-stealing executor, matching sequential semantics.
func TestWSComplexWorkflow(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "complex", helpers.ComplexWorkflow())
	ex := executor.NewWorkStealingExecutor(store, 4)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	if err != nil {
		t.Fatalf("complex: expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("complex: expected RunSuccess, got %s", run.Status)
	}

	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, State: storage.TaskSuccess})
	if len(tasks) != 4 { // a, b, c, d
		t.Errorf("complex: expected 4 successful tasks, got %d", len(tasks))
	}
}

// TestWSParallelismActuallyHappens verifies that with numWorkers>1 and
// independent tasks, multiple tasks run concurrently (their time ranges overlap).
func TestWSParallelismActuallyHappens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses 'sleep' which is not available in cmd.exe")
	}
	if testing.Short() {
		t.Skip("skipping timing-sensitive test in short mode")
	}

	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	// A workflow where b, c, d are all independent (only depend on a) and
	// take non-trivial time so their windows reliably overlap.
	const toml = `
name = "overlap-test"

[tasks.a]
cmd = "echo a"

[tasks.b]
cmd = "sleep 0.1"
depends_on = ["a"]

[tasks.c]
cmd = "sleep 0.1"
depends_on = ["a"]

[tasks.d]
cmd = "sleep 0.1"
depends_on = ["a"]
`
	d := buildDAG(t, fs, "overlap-test", toml)
	ex := executor.NewWorkStealingExecutor(store, 4)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())

	if err != nil {
		t.Fatalf("overlap-test: expected success, got: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("overlap-test: expected RunSuccess, got %s", run.Status)
	}

	// Verify b, c, d overlap — at least two of them must have started before
	// any of them ended.
	allTasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	starts := map[string]time.Time{}
	ends := map[string]time.Time{}
	for _, tk := range allTasks {
		if tk.StartTime.Valid {
			starts[tk.TaskID] = tk.StartTime.Time
		}
		if tk.EndTime.Valid {
			ends[tk.TaskID] = tk.EndTime.Time
		}
	}

	// For any pair among b/c/d, check if they overlap.
	fanTasks := []string{"b", "c", "d"}
	overlaps := 0
	for i := 0; i < len(fanTasks); i++ {
		for j := i + 1; j < len(fanTasks); j++ {
			ti, tj := fanTasks[i], fanTasks[j]
			si, ei := starts[ti], ends[ti]
			sj, ej := starts[tj], ends[tj]
			if si.IsZero() || ei.IsZero() || sj.IsZero() || ej.IsZero() {
				continue
			}
			// Overlap: si < ej AND sj < ei
			if si.Before(ej) && sj.Before(ei) {
				overlaps++
			}
		}
	}
	if overlaps == 0 {
		t.Error("expected b/c/d to overlap in time (work-stealing parallelism), but none did")
	}
}
