// Package examples validates that every workflow in files/examples/ can be
// parsed, built, and executed end-to-end without errors.
//
// Validation (parse + build) always runs. Execution is skipped when
// go test -short is passed, because many examples contain sleep commands
// that make full execution slow in CI.
package examples

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/silocorp/workflow/internal/config"
	"github.com/silocorp/workflow/internal/contextmap"
	"github.com/silocorp/workflow/internal/dag"
	"github.com/silocorp/workflow/internal/executor"
	"github.com/silocorp/workflow/internal/logger"
	"github.com/silocorp/workflow/internal/storage"
)

func init() {
	logger.Init(logger.Config{Level: "error", Format: "console"})
}

// examplesDir returns the absolute path to files/examples/ regardless of
// where the test binary runs from.
func examplesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile = …/tests/examples/examples_test.go
	// root     = …/
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(root, "files", "examples")
}

// discoverExamples returns a list of (name, content) pairs for every .toml
// file found in files/examples/.
func discoverExamples(t *testing.T) []struct{ name, content string } {
	t.Helper()
	dir := examplesDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read examples dir %s: %v", dir, err)
	}
	var out []struct{ name, content string }
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".toml")
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read example %s: %v", e.Name(), err)
		}
		out = append(out, struct{ name, content string }{name, string(raw)})
	}
	if len(out) == 0 {
		t.Fatalf("no .toml files found in %s", dir)
	}
	return out
}

// TestExamplesValidate confirms every example parses and builds without error.
// This is the regression gate for TOML syntax and DAG construction.
func TestExamplesValidate(t *testing.T) {
	examples := discoverExamples(t)

	for _, ex := range examples {
		ex := ex // capture range variable
		t.Run(ex.name, func(t *testing.T) {
			// No t.Parallel() — config.Get().Paths.Workflows is global state;
			// concurrent writes would cause a data race.

			// Isolate workflows dir per sub-test.
			wfDir := t.TempDir()
			tomlPath := filepath.Join(wfDir, ex.name+".toml")
			if err := os.WriteFile(tomlPath, []byte(ex.content), 0644); err != nil {
				t.Fatalf("write toml: %v", err)
			}
			config.Get().Paths.Workflows = wfDir

			def, err := dag.NewParser(ex.name).Parse()
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			if _, err := dag.NewBuilder(def).Build(); err != nil {
				t.Fatalf("build failed: %v", err)
			}
		})
	}
}

// TestExamplesRun executes every example end-to-end using the sequential
// executor. Skipped when -short is passed (examples contain sleep commands).
func TestExamplesRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow example execution under -short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("example workflows use Unix shell commands — not runnable on Windows")
	}

	examples := discoverExamples(t)

	for _, ex := range examples {
		ex := ex
		t.Run(ex.name, func(t *testing.T) {
			// No t.Parallel() — config.Get() paths are global state.
			wfDir := t.TempDir()
			logDir := t.TempDir()
			tomlPath := filepath.Join(wfDir, ex.name+".toml")
			if err := os.WriteFile(tomlPath, []byte(ex.content), 0644); err != nil {
				t.Fatalf("write toml: %v", err)
			}
			config.Get().Paths.Workflows = wfDir
			config.Get().Paths.Logs = logDir

			def, err := dag.NewParser(ex.name).Parse()
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			d, err := dag.NewBuilder(def).Build()
			if err != nil {
				t.Fatalf("build: %v", err)
			}

			store, err := storage.New(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatalf("store: %v", err)
			}
			defer store.Close()

			run, err := executor.NewSequentialExecutor(store).Execute(
				context.Background(), d, contextmap.NewContextMap(),
			)
			if err != nil {
				t.Fatalf("execute returned error: %v", err)
			}
			if run.Status != storage.RunSuccess {
				t.Errorf("expected status %s, got %s", storage.RunSuccess, run.Status)
			}
		})
	}
}
