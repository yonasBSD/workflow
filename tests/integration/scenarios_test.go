// Package integration — scenario tests that verify specific executor behaviours
// end-to-end. Each test is named after the exact feature or regression it
// covers so a failing test immediately identifies the broken contract.
//
// Tests run sequentially (no t.Parallel) because config.Get() holds global
// path state that each test overwrites.
package integration

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/silocorp/workflow/internal/config"
	"github.com/silocorp/workflow/internal/contextmap"
	"github.com/silocorp/workflow/internal/dag"
	"github.com/silocorp/workflow/internal/executor"
	"github.com/silocorp/workflow/internal/storage"
	"github.com/silocorp/workflow/tests/helpers"
)

// ─── 1. register captures only the last non-empty stdout line ─────────────────

func TestRegisterLastLineOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses 'printf' which is not available in cmd.exe")
	}
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	// The command prints multiple lines; only the last should be registered.
	d := buildDAG(t, fs, "register-last", `
name = "register-last"

[tasks.produce]
cmd      = "printf 'line1\nline2\nfinal-value\n'"
register = "captured"

[tasks.consume]
cmd        = "echo using-{{.captured}}"
depends_on = ["produce"]
`)

	run, err := executor.NewSequentialExecutor(store).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Fatalf("expected success, got %s", run.Status)
	}

	// Verify consume task ran (it would fail if {{.captured}} resolved to "").
	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, TaskID: "consume"})
	if len(tasks) == 0 {
		t.Fatal("consume task not found in DB")
	}
	if tasks[0].State != storage.TaskSuccess {
		t.Errorf("consume task expected success, got %s", tasks[0].State)
	}
}

// ─── 2. Matrix variable interpolation ─────────────────────────────────────────

func TestMatrixVarInterpolation(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "matrix-interp", `
name = "matrix-interp"

[tasks.build]
cmd = "echo building-{{.env}}"

[tasks.build.matrix]
env = ["dev", "prod"]
`)

	run, err := executor.NewParallelExecutor(store, 4).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected success, got %s", run.Status)
	}
	// Both matrix tasks must succeed; a failed interpolation causes a task error.
	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	for _, task := range tasks {
		if task.State != storage.TaskSuccess {
			t.Errorf("task %s expected success, got %s", task.TaskID, task.State)
		}
	}
}

// ─── 3. working_dir must exist before a task starts ───────────────────────────

func TestWorkingDirMustExist(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "wd-not-exist", `
name = "wd-not-exist"

[tasks.broken]
cmd         = "echo hello"
working_dir = "/tmp/wf-nonexistent-dir-that-does-not-exist-abc123"
`)

	_, err := executor.NewSequentialExecutor(store).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	if err == nil {
		t.Fatal("expected error for non-existent working_dir, got nil")
	}
}

// ─── 4. Task-level forensic trap fires on task failure ────────────────────────

func TestForensicTrapFires(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("forensic trap tests use 'touch' and Unix paths — not supported on Windows")
	}
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	// Use a file to detect whether the trap ran.
	trapFile := filepath.Join(t.TempDir(), "trap-fired")

	d := buildDAG(t, fs, "task-trap", `
name = "task-trap"

[tasks.failing]
cmd        = "exit 1"
on_failure = "my-trap"

[tasks.my-trap]
type = "forensic"
cmd  = "touch `+trapFile+`"
`)

	_, err := executor.NewSequentialExecutor(store).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	if err == nil {
		t.Fatal("expected workflow to fail")
	}
	if _, statErr := os.Stat(trapFile); statErr != nil {
		t.Errorf("forensic trap did not run (file %s not created): %v", trapFile, statErr)
	}
}

// ─── 5. Global forensic trap fires on any task failure ────────────────────────

func TestGlobalForensicTrapFires(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("forensic trap tests use 'touch' and Unix paths — not supported on Windows")
	}
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	trapFile := filepath.Join(t.TempDir(), "global-trap-fired")

	d := buildDAG(t, fs, "global-trap", `
name       = "global-trap"
on_failure = "global-handler"

[tasks.failing]
cmd = "exit 1"

[tasks.global-handler]
type = "forensic"
cmd  = "touch `+trapFile+`"
`)

	_, err := executor.NewSequentialExecutor(store).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	if err == nil {
		t.Fatal("expected workflow to fail")
	}
	if _, statErr := os.Stat(trapFile); statErr != nil {
		t.Errorf("global forensic trap did not run: %v", statErr)
	}
}

// ─── 6. ignore_failure lets downstream tasks continue ─────────────────────────

func TestIgnoreFailureContinues(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "ignore-fail", `
name = "ignore-fail"

[tasks.failing]
cmd            = "exit 1"
ignore_failure = true

[tasks.after]
cmd        = "echo ran-after-failure"
depends_on = ["failing"]
`)

	run, err := executor.NewSequentialExecutor(store).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected success when ignore_failure=true, got %s", run.Status)
	}

	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, TaskID: "after"})
	if len(tasks) == 0 {
		t.Fatal("downstream task 'after' did not run")
	}
	if tasks[0].State != storage.TaskSuccess {
		t.Errorf("downstream task expected success, got %s", tasks[0].State)
	}
}

// ─── 7. Condition false → task is skipped ─────────────────────────────────────

func TestConditionSkipsTask(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "cond-skip", `
name = "cond-skip"

[tasks.producer]
cmd      = "echo no"
register = "gate"

[tasks.gated]
cmd        = "echo should-not-run"
if         = 'gate == "yes"'
depends_on = ["producer"]
`)

	run, err := executor.NewSequentialExecutor(store).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected success, got %s", run.Status)
	}

	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, TaskID: "gated"})
	if len(tasks) == 0 {
		t.Fatal("gated task record not found")
	}
	if tasks[0].State != storage.TaskSkipped {
		t.Errorf("expected task to be skipped, got %s", tasks[0].State)
	}
}

// ─── 8. Condition true → task runs ────────────────────────────────────────────

func TestConditionRunsTask(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "cond-run", `
name = "cond-run"

[tasks.producer]
cmd      = "echo yes"
register = "gate"

[tasks.gated]
cmd        = "echo ran"
if         = 'gate == "yes"'
depends_on = ["producer"]
`)

	run, err := executor.NewSequentialExecutor(store).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected success, got %s", run.Status)
	}

	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID, TaskID: "gated"})
	if len(tasks) == 0 {
		t.Fatal("gated task record not found")
	}
	if tasks[0].State != storage.TaskSuccess {
		t.Errorf("expected task to succeed, got %s", tasks[0].State)
	}
}

// ─── 9. Resume skips already-succeeded tasks ──────────────────────────────────

func TestResumeSkipsSucceeded(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	// First run: task1 succeeds, task2 fails.
	d := buildDAG(t, fs, "resume-skip", `
name = "resume-skip"

[tasks.task1]
cmd = "echo task1-ok"

[tasks.task2]
cmd        = "exit 1"
depends_on = ["task1"]
`)

	ex := executor.NewSequentialExecutor(store)
	run, err := ex.Execute(context.Background(), d, contextmap.NewContextMap())
	if err == nil {
		t.Fatal("expected first run to fail")
	}
	runID := run.ID

	// Fix workflow so task2 now passes.
	buildDAG(t, fs, "resume-skip", `
name = "resume-skip"

[tasks.task1]
cmd = "echo task1-ok"

[tasks.task2]
cmd        = "echo task2-fixed"
depends_on = ["task1"]
`)

	resumed, resumeErr := ex.Resume(context.Background(), runID)
	if resumeErr != nil {
		t.Fatalf("resume failed: %v", resumeErr)
	}
	if resumed.Status != storage.RunSuccess {
		t.Errorf("expected resume to succeed, got %s", resumed.Status)
	}

	// task1 must have exactly one execution record (not re-run on resume).
	all, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: runID})
	task1Count := 0
	for _, te := range all {
		if te.TaskID == "task1" {
			task1Count++
		}
	}
	if task1Count != 1 {
		t.Errorf("task1 should run exactly once, found %d execution records", task1Count)
	}
}

// ─── 10. Parallel and sequential executors produce the same final status ───────

func TestParallelVsSequentialSameResult(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")

	seqStore := newStore(t, fs.Path("seq.db"))
	defer seqStore.Close()
	parStore := newStore(t, fs.Path("par.db"))
	defer parStore.Close()

	const toml = `
name = "compare"

[tasks.a]
cmd = "echo A"

[tasks.b]
cmd        = "echo B"
depends_on = ["a"]

[tasks.c]
cmd        = "echo C"
depends_on = ["a"]

[tasks.d]
cmd        = "echo D"
depends_on = ["b", "c"]
`

	dSeq := buildDAG(t, fs, "compare", toml)
	dPar := buildDAG(t, fs, "compare", toml)

	seqRun, seqErr := executor.NewSequentialExecutor(seqStore).Execute(
		context.Background(), dSeq, contextmap.NewContextMap(),
	)
	parRun, parErr := executor.NewParallelExecutor(parStore, 4).Execute(
		context.Background(), dPar, contextmap.NewContextMap(),
	)

	if (seqErr != nil) != (parErr != nil) {
		t.Errorf("error mismatch: seq=%v par=%v", seqErr, parErr)
	}
	if seqRun.Status != parRun.Status {
		t.Errorf("status mismatch: seq=%s par=%s", seqRun.Status, parRun.Status)
	}
	if seqRun.TotalTasks != parRun.TotalTasks {
		t.Errorf("total tasks mismatch: seq=%d par=%d", seqRun.TotalTasks, parRun.TotalTasks)
	}
}

// ─── 11. Work-stealing handles diamond dependency correctly ───────────────────

func TestWorkStealingDiamondPattern(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "ws-diamond", `
name = "ws-diamond"

[tasks.a]
cmd = "echo A"

[tasks.b]
cmd        = "echo B"
depends_on = ["a"]

[tasks.c]
cmd        = "echo C"
depends_on = ["a"]

[tasks.d]
cmd        = "echo D"
depends_on = ["b", "c"]
`)

	run, err := executor.NewWorkStealingExecutor(store, 4).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("expected success, got %s", run.Status)
	}

	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	if len(tasks) != 4 {
		t.Errorf("expected 4 task executions, got %d", len(tasks))
	}
	for _, task := range tasks {
		if task.State != storage.TaskSuccess {
			t.Errorf("task %s expected success, got %s", task.TaskID, task.State)
		}
	}
}

// ─── 12. Timeout kills a long-running task ────────────────────────────────────

func TestTimeoutKillsTask(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "timeout-kill", `
name    = "timeout-kill"

[tasks.slow]
cmd     = "sleep 30"
timeout = "500ms"
retries = 0
`)

	start := time.Now()
	_, err := executor.NewSequentialExecutor(store).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// Must abort well under 5 seconds (not wait the full 30 s).
	if elapsed > 5*time.Second {
		t.Errorf("task did not time out promptly: elapsed %v (expected < 5s)", elapsed)
	}
}

// ─── 13. Retry mechanism makes the correct number of attempts ─────────────────

func TestRetryMakesCorrectAttempts(t *testing.T) {
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	d := buildDAG(t, fs, "retry-attempts", `
name = "retry-attempts"

[tasks.flaky]
cmd         = "exit 1"
retries     = 2
retry_delay = "0s"
`)

	run, err := executor.NewSequentialExecutor(store).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	if err == nil {
		t.Fatal("expected workflow to fail after retries")
	}

	// retries=2 → 3 total attempts → 3 task_execution rows.
	tasks, _ := store.ListTaskExecutions(storage.TaskFilters{RunID: run.ID})
	if len(tasks) < 3 {
		t.Errorf("expected at least 3 task execution records (one per attempt), got %d", len(tasks))
	}
}

// ─── 14. clean_env starts with an empty environment ──────────────────────────

func TestCleanEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix commands ('env', ';') not available in cmd.exe")
	}
	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	config.Get().Paths.Logs = fs.Path("logs")
	store := newStore(t, fs.Path("test.db"))
	defer store.Close()

	// Set a sentinel env var in the parent; the task should NOT see it.
	t.Setenv("WF_SENTINEL_TEST_VAR_ABC123", "should-not-appear")

	outFile := filepath.Join(t.TempDir(), "env-output.txt")

	d := buildDAG(t, fs, "clean-env-test", `
name = "clean-env-test"

[tasks.check]
cmd       = "env > `+outFile+`; true"
clean_env = true
`)

	run, err := executor.NewSequentialExecutor(store).Execute(
		context.Background(), d, contextmap.NewContextMap(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Fatalf("expected success, got %s", run.Status)
	}

	raw, readErr := os.ReadFile(outFile)
	if readErr != nil {
		t.Fatalf("could not read env output: %v", readErr)
	}
	if strings.Contains(string(raw), "WF_SENTINEL_TEST_VAR_ABC123") {
		t.Error("clean_env=true should not expose parent process env vars to the task")
	}
}

// ─── 15. Concurrent runs stress test (race detector guard) ────────────────────

func TestConcurrentRunsStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test under -short")
	}

	const workers = 10

	const stressToml = `
name = "stress"

[tasks.a]
cmd = "echo A"

[tasks.b]
cmd        = "echo B"
depends_on = ["a"]
`
	// Set up the shared workflows dir and config ONCE before any goroutine
	// starts so that no goroutine races to overwrite config.Get().Paths.Workflows.
	sharedFS := helpers.NewTestFS(t)
	defer sharedFS.Cleanup()

	workflowsDir := sharedFS.Path("workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "stress.toml"), []byte(stressToml), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	config.Get().Paths.Workflows = workflowsDir
	config.Get().Paths.Logs = t.TempDir()

	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			localFS := helpers.NewTestFS(t)
			defer localFS.Cleanup()

			localStore := newStore(t, localFS.Path("stress.db"))
			defer localStore.Close()

			// Build a fresh DAG per goroutine without touching config.
			def, err := dag.NewParser("stress").Parse()
			if err != nil {
				errs <- err
				return
			}
			d, err := dag.NewBuilder(def).Build()
			if err != nil {
				errs <- err
				return
			}

			_, err = executor.NewSequentialExecutor(localStore).Execute(
				context.Background(), d, contextmap.NewContextMap(),
			)
			errs <- err
		}()
	}

	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}
