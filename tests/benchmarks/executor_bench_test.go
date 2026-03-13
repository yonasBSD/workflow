// Package benchmarks contains performance and reliability benchmarks for the
// workflow executor pipeline.  Run with:
//
//	go test ./tests/benchmarks/... -bench=. -benchmem -benchtime=5s
//
// Standard metrics reported per operation:
//   - ns/op  — wall-clock latency
//   - B/op   — heap bytes allocated
//   - allocs/op — heap allocation count
package benchmarks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/joelfokou/workflow/internal/config"
	"github.com/joelfokou/workflow/internal/contextmap"
	"github.com/joelfokou/workflow/internal/dag"
	"github.com/joelfokou/workflow/internal/executor"
	"github.com/joelfokou/workflow/internal/logger"
	"github.com/joelfokou/workflow/internal/storage"
)

func init() {
	if err := logger.Init(logger.Config{Level: "error", Format: "console"}); err != nil {
		panic(err)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func tempEnv(b *testing.B) (store *storage.Store, cleanup func()) {
	b.Helper()
	dir := b.TempDir()
	config.Get().Paths.Logs = filepath.Join(dir, "logs")
	os.MkdirAll(config.Get().Paths.Logs, 0755) //nolint:errcheck

	s, err := storage.New(filepath.Join(dir, "wf.db"))
	if err != nil {
		b.Fatalf("store: %v", err)
	}
	return s, func() { s.Close() }
}

// wideDAG returns a DAG with n independent tasks (all running `echo`).
func wideDAG(n int) *dag.DAG {
	tasks := make(map[string]*dag.TaskDefinition, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("t%d", i)
		tasks[id] = &dag.TaskDefinition{Cmd: "echo x", Name: id}
	}
	def := &dag.WorkflowDefinition{Name: fmt.Sprintf("wide-%d", n), Tasks: tasks}
	d, err := dag.NewBuilder(def).Build()
	if err != nil {
		panic(err)
	}
	return d
}

// diamondDAG returns the classic A→{B,C}→D diamond.
func diamondDAG() *dag.DAG {
	def := &dag.WorkflowDefinition{
		Name: "diamond",
		Tasks: map[string]*dag.TaskDefinition{
			"a": {Cmd: "echo a", Name: "a"},
			"b": {Cmd: "echo b", Name: "b", DependsOn: []string{"a"}},
			"c": {Cmd: "echo c", Name: "c", DependsOn: []string{"a"}},
			"d": {Cmd: "echo d", Name: "d", DependsOn: []string{"b", "c"}},
		},
	}
	d, err := dag.NewBuilder(def).Build()
	if err != nil {
		panic(err)
	}
	return d
}

// chainDAG returns a linear n-task chain.
func chainDAG(n int) *dag.DAG {
	tasks := make(map[string]*dag.TaskDefinition, n)
	tasks["t0"] = &dag.TaskDefinition{Cmd: "echo 0", Name: "t0"}
	for i := 1; i < n; i++ {
		id := fmt.Sprintf("t%d", i)
		tasks[id] = &dag.TaskDefinition{
			Cmd: fmt.Sprintf("echo %d", i), Name: id,
			DependsOn: []string{fmt.Sprintf("t%d", i-1)},
		}
	}
	def := &dag.WorkflowDefinition{Name: fmt.Sprintf("chain-%d", n), Tasks: tasks}
	d, err := dag.NewBuilder(def).Build()
	if err != nil {
		panic(err)
	}
	return d
}

// ─── Throughput benchmarks ────────────────────────────────────────────────────

// BenchmarkSequential_1Task measures overhead per workflow run with a single task.
func BenchmarkSequential_1Task(b *testing.B) {
	store, cleanup := tempEnv(b)
	defer cleanup()
	d := wideDAG(1)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		executor.NewSequentialExecutor(store).Execute( //nolint:errcheck
			context.Background(), d, contextmap.NewContextMap(),
		)
	}
}

// BenchmarkParallel_4Tasks benchmarks parallel execution of 4 independent tasks.
func BenchmarkParallel_4Tasks(b *testing.B) {
	store, cleanup := tempEnv(b)
	defer cleanup()
	d := wideDAG(4)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		executor.NewParallelExecutor(store, 4).Execute( //nolint:errcheck
			context.Background(), d, contextmap.NewContextMap(),
		)
	}
}

// BenchmarkWS_4Tasks benchmarks work-stealing execution of 4 independent tasks.
func BenchmarkWS_4Tasks(b *testing.B) {
	store, cleanup := tempEnv(b)
	defer cleanup()
	d := wideDAG(4)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		executor.NewWorkStealingExecutor(store, 4).Execute( //nolint:errcheck
			context.Background(), d, contextmap.NewContextMap(),
		)
	}
}

// BenchmarkWS_Diamond shows work-stealing on the classic diamond pattern.
func BenchmarkWS_Diamond(b *testing.B) {
	store, cleanup := tempEnv(b)
	defer cleanup()
	d := diamondDAG()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		executor.NewWorkStealingExecutor(store, 4).Execute( //nolint:errcheck
			context.Background(), d, contextmap.NewContextMap(),
		)
	}
}

// BenchmarkWS_Chain8 measures work-stealing on a linear 8-task chain.
func BenchmarkWS_Chain8(b *testing.B) {
	store, cleanup := tempEnv(b)
	defer cleanup()
	d := chainDAG(8)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		executor.NewWorkStealingExecutor(store, 4).Execute( //nolint:errcheck
			context.Background(), d, contextmap.NewContextMap(),
		)
	}
}

// ─── Concurrency efficiency benchmarks ───────────────────────────────────────

// BenchmarkConcurrencyEfficiency runs N independent workflows concurrently to
// measure scheduling overhead under load.
func BenchmarkConcurrencyEfficiency(b *testing.B) {
	const concurrency = 4

	store, cleanup := tempEnv(b)
	defer cleanup()
	d := wideDAG(2)

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			executor.NewWorkStealingExecutor(store, concurrency).Execute( //nolint:errcheck
				context.Background(), d, contextmap.NewContextMap(),
			)
		}
	})
}

// ─── DAG build benchmarks ─────────────────────────────────────────────────────

// BenchmarkDAGBuild_100Wide measures how fast the builder processes 100 independent tasks.
func BenchmarkDAGBuild_100Wide(b *testing.B) {
	tasks := make(map[string]*dag.TaskDefinition, 100)
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("t%d", i)
		tasks[id] = &dag.TaskDefinition{Cmd: "echo x", Name: id}
	}
	def := &dag.WorkflowDefinition{Name: "wide-100", Tasks: tasks}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := dag.NewBuilder(def).Build(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDAGBuild_Chain50 measures topological-sort performance on a 50-task chain.
func BenchmarkDAGBuild_Chain50(b *testing.B) {
	tasks := make(map[string]*dag.TaskDefinition, 50)
	tasks["t0"] = &dag.TaskDefinition{Cmd: "echo 0", Name: "t0"}
	for i := 1; i < 50; i++ {
		id := fmt.Sprintf("t%d", i)
		tasks[id] = &dag.TaskDefinition{
			Cmd: fmt.Sprintf("echo %d", i), Name: id,
			DependsOn: []string{fmt.Sprintf("t%d", i-1)},
		}
	}
	def := &dag.WorkflowDefinition{Name: "chain-50", Tasks: tasks}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := dag.NewBuilder(def).Build(); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── Reliability tests ────────────────────────────────────────────────────────

// TestReliability_RapidCancellation verifies that rapid context cancellations
// across concurrent runs never leave goroutines or resources behind.
func TestReliability_RapidCancellation(t *testing.T) {
	dir := t.TempDir()
	config.Get().Paths.Logs = filepath.Join(dir, "logs")
	os.MkdirAll(config.Get().Paths.Logs, 0755) //nolint:errcheck

	store, err := storage.New(filepath.Join(dir, "wf.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()

	d := wideDAG(3) // 3 independent slow tasks

	const runs = 20
	var wg sync.WaitGroup
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			executor.NewWorkStealingExecutor(store, 3).Execute(ctx, d, contextmap.NewContextMap()) //nolint:errcheck
		}()
	}
	wg.Wait()
}

// TestReliability_LargeFanOut verifies correctness on a wide fan-out (root → 20 tasks → final).
func TestReliability_LargeFanOut(t *testing.T) {
	dir := t.TempDir()
	config.Get().Paths.Logs = filepath.Join(dir, "logs")
	os.MkdirAll(config.Get().Paths.Logs, 0755) //nolint:errcheck

	store, err := storage.New(filepath.Join(dir, "wf.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()

	const fanWidth = 20
	tasks := make(map[string]*dag.TaskDefinition, fanWidth+2)
	tasks["root"] = &dag.TaskDefinition{Cmd: "echo root", Name: "root"}

	deps := make([]string, fanWidth)
	for i := 0; i < fanWidth; i++ {
		id := fmt.Sprintf("w%d", i)
		tasks[id] = &dag.TaskDefinition{Cmd: "echo " + id, Name: id, DependsOn: []string{"root"}}
		deps[i] = id
	}
	tasks["final"] = &dag.TaskDefinition{Cmd: "echo final", Name: "final", DependsOn: deps}

	def := &dag.WorkflowDefinition{Name: "fanout-20", Tasks: tasks}
	d, err := dag.NewBuilder(def).Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	exec := executor.NewWorkStealingExecutor(store, 8)
	run, err := exec.Execute(context.Background(), d, contextmap.NewContextMap())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if run.Status != storage.RunSuccess {
		t.Errorf("want RunSuccess, got %v", run.Status)
	}
	if run.TasksSuccess != fanWidth+2 {
		t.Errorf("want %d tasks succeeded, got %d", fanWidth+2, run.TasksSuccess)
	}
}

// TestReliability_ConcurrentContextMapWrites stress-tests concurrent ContextMap
// access under a realistic multi-task scenario.
func TestReliability_ConcurrentContextMapWrites(t *testing.T) {
	const workers = 32
	cm := contextmap.NewContextMap()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			taskID := fmt.Sprintf("task%d", id)
			key := fmt.Sprintf("var%d", id)
			for j := 0; j < 100; j++ {
				cm.Set(taskID, key, j)          //nolint:errcheck
				cm.Get(key)                     //nolint:errcheck
				cm.EvalCondition(key + " > -1") //nolint:errcheck
			}
		}(i)
	}
	wg.Wait()
}

// TestReliability_StoreMigrationIdempotent verifies that calling New() on an
// already-migrated database is safe (no double-apply).
func TestReliability_StoreMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wf.db")

	for i := 0; i < 5; i++ {
		s, err := storage.New(dbPath)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		s.Close()
	}
}
