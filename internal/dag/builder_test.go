package dag

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/joelfokou/workflow/internal/logger"
)

func TestMain(m *testing.M) {
	logger.Init(logger.Config{Level: "error", Format: "console"}) //nolint:errcheck
	os.Exit(m.Run())
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// def builds a WorkflowDefinition with the supplied task map.
func def(name string, tasks map[string]*TaskDefinition) *WorkflowDefinition {
	return &WorkflowDefinition{Name: name, Tasks: tasks}
}

// task creates a simple TaskDefinition with the given command and dependencies.
func task(cmd string, deps ...string) *TaskDefinition {
	return &TaskDefinition{Cmd: cmd, DependsOn: deps}
}

func mustBuild(t *testing.T, d *WorkflowDefinition) *DAG {
	t.Helper()
	g, err := NewBuilder(d).Build()
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	return g
}

// ─── Basic graph construction ─────────────────────────────────────────────────

func TestBuildSimple(t *testing.T) {
	g := mustBuild(t, def("simple", map[string]*TaskDefinition{
		"a": task("echo a"),
	}))

	if len(g.Nodes) != 1 {
		t.Errorf("want 1 node, got %d", len(g.Nodes))
	}
	if g.TotalTasks != 1 {
		t.Errorf("want TotalTasks=1, got %d", g.TotalTasks)
	}
	if len(g.RootNodes) != 1 {
		t.Errorf("want 1 root node, got %d", len(g.RootNodes))
	}
	if len(g.LeafNodes) != 1 {
		t.Errorf("want 1 leaf node, got %d", len(g.LeafNodes))
	}
}

func TestBuildLinearChain(t *testing.T) {
	// a → b → c
	g := mustBuild(t, def("chain", map[string]*TaskDefinition{
		"a": task("echo a"),
		"b": task("echo b", "a"),
		"c": task("echo c", "b"),
	}))

	if len(g.Levels) != 3 {
		t.Fatalf("want 3 levels, got %d", len(g.Levels))
	}
	if g.Levels[0][0].ID != "a" {
		t.Errorf("level 0 should be 'a', got %q", g.Levels[0][0].ID)
	}
	if g.Levels[2][0].ID != "c" {
		t.Errorf("level 2 should be 'c', got %q", g.Levels[2][0].ID)
	}
}

func TestBuildDiamond(t *testing.T) {
	// Classic diamond:  a → b → d
	//                   a → c → d
	g := mustBuild(t, def("diamond", map[string]*TaskDefinition{
		"a": task("echo a"),
		"b": task("echo b", "a"),
		"c": task("echo c", "a"),
		"d": task("echo d", "b", "c"),
	}))

	// Expected levels: [a] [b,c] [d]
	if len(g.Levels) != 3 {
		t.Fatalf("want 3 levels, got %d: %v", len(g.Levels), levelNames(g))
	}
	if len(g.Levels[1]) != 2 {
		t.Errorf("level 1 should have 2 nodes (b,c), got %d", len(g.Levels[1]))
	}

	// Both b and c should be marked parallel-safe.
	for _, n := range g.Levels[1] {
		if !n.CanRunInParallel {
			t.Errorf("node %q in level 1 should have CanRunInParallel=true", n.ID)
		}
	}

	// d should be at level 2.
	d := g.Nodes["d"]
	if d == nil {
		t.Fatal("node 'd' missing from graph")
	}
	if d.Level != 2 {
		t.Errorf("want d.Level=2, got %d", d.Level)
	}
	if len(d.Dependencies) != 2 {
		t.Errorf("want d to have 2 dependencies, got %d", len(d.Dependencies))
	}
}

func TestBuildFanOut(t *testing.T) {
	// root → t1/t2/t3 → final
	g := mustBuild(t, def("fanout", map[string]*TaskDefinition{
		"root":  task("echo root"),
		"t1":    task("echo t1", "root"),
		"t2":    task("echo t2", "root"),
		"t3":    task("echo t3", "root"),
		"final": task("echo final", "t1", "t2", "t3"),
	}))

	if len(g.RootNodes) != 1 {
		t.Errorf("want 1 root, got %d", len(g.RootNodes))
	}
	if len(g.LeafNodes) != 1 {
		t.Errorf("want 1 leaf (final), got %d", len(g.LeafNodes))
	}
	if len(g.Levels) != 3 {
		t.Errorf("want 3 levels, got %d", len(g.Levels))
	}
	if len(g.Levels[1]) != 3 {
		t.Errorf("want 3 nodes in level 1, got %d", len(g.Levels[1]))
	}
}

func TestBuildWideParallel(t *testing.T) {
	// 4 independent root tasks, all at level 0.
	g := mustBuild(t, def("wide", map[string]*TaskDefinition{
		"p1": task("echo p1"),
		"p2": task("echo p2"),
		"p3": task("echo p3"),
		"p4": task("echo p4"),
	}))

	if len(g.RootNodes) != 4 {
		t.Errorf("want 4 root nodes, got %d", len(g.RootNodes))
	}
	if len(g.Levels) != 1 {
		t.Errorf("want 1 level, got %d", len(g.Levels))
	}
}

// ─── Error cases ──────────────────────────────────────────────────────────────

func TestBuildMissingDependency(t *testing.T) {
	_, err := NewBuilder(def("missing", map[string]*TaskDefinition{
		"a": task("echo a", "nonexistent"),
	})).Build()
	if err == nil {
		t.Fatal("expected error for missing dependency, got nil")
	}
	if !strings.Contains(err.Error(), "dependency resolution failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildCycleDetection(t *testing.T) {
	// task1 → task2 → task1
	_, err := NewBuilder(def("cycle", map[string]*TaskDefinition{
		"task1": task("echo 1", "task2"),
		"task2": task("echo 2", "task1"),
	})).Build()
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "circular dependency") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildSelfDependency(t *testing.T) {
	_, err := NewBuilder(def("self", map[string]*TaskDefinition{
		"a": task("echo a", "a"),
	})).Build()
	if err == nil {
		t.Fatal("expected cycle error for self-dependency, got nil")
	}
}

// ─── Forensic tasks ───────────────────────────────────────────────────────────

func TestForensicTaskExcludedFromNormalFlow(t *testing.T) {
	g := mustBuild(t, def("forensic", map[string]*TaskDefinition{
		"normal":   task("echo ok"),
		"forensic": {Cmd: "echo trap", Type: TaskTypeForensic},
	}))

	// Forensic task should NOT appear in RootNodes or Levels.
	for _, n := range g.RootNodes {
		if n.TaskDef.Type == TaskTypeForensic {
			t.Errorf("forensic node %q must not appear in RootNodes", n.ID)
		}
	}
	for _, level := range g.Levels {
		for _, n := range level {
			if n.TaskDef.Type == TaskTypeForensic {
				t.Errorf("forensic node %q must not appear in Levels", n.ID)
			}
		}
	}
}

func TestGlobalTrapAttachment(t *testing.T) {
	d := &WorkflowDefinition{
		Name:      "trapped",
		OnFailure: "trap",
		Tasks: map[string]*TaskDefinition{
			"a":    {Cmd: "echo ok"},
			"trap": {Cmd: "echo trap", Type: TaskTypeForensic},
		},
	}

	g, err := NewBuilder(d).Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// The global trap node should be in dag.GlobalTrap, not dag.Nodes.
	if g.GlobalTrap == nil {
		t.Fatal("global trap not set on DAG")
	}
	if _, inNodes := g.Nodes["__GLOBAL_FAILURE_TRAP__"]; inNodes {
		t.Error("global trap should not be in dag.Nodes (it inflates TotalTasks)")
	}
	if g.GlobalTrap.TaskDef.Type != TaskTypeForensic {
		t.Errorf("trap node type: want forensic, got %q", g.GlobalTrap.TaskDef.Type)
	}
}

// ─── Matrix expansion ────────────────────────────────────────────────────────

func TestMatrixExpansion2x2(t *testing.T) {
	d := &WorkflowDefinition{
		Name: "matrix",
		Tasks: map[string]*TaskDefinition{
			"test": {
				Cmd:    "echo {{.env}} {{.region}}",
				Matrix: map[string][]string{"env": {"dev", "prod"}, "region": {"us", "eu"}},
			},
		},
	}

	g, err := NewBuilder(d).Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// 2x2 = 4 expanded nodes.
	if len(g.Nodes) != 4 {
		t.Errorf("want 4 matrix expansions, got %d", len(g.Nodes))
	}
	for _, node := range g.Nodes {
		if !node.IsExpanded {
			t.Errorf("node %q should be marked IsExpanded=true", node.ID)
		}
		if len(node.MatrixVars) != 2 {
			t.Errorf("node %q should have 2 MatrixVars, got %d", node.ID, len(node.MatrixVars))
		}
	}
}

func TestMatrixNodeIDFormat(t *testing.T) {
	d := &WorkflowDefinition{
		Name: "matrix-id",
		Tasks: map[string]*TaskDefinition{
			"build": {
				Cmd:    "echo build",
				Matrix: map[string][]string{"env": {"staging"}},
			},
		},
	}

	g, err := NewBuilder(d).Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Expect exactly one node named "build[env=staging]".
	if _, ok := g.Nodes["build[env=staging]"]; !ok {
		t.Errorf("expected node 'build[env=staging]', nodes: %v", nodeIDs(g))
	}
}

// ─── Level & dependency integrity ────────────────────────────────────────────

func TestLevelMonotonicity(t *testing.T) {
	// Every dependency of a node must be at a strictly lower level.
	g := mustBuild(t, def("chain", map[string]*TaskDefinition{
		"a": task("a"),
		"b": task("b", "a"),
		"c": task("c", "b"),
		"d": task("d", "c"),
	}))

	for _, node := range g.Nodes {
		for _, dep := range node.Dependencies {
			if dep.Level >= node.Level {
				t.Errorf("node %q (level %d) has dep %q at same/higher level %d",
					node.ID, node.Level, dep.ID, dep.Level)
			}
		}
	}
}

func TestValidatedFlag(t *testing.T) {
	g := mustBuild(t, def("v", map[string]*TaskDefinition{"a": task("echo a")}))
	if !g.Validated {
		t.Error("DAG.Validated should be true after Build()")
	}
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

func BenchmarkBuildSimple(b *testing.B) {
	d := def("simple", map[string]*TaskDefinition{"a": task("echo a")})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewBuilder(d).Build(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildDiamond(b *testing.B) {
	d := def("diamond", map[string]*TaskDefinition{
		"a": task("echo a"),
		"b": task("echo b", "a"),
		"c": task("echo c", "a"),
		"d": task("echo d", "b", "c"),
	})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewBuilder(d).Build(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildWide100(b *testing.B) {
	tasks := make(map[string]*TaskDefinition, 100)
	for i := 0; i < 100; i++ {
		tasks[fmt.Sprintf("t%d", i)] = task(fmt.Sprintf("echo %d", i))
	}
	d := def("wide100", tasks)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewBuilder(d).Build(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildLinearChain50(b *testing.B) {
	tasks := make(map[string]*TaskDefinition, 50)
	tasks["t0"] = task("echo 0")
	for i := 1; i < 50; i++ {
		tasks[fmt.Sprintf("t%d", i)] = task(fmt.Sprintf("echo %d", i), fmt.Sprintf("t%d", i-1))
	}
	d := def("chain50", tasks)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewBuilder(d).Build(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildMatrix3x3(b *testing.B) {
	d := &WorkflowDefinition{
		Name: "matrix3x3",
		Tasks: map[string]*TaskDefinition{
			"job": {
				Cmd: "echo {{.a}} {{.b}}",
				Matrix: map[string][]string{
					"a": {"1", "2", "3"},
					"b": {"x", "y", "z"},
				},
			},
		},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewBuilder(d).Build(); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func levelNames(g *DAG) [][]string {
	result := make([][]string, len(g.Levels))
	for i, level := range g.Levels {
		names := make([]string, len(level))
		for j, n := range level {
			names[j] = n.ID
		}
		result[i] = names
	}
	return result
}

func nodeIDs(g *DAG) []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	return ids
}
