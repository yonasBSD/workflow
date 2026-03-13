// Package e2e contains end-to-end tests for the complete workflow application.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joelfokou/workflow/internal/logger"
	"github.com/joelfokou/workflow/internal/storage"
	"github.com/joelfokou/workflow/tests/helpers"
)

// wfBinary is the platform-specific name of the compiled wf executable.
var wfBinary = func() string {
	if runtime.GOOS == "windows" {
		return "wf.exe"
	}
	return "wf"
}()

func init() {
	if err := logger.Init(logger.Config{
		Level:  "info",
		Format: "console",
	}); err != nil {
		panic(err)
	}
}

// TestE2ECompleteWorkflow tests the entire CLI workflow from start to finish.
func TestE2ECompleteWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	// Setup project structure
	if err := setupProject(fs); err != nil {
		t.Fatalf("failed to setup project: %v", err)
	}

	// Test init command
	t.Run("init", func(t *testing.T) {
		testInit(t, fs)
	})

	// Test validate command
	t.Run("validate", func(t *testing.T) {
		testValidate(t, fs)
	})

	// Test list command
	t.Run("list", func(t *testing.T) {
		testList(t, fs)
	})

	// Test graph command
	t.Run("graph", func(t *testing.T) {
		testGraph(t, fs)
	})

	// Test run command
	t.Run("run", func(t *testing.T) {
		testRun(t, fs)
	})

	// Test logs command
	t.Run("logs", func(t *testing.T) {
		testLogs(t, fs)
	})

	// Test runs command
	t.Run("runs", func(t *testing.T) {
		testRuns(t, fs)
	})

	// Test resume command
	t.Run("resume", func(t *testing.T) {
		testResume(t, fs)
	})

	// Test audit command
	t.Run("audit", func(t *testing.T) {
		testAudit(t, fs)
	})

	// Test inspect command
	t.Run("inspect", func(t *testing.T) {
		testInspect(t, fs)
	})

	// Test health command
	t.Run("health", func(t *testing.T) {
		testHealth(t, fs)
	})

	// Test runs advanced flags
	t.Run("runs_advanced", func(t *testing.T) {
		testRunsAdvanced(t, fs)
	})
}

// TestE2EErrorHandling tests error scenarios across the CLI.
func TestE2EErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	t.Run("invalid_workflow", func(t *testing.T) {
		testInvalidWorkflow(t, fs)
	})

	t.Run("missing_workflow", func(t *testing.T) {
		testMissingWorkflow(t, fs)
	})

	t.Run("cycle_detection", func(t *testing.T) {
		testCycleDetection(t, fs)
	})

	t.Run("missing_dependency", func(t *testing.T) {
		testMissingDependency(t, fs)
	})
}

// TestE2EConfigManagement tests configuration loading and overrides.
func TestE2EConfigManagement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	fs := helpers.NewTestFS(t)
	defer fs.Cleanup()

	t.Run("env_var_override", func(t *testing.T) {
		testEnvVarOverride(t, fs)
	})

	t.Run("config_file_override", func(t *testing.T) {
		testConfigFileOverride(t, fs)
	})
}

// setupProject creates the necessary project structure for E2E testing.
func setupProject(fs *helpers.TestFS) error {
	// Build the wf binary
	output, err := exec.Command("go", "build", "-o", filepath.Join(fs.Path("."), wfBinary), "../..").CombinedOutput() //nolint:gosec
	if err != nil {
		panic(fmt.Sprintf("failed to build wf binary: %v\noutput: %s", err, string(output)))
	}

	// Create directories
	dirs := []string{"workflows", "logs"}
	for _, dir := range dirs {
		if err := os.MkdirAll(fs.Path(dir), 0755); err != nil {
			return err
		}
	}

	// Create database file
	dbPath := filepath.Join(fs.Path("test.db"))
	if _, err := os.Create(dbPath); err != nil {
		return err
	}

	// Create example workflows
	workflows := map[string]string{
		"simple.toml": helpers.SimpleWorkflow(),
		"multi.toml":  helpers.MultiTaskWorkflow(),
		"resume.toml": helpers.ResumeWorkflow(),
	}

	for name, content := range workflows {
		path := filepath.Join(fs.Path("workflows"), name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return err
		}
	}

	return nil
}

// testInit tests the init command.
func testInit(t *testing.T, fs *helpers.TestFS) {
	cmd := newCmd(fs, "init")
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("init command failed: %v\noutput: %s", err, string(output))
	}

	// Verify directories exist
	for _, dir := range []string{"workflows", "logs"} {
		path := fs.Path(dir)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected directory %s to exist", path)
		}
	}

	// Verify database was created
	dbPath := filepath.Join(fs.Path("test.db"))
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected database file at %s", dbPath)
	}

	if !strings.Contains(string(output), "initialised") {
		t.Error("expected success message in output, got :", string(output))
	}
}

// testValidate tests the validate command.
func testValidate(t *testing.T, fs *helpers.TestFS) {
	// Test validating all workflows
	cmd := newCmd(fs, "validate")
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("validate command failed: %v\noutput: %s", err, string(output))
	}

	// Should succeed for valid workflows
	validCount := strings.Count(string(output), "✓")
	if validCount == 0 {
		t.Error("expected at least one valid workflow")
	}

	// Should show invalid workflows
	if strings.Contains(string(output), "invalid") {
		invalidCount := strings.Count(string(output), "✗")
		if invalidCount == 0 {
			t.Error("expected invalid workflow to be reported")
		}
	}

	// Test validating specific workflow
	cmd = newCmd(fs, "validate", "simple")
	output, err = cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("validate simple command failed: %v", err)
	}

	if !strings.Contains(string(output), "simple") {
		t.Error("expected workflow name in output")
	}
}

// testList tests the list command.
func testList(t *testing.T, fs *helpers.TestFS) {
	cmd := newCmd(fs, "list")
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("list command failed: %v", err)
	}

	// Verify output contains workflow names
	if !strings.Contains(string(output), "simple") {
		t.Error("expected 'simple' workflow in list")
	}

	if !strings.Contains(string(output), "multi") {
		t.Error("expected 'multi' workflow in list")
	}

	// Test with JSON output
	cmd = newCmd(fs, "list", "--json")
	output, err = cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("list --json command failed: %v", err)
	}

	if !strings.Contains(string(output), "\"name\"") {
		t.Error("expected JSON output format")
	}
}

// testGraph tests the graph command.
func testGraph(t *testing.T, fs *helpers.TestFS) {
	// Test ASCII format (default)
	cmd := newCmd(fs, "graph", "simple")
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("graph command failed: %v", err)
	}

	if len(string(output)) == 0 {
		t.Error("expected graph output")
	}

	// Test DOT format
	cmd = newCmd(fs, "graph", "simple", "--format", "dot")
	output, err = cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("graph --format dot command failed: %v", err)
	}

	if !strings.Contains(string(output), "digraph") {
		t.Error("expected DOT format output")
	}

	// Test JSON format
	cmd = newCmd(fs, "graph", "simple", "--format", "json")
	output, err = cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("graph --format json command failed: %v", err)
	}

	if !strings.Contains(string(output), "\"name\"") {
		t.Error("expected JSON format output")
	}
}

// testRun tests the run command.
func testRun(t *testing.T, fs *helpers.TestFS) {
	cmd := newCmd(fs, "run", "simple")
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("run command failed: %v\noutput: %s", err, string(output))
	}

	// Verify database has run recorded
	dbPath := filepath.Join(fs.Path("test.db"))
	store, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	runs, err := store.ListRuns(storage.RunFilters{WorkflowName: "simple", Limit: 10})
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}

	if len(runs) == 0 {
		t.Fatal("expected run to be recorded in database")
	}

	if runs[0].Status != storage.RunSuccess {
		t.Errorf("expected status success, got %s", runs[0].Status)
	}

	// Test dry-run mode
	cmd = newCmd(fs, "run", "simple", "--dry-run")
	output, err = cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("run --dry-run command failed: %v", err)
	}

	if !strings.Contains(string(output), "EXECUTION PLAN") {
		t.Error("expected dry run output")
	}

	// Test JSON output
	cmd = newCmd(fs, "run", "simple", "--dry-run", "--json")
	output, err = cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("run --dry-run --json command failed: %v", err)
	}

	if !strings.Contains(string(output), "\"workflow_name\"") {
		t.Error("expected JSON output format")
	}
}

// testLogs tests the logs command.
func testLogs(t *testing.T, fs *helpers.TestFS) {
	// First run a workflow
	cmd := newCmd(fs, "run", "simple")
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run command failed: %v", err)
	}

	// Get the run ID
	dbPath := filepath.Join(fs.Path("test.db"))
	store, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	runs, err := store.ListRuns(storage.RunFilters{WorkflowName: "simple", Limit: 10})
	store.Close()

	if err != nil || len(runs) == 0 {
		t.Fatal("no runs found to display logs for")
	}

	runID := runs[0].ID

	// Test logs command with run ID
	cmd = newCmd(fs, "logs", runID)
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("logs command failed: %v", err)
	}

	if len(string(output)) == 0 {
		t.Error("expected log output")
	}
}

// testRuns tests the runs command.
func testRuns(t *testing.T, fs *helpers.TestFS) {
	// First run a workflow
	cmd := newCmd(fs, "run", "simple")
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run command failed: %v", err)
	}

	// Test runs command
	cmd = newCmd(fs, "runs")
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("runs command failed: %v", err)
	}

	if !strings.Contains(string(output), "simple") {
		t.Error("expected workflow name in runs output")
	}

	// Test with JSON output
	cmd = newCmd(fs, "runs", "--json")
	output, err = cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("runs --json command failed: %v", err)
	}

	if !strings.Contains(string(output), "\"workflow\"") {
		t.Error("expected JSON output format")
	}

	// Test with filters
	cmd = newCmd(fs, "runs", "--workflow", "simple")
	output, err = cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("runs --workflow command failed: %v", err)
	}

	if !strings.Contains(string(output), "simple") {
		t.Error("expected filtered results")
	}
}

// testResume tests the resume command.
func testResume(t *testing.T, fs *helpers.TestFS) {
	// Run a workflow that will fail
	cmd := newCmd(fs, "run", "resume")
	var output []byte
	var err error
	if _, err = cmd.CombinedOutput(); err == nil {
		t.Fatal("expected run to fail for 'resume' workflow")
	}

	// Update the workflow to fix the failure
	resumeWorkflowFixed := helpers.ResumeWorkflowFixed()
	path := filepath.Join(fs.Path("workflows"), "resume.toml")
	if err := os.WriteFile(path, []byte(resumeWorkflowFixed), 0644); err != nil {
		t.Fatalf("failed to update resume workflow: %v", err)
	}

	// Retrieve the run ID of the failed run
	dbPath := filepath.Join(fs.Path("test.db"))
	store, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	runs, err := store.ListRuns(storage.RunFilters{WorkflowName: "resume", Limit: 10})
	if err != nil || len(runs) == 0 {
		t.Fatal("no runs found to resume")
	}

	failedRunID := runs[0].ID

	// Resume the workflow
	cmd = newCmd(fs, "resume", failedRunID)
	output, err = cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("resume command failed: %v\noutput: %s", err, string(output))
	}

	if !strings.Contains(string(output), "EXECUTION RESULT") {
		t.Errorf("expected execution result output, got: %s", string(output))
	}

	runs, err = store.ListRuns(storage.RunFilters{WorkflowName: "resume", Limit: 10})
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}

	if len(runs) == 0 {
		t.Fatal("expected run to be recorded in database")
	}

	if runs[0].Status != storage.RunSuccess {
		t.Errorf("expected status success after resume, got %s", runs[0].Status)
	}
}

// testInvalidWorkflow tests behavior with invalid workflow.
func testInvalidWorkflow(t *testing.T, fs *helpers.TestFS) {
	if err := setupProject(fs); err != nil {
		t.Fatal(err)
	}

	cmd := newCmd(fs, "validate", "invalid")
	_, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatal("expected validate to fail for invalid workflow")
	}
}

// testMissingWorkflow tests behavior with missing workflow.
func testMissingWorkflow(t *testing.T, fs *helpers.TestFS) {
	if err := setupProject(fs); err != nil {
		t.Fatal(err)
	}

	cmd := newCmd(fs, "run", "nonexistent")
	_, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatal("expected run to fail for nonexistent workflow")
	}
}

// testCycleDetection tests cycle detection in workflows.
func testCycleDetection(t *testing.T, fs *helpers.TestFS) {
	if err := setupProject(fs); err != nil {
		t.Fatal(err)
	}

	cycleWorkflow := helpers.CycleWorkflow()

	path := filepath.Join(fs.Path("workflows"), "cycle.toml")
	if err := os.WriteFile(path, []byte(cycleWorkflow), 0644); err != nil {
		t.Fatalf("failed to create cycle workflow: %v", err)
	}

	cmd := newCmd(fs, "validate", "cycle")
	_, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatal("expected validation to fail for cyclic workflow")
	}
}

// testMissingDependency tests detection of missing dependencies.
func testMissingDependency(t *testing.T, fs *helpers.TestFS) {
	if err := setupProject(fs); err != nil {
		t.Fatal(err)
	}

	cmd := newCmd(fs, "validate", "invalid")
	_, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatal("expected validation to fail for missing dependency")
	}
}

// testEnvVarOverride tests environment variable configuration override.
func testEnvVarOverride(t *testing.T, fs *helpers.TestFS) {
	if err := setupProject(fs); err != nil {
		t.Fatal(err)
	}

	cmd := newCmd(fs, "init")
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("WF_PATHS_WORKFLOWS=%s", fs.Path("workflows")),
		fmt.Sprintf("WF_PATHS_LOGS=%s", fs.Path("logs")),
		fmt.Sprintf("WF_PATHS_DATABASE=%s", fs.Path("test.db")),
	)

	_, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("init with env vars failed: %v", err)
	}
}

// testConfigFileOverride tests config file configuration override.
func testConfigFileOverride(t *testing.T, fs *helpers.TestFS) {
	if err := setupProject(fs); err != nil {
		t.Fatal(err)
	}

	configContent := fmt.Sprintf(`
paths:
  workflows: %s
  logs: %s
  database: %s
`, fs.Path("workflows"), fs.Path("logs"), fs.Path("test.db"))

	configPath := filepath.Join(fs.Path("."), "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	cmd := newCmd(fs, "--config", configPath, "init")
	_, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("init with config file failed: %v", err)
	}
}

// newCmd creates a new command with proper environment.
func newCmd(fs *helpers.TestFS, args ...string) *exec.Cmd {
	binary := "./" + wfBinary

	cmd := exec.Command(binary, args...)
	cmd.Dir = fs.Path(".")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("WF_PATHS_WORKFLOWS=%s", fs.Path("workflows")),
		fmt.Sprintf("WF_PATHS_LOGS=%s", fs.Path("logs")),
		fmt.Sprintf("WF_PATHS_DATABASE=%s", fs.Path("test.db")),
	)
	return cmd
}

// testAudit tests the audit command against a run that has already been executed.
func testAudit(t *testing.T, fs *helpers.TestFS) {
	// Ensure a run exists (may already exist from testRun).
	cmd := newCmd(fs, "run", "simple")
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("prerequisite run failed: %v", err)
	}

	// Fetch latest run ID from the database.
	dbPath := fs.Path("test.db")
	store, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	runs, err := store.ListRuns(storage.RunFilters{WorkflowName: "simple", Limit: 1})
	store.Close()
	if err != nil || len(runs) == 0 {
		t.Fatal("no runs found for audit test")
	}
	runID := runs[0].ID

	// Text output — tabwriter table with TIME / EVENT / DATA columns.
	out, err := newCmd(fs, "audit", runID).CombinedOutput()
	if err != nil {
		t.Fatalf("audit command failed: %v\noutput: %s", err, out)
	}
	outStr := string(out)
	if !strings.Contains(outStr, "run_started") {
		t.Errorf("expected run_started event in audit output, got: %s", outStr)
	}
	if !strings.Contains(outStr, "run_completed") {
		t.Errorf("expected run_completed event in audit output, got: %s", outStr)
	}
	if !strings.Contains(outStr, "EVENT") {
		t.Errorf("expected EVENT column header in audit output, got: %s", outStr)
	}

	// JSON output — raw Go struct array; field is "EventType".
	out, err = newCmd(fs, "audit", runID, "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("audit --json command failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "EventType") {
		t.Errorf("expected JSON with EventType field, got: %s", string(out))
	}
	if !strings.Contains(string(out), "run_started") {
		t.Errorf("expected run_started event in JSON audit output, got: %s", string(out))
	}

	// Unknown run ID should fail.
	out, err = newCmd(fs, "audit", "nonexistent-run-id").CombinedOutput()
	if err == nil {
		t.Errorf("expected audit to fail for unknown run ID, got: %s", out)
	}
}

// testInspect tests the inspect command against a completed run.
func testInspect(t *testing.T, fs *helpers.TestFS) {
	// Ensure a run exists.
	cmd := newCmd(fs, "run", "simple")
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("prerequisite run failed: %v", err)
	}

	dbPath := fs.Path("test.db")
	store, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	runs, err := store.ListRuns(storage.RunFilters{WorkflowName: "simple", Limit: 1})
	store.Close()
	if err != nil || len(runs) == 0 {
		t.Fatal("no runs found for inspect test")
	}
	runID := runs[0].ID

	// Text output.
	out, err := newCmd(fs, "inspect", runID).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect command failed: %v\noutput: %s", err, out)
	}
	outStr := string(out)
	if !strings.Contains(outStr, "RUN METADATA") {
		t.Errorf("expected RUN METADATA section, got: %s", outStr)
	}
	if !strings.Contains(outStr, "TASKS") {
		t.Errorf("expected TASKS section, got: %s", outStr)
	}
	if !strings.Contains(outStr, runID) {
		t.Errorf("expected run ID in output, got: %s", outStr)
	}

	// JSON output — top-level object has "run", "tasks", "forensic_logs", "dag_cache", "variables".
	out, err = newCmd(fs, "inspect", runID, "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect --json command failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "\"run\"") {
		t.Errorf("expected JSON with run field, got: %s", string(out))
	}
	if !strings.Contains(string(out), "\"tasks\"") {
		t.Errorf("expected JSON with tasks field, got: %s", string(out))
	}

	// Unknown run ID should fail.
	out, err = newCmd(fs, "inspect", "nonexistent-run-id").CombinedOutput()
	if err == nil {
		t.Errorf("expected inspect to fail for unknown run ID, got: %s", out)
	}
}

// testHealth tests the health command.
func testHealth(t *testing.T, fs *helpers.TestFS) {
	// Text output — database must be reachable.
	out, err := newCmd(fs, "health").CombinedOutput()
	if err != nil {
		t.Fatalf("health command failed: %v\noutput: %s", err, out)
	}
	outStr := string(out)
	if !strings.Contains(outStr, "SYSTEM HEALTH") {
		t.Errorf("expected SYSTEM HEALTH header, got: %s", outStr)
	}
	if !strings.Contains(outStr, "Database") {
		t.Errorf("expected Database line in health output, got: %s", outStr)
	}
	if !strings.Contains(outStr, "Workflows") {
		t.Errorf("expected Workflows line in health output, got: %s", outStr)
	}

	// JSON output.
	out, err = newCmd(fs, "health", "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("health --json command failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "\"database_ok\"") {
		t.Errorf("expected JSON with database_ok field, got: %s", string(out))
	}
	if !strings.Contains(string(out), "\"healthy\"") {
		t.Errorf("expected JSON with healthy field, got: %s", string(out))
	}
}

// testRunsAdvanced tests the advanced flags of the runs command.
func testRunsAdvanced(t *testing.T, fs *helpers.TestFS) {
	// Ensure at least one run exists.
	if _, err := newCmd(fs, "run", "simple").CombinedOutput(); err != nil {
		t.Fatalf("prerequisite run failed: %v", err)
	}

	// --stats: should print summary only (no per-run table rows).
	out, err := newCmd(fs, "runs", "--stats").CombinedOutput()
	if err != nil {
		t.Fatalf("runs --stats failed: %v\noutput: %s", err, out)
	}
	outStr := string(out)
	if !strings.Contains(outStr, "Total") {
		t.Errorf("expected Total in --stats output, got: %s", outStr)
	}

	// --detailed: should include per-task breakdown.
	out, err = newCmd(fs, "runs", "--detailed").CombinedOutput()
	if err != nil {
		t.Fatalf("runs --detailed failed: %v\noutput: %s", err, out)
	}
	if len(string(out)) == 0 {
		t.Error("expected non-empty output for runs --detailed")
	}

	// --sort time: should succeed.
	out, err = newCmd(fs, "runs", "--sort", "time").CombinedOutput()
	if err != nil {
		t.Fatalf("runs --sort time failed: %v\noutput: %s", err, out)
	}

	// --sort workflow: should succeed.
	out, err = newCmd(fs, "runs", "--sort", "workflow").CombinedOutput()
	if err != nil {
		t.Fatalf("runs --sort workflow failed: %v\noutput: %s", err, out)
	}

	// --sort duration: should succeed.
	out, err = newCmd(fs, "runs", "--sort", "duration").CombinedOutput()
	if err != nil {
		t.Fatalf("runs --sort duration failed: %v\noutput: %s", err, out)
	}

	// --timeline: should succeed.
	out, err = newCmd(fs, "runs", "--timeline").CombinedOutput()
	if err != nil {
		t.Fatalf("runs --timeline failed: %v\noutput: %s", err, out)
	}
	if len(string(out)) == 0 {
		t.Error("expected non-empty output for runs --timeline")
	}

	// --workflow filter with --json: should contain matching workflow.
	out, err = newCmd(fs, "runs", "--workflow", "simple", "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("runs --workflow simple --json failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "simple") {
		t.Errorf("expected workflow name 'simple' in JSON output, got: %s", string(out))
	}

	// --status filter: filter by success status. The table prints "SUCCESS" (upper-case, ANSI-coloured).
	out, err = newCmd(fs, "runs", "--status", "success").CombinedOutput()
	if err != nil {
		t.Fatalf("runs --status success failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "SUCCESS") {
		t.Errorf("expected SUCCESS in status-filtered output, got: %s", string(out))
	}
}
