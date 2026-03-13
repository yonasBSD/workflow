//go:build windows

package storage

// journalMode returns the SQLite journal mode to use on this platform.
// DELETE (rollback journal) avoids WAL sidecar files (.db-wal, .db-shm)
// which Windows holds with exclusive locks that can prevent temp-dir cleanup
// in tests and cause "file in use" errors when a process exits abnormally.
func journalMode() string { return "DELETE" }
