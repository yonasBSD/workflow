// Package main is the entry point for the WorkFlow CLI application.
// It initialises and executes the command-line interface by calling cmd.Execute().
package main

import (
	"github.com/silocorp/workflow/cmd"
)

// version, commit, and date are set by GoReleaser via ldflags at build time.
// When built without ldflags (e.g. go build), they default to "dev".
var (
	version = "dev"
	commit  = "dev"
	date    = "unknown"
)

// main is the entry point of the application.
// It delegates execution to the cmd package's Execute function.
func main() {
	cmd.SetVersionInfo(version, commit, date)
	cmd.Execute()
}
