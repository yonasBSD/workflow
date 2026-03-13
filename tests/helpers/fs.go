// Package helpers - fs provides utility functions for filesystem operations in tests.
package helpers

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type TestFS struct {
	Root string
}

func NewTestFS(t *testing.T) *TestFS {
	t.Helper()
	dir, err := os.MkdirTemp("", "wf-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAll(dir) })
	return &TestFS{Root: dir}
}

// removeAll removes dir. On Windows, transient file locks (e.g. from
// antivirus scanning freshly written SQLite files) can cause the first attempt
// to fail with "file in use". We retry with exponential back-off up to a 2-
// second deadline — the same strategy used by Go's own testing package for
// t.TempDir cleanup on Windows.
func removeAll(dir string) {
	if runtime.GOOS != "windows" {
		os.RemoveAll(dir) //nolint:errcheck
		return
	}
	const deadline = 2 * time.Second
	start := time.Now()
	sleep := 5 * time.Millisecond
	for {
		if err := os.RemoveAll(dir); err == nil {
			return
		}
		if time.Since(start)+sleep > deadline {
			os.RemoveAll(dir) //nolint:errcheck
			return
		}
		time.Sleep(sleep)
		sleep *= 2
	}
}

func (fs *TestFS) Path(parts ...string) string {
	return filepath.Join(append([]string{fs.Root}, parts...)...)
}

func (fs *TestFS) Write(rel string, content string) {
	path := fs.Path(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		panic(err)
	}
}

func (fs *TestFS) Cleanup() {
	removeAll(fs.Root)
}
