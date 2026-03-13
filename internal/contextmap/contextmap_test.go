package contextmap

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joelfokou/workflow/internal/logger"
)

func TestMain(m *testing.M) {
	logger.Init(logger.Config{Level: "error", Format: "console"}) //nolint:errcheck
	os.Exit(m.Run())
}

// ─── Set / Get ────────────────────────────────────────────────────────────────

func TestSetAndGet(t *testing.T) {
	cm := NewContextMap()

	if err := cm.Set("taskA", "greeting", "hello"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, ok := cm.Get("greeting")
	if !ok {
		t.Fatal("Get: variable not found after Set")
	}
	if v.(string) != "hello" {
		t.Errorf("want %q, got %q", "hello", v)
	}
}

func TestGetMissingReturnsNotFound(t *testing.T) {
	cm := NewContextMap()
	_, ok := cm.Get("nonexistent")
	if ok {
		t.Error("Get should return false for a non-existent key")
	}
}

func TestSetSameTaskCanOverwrite(t *testing.T) {
	cm := NewContextMap()
	cm.Set("t1", "x", "first") //nolint:errcheck
	if err := cm.Set("t1", "x", "second"); err != nil {
		t.Errorf("same-task overwrite should succeed: %v", err)
	}
	v, _ := cm.Get("x")
	if v.(string) != "second" {
		t.Errorf("want %q, got %q", "second", v)
	}
}

func TestSetDifferentTaskCannotOverwrite(t *testing.T) {
	cm := NewContextMap()
	cm.Set("t1", "shared", "by-t1") //nolint:errcheck
	err := cm.Set("t2", "shared", "by-t2")
	if err == nil {
		t.Fatal("overwrite by different task should fail")
	}
	if !strings.Contains(err.Error(), "already set by task") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ─── Type inference ───────────────────────────────────────────────────────────

func TestTypeInferenceString(t *testing.T) {
	cm := NewContextMap()
	cm.Set("t", "s", "hello") //nolint:errcheck
	v := cm.variables["s"]
	if v.Type != VarTypeString {
		t.Errorf("want VarTypeString, got %v", v.Type)
	}
}

func TestTypeInferenceInt(t *testing.T) {
	cm := NewContextMap()
	cm.Set("t", "n", 42) //nolint:errcheck
	v := cm.variables["n"]
	if v.Type != VarTypeInt {
		t.Errorf("want VarTypeInt, got %v", v.Type)
	}
}

func TestTypeInferenceFloat(t *testing.T) {
	cm := NewContextMap()
	cm.Set("t", "f", 3.14) //nolint:errcheck
	if cm.variables["f"].Type != VarTypeFloat {
		t.Error("want VarTypeFloat")
	}
}

func TestTypeInferenceBool(t *testing.T) {
	cm := NewContextMap()
	cm.Set("t", "b", true) //nolint:errcheck
	if cm.variables["b"].Type != VarTypeBool {
		t.Error("want VarTypeBool")
	}
}

func TestTypeInferenceUnsupported(t *testing.T) {
	cm := NewContextMap()
	err := cm.Set("t", "bad", []int{1, 2, 3})
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

// ─── Read-only (matrix) variables ────────────────────────────────────────────

func TestSetMatrixCreatesReadOnlyVars(t *testing.T) {
	cm := NewContextMap()
	cm.SetMatrix("task[env=dev]", map[string]string{"env": "dev", "region": "us"}) //nolint:errcheck

	// Should be scoped as "<taskID>.<varName>"
	v, ok := cm.variables["task[env=dev].env"]
	if !ok {
		t.Fatal("matrix variable not stored with scoped key")
	}
	if !v.ReadOnly {
		t.Error("matrix variable should be read-only")
	}
}

func TestReadOnlyCannotBeOverwritten(t *testing.T) {
	cm := NewContextMap()
	cm.SetMatrix("t", map[string]string{"env": "dev"}) //nolint:errcheck

	// Attempt to overwrite via Set (uses same scoped name).
	err := cm.Set("t", "t.env", "prod")
	if err == nil {
		t.Error("expected error overwriting read-only variable")
	}
}

// ─── EvalCondition ────────────────────────────────────────────────────────────

func seedCM(t *testing.T) *ContextMap {
	t.Helper()
	cm := NewContextMap()
	cm.Set("t", "count", 5)          //nolint:errcheck
	cm.Set("t", "ratio", 0.75)       //nolint:errcheck
	cm.Set("t", "env", "production") //nolint:errcheck
	cm.Set("t", "enabled", true)     //nolint:errcheck
	return cm
}

func TestEvalConditionIntGT(t *testing.T) {
	cm := seedCM(t)
	ok, err := cm.EvalCondition("count > 3")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("5 > 3 should be true")
	}
}

func TestEvalConditionIntLT(t *testing.T) {
	cm := seedCM(t)
	ok, err := cm.EvalCondition("count < 10")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("5 < 10 should be true")
	}
}

func TestEvalConditionIntEQ(t *testing.T) {
	cm := seedCM(t)
	ok, err := cm.EvalCondition("count == 5")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("5 == 5 should be true")
	}
}

func TestEvalConditionIntNE(t *testing.T) {
	cm := seedCM(t)
	ok, err := cm.EvalCondition("count != 99")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("5 != 99 should be true")
	}
}

func TestEvalConditionIntGTE(t *testing.T) {
	cm := seedCM(t)
	ok, _ := cm.EvalCondition("count >= 5")
	if !ok {
		t.Error("5 >= 5 should be true")
	}
}

func TestEvalConditionIntLTE(t *testing.T) {
	cm := seedCM(t)
	ok, _ := cm.EvalCondition("count <= 5")
	if !ok {
		t.Error("5 <= 5 should be true")
	}
}

func TestEvalConditionFloat(t *testing.T) {
	cm := seedCM(t)
	ok, err := cm.EvalCondition("ratio > 0.5")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("0.75 > 0.5 should be true")
	}
}

func TestEvalConditionString(t *testing.T) {
	cm := seedCM(t)
	ok, err := cm.EvalCondition(`env == "production"`)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error(`"production" == "production" should be true`)
	}
}

func TestEvalConditionStringNE(t *testing.T) {
	cm := seedCM(t)
	ok, err := cm.EvalCondition(`env != "staging"`)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error(`"production" != "staging" should be true`)
	}
}

func TestEvalConditionBool(t *testing.T) {
	cm := seedCM(t)
	ok, err := cm.EvalCondition("enabled == true")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("true == true should be true")
	}
}

func TestEvalConditionUndefinedVar(t *testing.T) {
	cm := NewContextMap()
	_, err := cm.EvalCondition("missing == 1")
	if err == nil {
		t.Error("expected error for undefined variable")
	}
}

func TestEvalConditionNoOperator(t *testing.T) {
	cm := NewContextMap()
	_, err := cm.EvalCondition("just_a_name")
	if err == nil {
		t.Error("expected error when no operator is found")
	}
}

// ─── Snapshot / Restore ───────────────────────────────────────────────────────

func TestSnapshotAndRestore(t *testing.T) {
	cm := NewContextMap()
	cm.Set("t", "x", "original") //nolint:errcheck

	data, err := cm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	cm2 := NewContextMap()
	if err := cm2.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	v, ok := cm2.Get("x")
	if !ok {
		t.Fatal("restored map should contain 'x'")
	}
	if v.(string) != "original" {
		t.Errorf("want %q, got %v", "original", v)
	}
}

// ─── RestoreVariable ─────────────────────────────────────────────────────────

func TestRestoreVariableBypassesOwnership(t *testing.T) {
	cm := NewContextMap()
	cm.Set("task1", "myvar", "v1") //nolint:errcheck

	// Restoring with a different task ID should succeed (resume path).
	cm.RestoreVariable("myvar", "v2", VarTypeString, "task2", time.Now(), false)
	v, _ := cm.Get("myvar")
	if v.(string) != "v2" {
		t.Errorf("want v2, got %v", v)
	}
}

// ─── Audit trail ─────────────────────────────────────────────────────────────

func TestAuditTrailRecordsEntries(t *testing.T) {
	cm := NewContextMap()
	cm.Set("t1", "a", "v1") //nolint:errcheck
	cm.Set("t1", "a", "v2") //nolint:errcheck

	trail := cm.GetAuditTrail()
	if len(trail) != 2 {
		t.Errorf("want 2 audit entries, got %d", len(trail))
	}
}

// ─── Concurrent access ────────────────────────────────────────────────────────

func TestConcurrentSetGet(t *testing.T) {
	cm := NewContextMap()
	var wg sync.WaitGroup

	// N goroutines each set their own variable and read it back.
	const N = 50
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "var"
			taskID := "t"
			// Use unique keys per goroutine to avoid ownership conflicts.
			uniqueKey := strings.Join([]string{key, strings.Repeat("x", id+1)}, "_")
			if err := cm.Set(taskID+strings.Repeat("_", id+1), uniqueKey, id); err != nil {
				t.Errorf("Set(%q): %v", uniqueKey, err)
			}
			cm.Get(uniqueKey) //nolint:errcheck
		}(i)
	}
	wg.Wait()
}

func TestConcurrentReads(t *testing.T) {
	cm := NewContextMap()
	cm.Set("t", "shared", "value") //nolint:errcheck

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, ok := cm.Get("shared")
			if !ok || v.(string) != "value" {
				t.Errorf("concurrent Get failed: ok=%v v=%v", ok, v)
			}
		}()
	}
	wg.Wait()
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

func BenchmarkSet(b *testing.B) {
	cm := NewContextMap()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		key := "key"
		cm.Set("task", key, "value") //nolint:errcheck
	}
}

func BenchmarkGet(b *testing.B) {
	cm := NewContextMap()
	cm.Set("t", "x", "hello") //nolint:errcheck
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cm.Get("x")
	}
}

func BenchmarkEvalConditionInt(b *testing.B) {
	cm := NewContextMap()
	cm.Set("t", "count", 42) //nolint:errcheck
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cm.EvalCondition("count > 10") //nolint:errcheck
	}
}

func BenchmarkConcurrentGet(b *testing.B) {
	cm := NewContextMap()
	cm.Set("t", "x", "val") //nolint:errcheck
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cm.Get("x")
		}
	})
}

func BenchmarkSnapshot(b *testing.B) {
	cm := NewContextMap()
	for i := 0; i < 20; i++ {
		key := strings.Repeat("k", i+1)
		cm.Set("t", key, "value") //nolint:errcheck
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cm.Snapshot() //nolint:errcheck
	}
}
