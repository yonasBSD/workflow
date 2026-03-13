package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/silocorp/workflow/internal/config"
	"github.com/silocorp/workflow/internal/storage"
	"github.com/spf13/cobra"
)

var (
	exportFormat string
	exportOutput string
)

// RunExport is the full export payload serialised to JSON.
type RunExport struct {
	ExportedAt       time.Time                  `json:"exported_at"`
	Run              *storage.Run               `json:"run"`
	Tags             []string                   `json:"tags"`
	Tasks            []*storage.TaskExecution   `json:"tasks"`
	Dependencies     []*storage.TaskDependency  `json:"dependencies"`
	ContextSnapshots []*storage.ContextSnapshot `json:"context_snapshots"`
	ForensicLogs     []*storage.ForensicLog     `json:"forensic_logs"`
	AuditTrail       []*storage.AuditTrailEntry `json:"audit_trail"`
}

// exportCmd implements `wf export <run-id>`.
var exportCmd = &cobra.Command{
	Use:   "export <run-id>",
	Short: "Export a complete run record as JSON or tar.gz archive",
	Long: `Export all data for a workflow run — run metadata, task executions,
dependencies, context snapshots, forensic logs, audit trail entries, and
any on-disk task log files — to a portable JSON file or a tar.gz archive.

Formats:
  json    Single JSON file containing all DB records (default)
  tar     Compressed tar archive: run.json + task log files

Examples:
  wf export 2abc123                          # JSON to stdout
  wf export 2abc123 --format json -o run.json
  wf export 2abc123 --format tar  -o run.tar.gz`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runID := args[0]

		st, err := storage.New(config.Get().Paths.Database)
		if err != nil {
			return fmt.Errorf("failed to open store: %w", err)
		}
		defer st.Close()

		payload, err := buildExportPayload(st, runID)
		if err != nil {
			return err
		}

		switch exportFormat {
		case "json":
			return writeExportJSON(payload, exportOutput)
		case "tar":
			return writeExportTar(payload, exportOutput, config.Get().Paths.Logs)
		default:
			return fmt.Errorf("unknown format %q: use json or tar", exportFormat)
		}
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVarP(&exportFormat, "format", "f", "json", "Export format: json or tar")
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file path (default: stdout for json, required for tar)")
}

// buildExportPayload gathers all DB records for a run.
func buildExportPayload(st *storage.Store, runID string) (*RunExport, error) {
	run, err := st.GetRun(runID)
	if err != nil {
		return nil, fmt.Errorf("run %q not found: %w", runID, err)
	}

	tasks, err := st.ListTaskExecutions(storage.TaskFilters{RunID: runID})
	if err != nil {
		return nil, fmt.Errorf("failed to list task executions: %w", err)
	}

	deps, err := st.ListAllTaskDependencies(runID)
	if err != nil {
		return nil, fmt.Errorf("failed to list task dependencies: %w", err)
	}

	snapshots, err := st.ListContextSnapshots(storage.ContextSnapshotFilters{RunID: runID})
	if err != nil {
		return nil, fmt.Errorf("failed to list context snapshots: %w", err)
	}

	forensic, err := st.ListForensicLogs(storage.ForensicLogFilters{RunID: runID})
	if err != nil {
		return nil, fmt.Errorf("failed to list forensic logs: %w", err)
	}

	audit, err := st.ListAuditTrail(storage.AuditTrailFilters{RunID: runID})
	if err != nil {
		return nil, fmt.Errorf("failed to list audit trail: %w", err)
	}

	return &RunExport{
		ExportedAt:       time.Now().UTC(),
		Run:              run,
		Tags:             run.RunTags(),
		Tasks:            tasks,
		Dependencies:     deps,
		ContextSnapshots: snapshots,
		ForensicLogs:     forensic,
		AuditTrail:       audit,
	}, nil
}

// writeExportJSON marshals the payload to JSON. If outPath is empty it writes
// to stdout.
func writeExportJSON(payload *RunExport, outPath string) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal export: %w", err)
	}

	if outPath == "" {
		fmt.Println(string(data))
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	if err := os.WriteFile(outPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}
	fmt.Printf("exported run %s → %s\n", payload.Run.ID, outPath)
	return nil
}

// writeExportTar writes a .tar.gz archive containing run.json and any task log
// files referenced by the task executions.
func writeExportTar(payload *RunExport, outPath, logsDir string) error {
	if outPath == "" {
		return fmt.Errorf("--output is required for tar format")
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	// Write run.json into the archive.
	jsonData, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal export: %w", err)
	}
	if err := tarWriteBytes(tw, "run.json", jsonData); err != nil {
		return err
	}

	// Include each task log file that exists on disk.
	seen := make(map[string]bool)
	for _, t := range payload.Tasks {
		if !t.LogPath.Valid || t.LogPath.String == "" {
			continue
		}
		logPath := t.LogPath.String
		if !filepath.IsAbs(logPath) {
			logPath = filepath.Join(logsDir, logPath)
		}
		if seen[logPath] {
			continue
		}
		seen[logPath] = true

		if err := tarAddFile(tw, logPath, filepath.Join("logs", filepath.Base(logPath))); err != nil {
			// Missing log files are non-fatal: the task may have been skipped.
			continue
		}
	}

	fmt.Printf("exported run %s → %s (%d tasks, %d log files)\n",
		payload.Run.ID, outPath, len(payload.Tasks), len(seen))
	return nil
}

// tarWriteBytes adds raw bytes as a file entry inside the tar archive.
func tarWriteBytes(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0644,
		Size:    int64(len(data)),
		ModTime: time.Now().UTC(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("tar write header %q: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("tar write data %q: %w", name, err)
	}
	return nil
}

// tarAddFile adds an existing file on disk into the tar archive.
func tarAddFile(tw *tar.Writer, srcPath, archiveName string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	hdr := &tar.Header{
		Name:    archiveName,
		Mode:    0644,
		Size:    info.Size(),
		ModTime: info.ModTime().UTC(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("tar write header %q: %w", archiveName, err)
	}
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("tar copy %q: %w", archiveName, err)
	}
	return nil
}
