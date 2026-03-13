//go:build !windows

package storage

// journalMode returns the SQLite journal mode to use on this platform.
// WAL (Write-Ahead Logging) gives better concurrent read/write throughput
// and is safe on Unix where file-handle semantics allow clean WAL teardown.
func journalMode() string { return "WAL" }
