package executor

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/joelfokou/workflow/internal/config"
	"github.com/joelfokou/workflow/internal/contextmap"
	"github.com/joelfokou/workflow/internal/dag"
	"github.com/joelfokou/workflow/internal/logger"
	"github.com/joelfokou/workflow/internal/storage"
)

func init() {
	if err := logger.Init(logger.Config{Level: "error", Format: "console"}); err != nil {
		panic(err)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// tempStore opens a real SQLite database in t.TempDir() and registers cleanup.
func tempStore(t *testing.T) *storage.Store {
	t.Helper()
	dir := t.TempDir()

	// Configure log path so the executor can write task logs.
	config.Get().Paths.Logs = dir

	store, err := storage.New(dir + "/wf.db")
	if err != nil {
		t.Fatalf("tempStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// simpleDAG builds an in-memory DAG from a WorkflowDefinition without a TOML
// file, bypassing the parser.  Forensic tasks are excluded from the normal
// expansion path so they don't appear in Levels.
func simpleDAG(t *testing.T, name string, tasks map[string]*dag.TaskDefinition) *dag.DAG {
	t.Helper()
	def := &dag.WorkflowDefinition{Name: name, Tasks: tasks}
	d, err := dag.NewBuilder(def).Build()
	if err != nil {
		t.Fatalf("simpleDAG %q: %v", name, err)
	}
	return d
}

func ctxMap() *contextmap.ContextMap { return contextmap.NewContextMap() }

// ─── filterNonForensicDeps (pure unit) ───────────────────────────────────────

func TestFilterNonForensicDeps_AllNormal(t *testing.T) {
	nodes := []*dag.Node{
		{ID: "a", TaskDef: &dag.TaskDefinition{Cmd: "x", Type: dag.TaskTypeNormal}},
		{ID: "b", TaskDef: &dag.TaskDefinition{Cmd: "x", Type: dag.TaskTypeNormal}},
	}
	got := filterNonForensicDeps(nodes)
	if len(got) != 2 {
		t.Errorf("want 2, got %d", len(got))
	}
}

func TestFilterNonForensicDeps_MixedFiltersForensic(t *testing.T) {
	nodes := []*dag.Node{
		{ID: "normal", TaskDef: &dag.TaskDefinition{Cmd: "x", Type: dag.TaskTypeNormal}},
		{ID: "trap", TaskDef: &dag.TaskDefinition{Cmd: "x", Type: dag.TaskTypeForensic}},
	}
	got := filterNonForensicDeps(nodes)
	if len(got) != 1 || got[0].ID != "normal" {
		t.Errorf("want [normal], got %v", got)
	}
}

func TestFilterNonForensicDeps_AllForensic(t *testing.T) {
	nodes := []*dag.Node{
		{ID: "trap", TaskDef: &dag.TaskDefinition{Cmd: "x", Type: dag.TaskTypeForensic}},
	}
	got := filterNonForensicDeps(nodes)
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestFilterNonForensicDeps_Empty(t *testing.T) {
	got := filterNonForensicDeps(nil)
	if len(got) != 0 {
		t.Error("want empty slice for nil input")
	}
}

// ─── Sequential executor ──────────────────────────────────────────────────────

func TestSequentialHappyPath(t *testing.T) {
	store := tempStore(t)
	d := simpleDAG(t, "happy", map[string]*dag.TaskDefinition{
		"a": {Cmd: "echo hello", Name: "a"},
		"b": {Cmd: "echo world", Name: "b", DependsOn: []string{"a"}},
	})

	exec := NewSequentialExecutor(store)
	run, err := exec.Execute(context.Background(), d, ctxMap())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("want RunSuccess, got %v", run.Status)
	}
}

func TestSequentialTaskFailure(t *testing.T) {
	store := tempStore(t)
	d := simpleDAG(t, "fail", map[string]*dag.TaskDefinition{
		"bad": {Cmd: "exit 1", Name: "bad"},
	})

	exec := NewSequentialExecutor(store)
	run, err := exec.Execute(context.Background(), d, ctxMap())
	if err == nil {
		t.Fatal("expected error on task failure")
	}
	if run.Status != storage.RunFailed {
		t.Errorf("want RunFailed, got %v", run.Status)
	}
}

func TestSequentialContextCancellation(t *testing.T) {
	store := tempStore(t)

	// A long-running task that will be cancelled.
	d := simpleDAG(t, "cancel", map[string]*dag.TaskDefinition{
		"slow": {Cmd: "sleep 30", Name: "slow"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	exec := NewSequentialExecutor(store)
	run, err := exec.Execute(ctx, d, ctxMap())
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if run.Status != storage.RunCancelled {
		t.Errorf("want RunCancelled, got %v", run.Status)
	}
}

func TestSequentialIgnoreFailure(t *testing.T) {
	store := tempStore(t)
	d := simpleDAG(t, "ignore", map[string]*dag.TaskDefinition{
		"bad":   {Cmd: "exit 1", Name: "bad", IgnoreFailure: true},
		"after": {Cmd: "echo ok", Name: "after", DependsOn: []string{"bad"}},
	})

	exec := NewSequentialExecutor(store)
	run, err := exec.Execute(context.Background(), d, ctxMap())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("want RunSuccess after ignore_failure, got %v", run.Status)
	}
}

func TestSequentialTaskTimeout(t *testing.T) {
	store := tempStore(t)
	d := simpleDAG(t, "timeout", map[string]*dag.TaskDefinition{
		"slow": {Cmd: "sleep 10", Name: "slow", Timeout: dag.Duration(200 * time.Millisecond)},
	})

	exec := NewSequentialExecutor(store)
	run, err := exec.Execute(context.Background(), d, ctxMap())
	if err == nil {
		t.Fatal("expected error for timed-out task")
	}
	if run.Status != storage.RunFailed {
		t.Errorf("want RunFailed on task timeout, got %v", run.Status)
	}
}

// ─── Parallel executor ────────────────────────────────────────────────────────

func TestParallelHappyPath(t *testing.T) {
	store := tempStore(t)
	d := simpleDAG(t, "par-happy", map[string]*dag.TaskDefinition{
		"p1": {Cmd: "echo p1", Name: "p1"},
		"p2": {Cmd: "echo p2", Name: "p2"},
		"p3": {Cmd: "echo p3", Name: "p3"},
	})

	exec := NewParallelExecutor(store, 3)
	run, err := exec.Execute(context.Background(), d, ctxMap())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("want RunSuccess, got %v", run.Status)
	}
}

func TestParallelTaskFailure(t *testing.T) {
	store := tempStore(t)
	d := simpleDAG(t, "par-fail", map[string]*dag.TaskDefinition{
		"ok":  {Cmd: "echo ok", Name: "ok"},
		"bad": {Cmd: "exit 1", Name: "bad"},
	})

	exec := NewParallelExecutor(store, 2)
	run, err := exec.Execute(context.Background(), d, ctxMap())
	if err == nil {
		t.Fatal("expected error on task failure")
	}
	if run.Status != storage.RunFailed {
		t.Errorf("want RunFailed, got %v", run.Status)
	}
}

func TestParallelContextCancellation(t *testing.T) {
	store := tempStore(t)
	d := simpleDAG(t, "par-cancel", map[string]*dag.TaskDefinition{
		"slow1": {Cmd: "sleep 30", Name: "slow1"},
		"slow2": {Cmd: "sleep 30", Name: "slow2"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	exec := NewParallelExecutor(store, 2)
	run, err := exec.Execute(ctx, d, ctxMap())
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if run.Status != storage.RunCancelled {
		t.Errorf("want RunCancelled, got %v", run.Status)
	}
}

// ─── Work-stealing executor ───────────────────────────────────────────────────

func TestWSHappyPath(t *testing.T) {
	store := tempStore(t)
	d := simpleDAG(t, "ws-happy", map[string]*dag.TaskDefinition{
		"a": {Cmd: "echo a", Name: "a"},
		"b": {Cmd: "echo b", Name: "b", DependsOn: []string{"a"}},
	})

	exec := NewWorkStealingExecutor(store, 2)
	run, err := exec.Execute(context.Background(), d, ctxMap())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("want RunSuccess, got %v", run.Status)
	}
}

func TestWSDiamondRunsToSuccess(t *testing.T) {
	store := tempStore(t)
	d := simpleDAG(t, "ws-diamond", map[string]*dag.TaskDefinition{
		"a": {Cmd: "echo a", Name: "a"},
		"b": {Cmd: "echo b", Name: "b", DependsOn: []string{"a"}},
		"c": {Cmd: "echo c", Name: "c", DependsOn: []string{"a"}},
		"d": {Cmd: "echo d", Name: "d", DependsOn: []string{"b", "c"}},
	})

	exec := NewWorkStealingExecutor(store, 4)
	run, err := exec.Execute(context.Background(), d, ctxMap())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("want RunSuccess, got %v", run.Status)
	}
}

func TestWSContextCancellation(t *testing.T) {
	store := tempStore(t)
	d := simpleDAG(t, "ws-cancel", map[string]*dag.TaskDefinition{
		"slow": {Cmd: "sleep 30", Name: "slow"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	exec := NewWorkStealingExecutor(store, 2)
	run, err := exec.Execute(ctx, d, ctxMap())
	if err == nil {
		t.Fatal("expected error on cancellation")
	}
	if run.Status != storage.RunCancelled {
		t.Errorf("want RunCancelled, got %v", run.Status)
	}
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

func BenchmarkSequentialSimple(b *testing.B) {
	dir := b.TempDir()
	config.Get().Paths.Logs = dir
	store, _ := storage.New(dir + "/wf.db")
	defer store.Close()

	d := benchDAG(b, "bench-seq", 1)
	exec := NewSequentialExecutor(store)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		exec.Execute(context.Background(), d, contextmap.NewContextMap()) //nolint:errcheck
	}
}

func BenchmarkParallelWide4(b *testing.B) {
	dir := b.TempDir()
	config.Get().Paths.Logs = dir
	store, _ := storage.New(dir + "/wf.db")
	defer store.Close()

	d := benchDAG(b, "bench-par", 4)
	exec := NewParallelExecutor(store, 4)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		exec.Execute(context.Background(), d, contextmap.NewContextMap()) //nolint:errcheck
	}
}

func BenchmarkWSWide4(b *testing.B) {
	dir := b.TempDir()
	config.Get().Paths.Logs = dir
	store, _ := storage.New(dir + "/wf.db")
	defer store.Close()

	d := benchDAG(b, "bench-ws", 4)
	exec := NewWorkStealingExecutor(store, 4)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		exec.Execute(context.Background(), d, contextmap.NewContextMap()) //nolint:errcheck
	}
}

// benchDAG returns a DAG with n independent tasks (all echo commands).
func benchDAG(b interface {
	TempDir() string
	Helper()
}, name string, n int) *dag.DAG {
	tasks := make(map[string]*dag.TaskDefinition, n)
	for i := 0; i < n; i++ {
		id := string(rune('a' + i))
		tasks[id] = &dag.TaskDefinition{Cmd: "echo x", Name: id}
	}
	def := &dag.WorkflowDefinition{Name: name, Tasks: tasks}

	// Use dummy log dir so executor.go can create log files.
	os.MkdirAll(config.Get().Paths.Logs, 0755) //nolint:errcheck

	d, err := dag.NewBuilder(def).Build()
	if err != nil {
		panic(err)
	}
	return d
}
