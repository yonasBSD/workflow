package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/silocorp/workflow/internal/config"
	"github.com/silocorp/workflow/internal/logger"
	"github.com/silocorp/workflow/internal/storage"
	"github.com/spf13/cobra"
)

var (
	follow    bool
	tailLines int
)

// logsCmd shows logs for a specific run or task within a run. It queries the database for task information and reads the corresponding log files.
var logsCmd = &cobra.Command{
	Use:   "logs <run_id> [task]",
	Short: "Show logs for a run or specific task",
	Long:  "Display logs for a workflow run or a specific task within that run. Supports tailing and following.",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		runID := args[0]
		dbPath := config.Get().Paths.Database

		store, err := storage.New(dbPath)
		if err != nil {
			logger.Error("failed to initialise run store", "error", err)
			return fmt.Errorf("failed to initialise run store: %w", err)
		}
		defer store.Close()

		// Verify run exists
		workflowRun, err := store.GetRun(runID)
		if err != nil {
			logger.Error("run not found", "run_id", runID, "error", err)
			return fmt.Errorf("run '%s' not found: %w", runID, err)
		}

		// Load all tasks for this run
		tasks, err := store.ListTaskExecutions(storage.TaskFilters{RunID: runID})
		if err != nil {
			logger.Error("failed to load tasks for run", "run_id", runID, "error", err)
			return fmt.Errorf("failed to load tasks for run '%s': %w", runID, err)
		}

		if len(tasks) == 0 {
			fmt.Printf("No task executions found for run '%s'\n", runID)
			return nil
		}

		// Setup cancellation on interrupt so follow mode exits cleanly.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			select {
			case <-signals:
				cancel()
			case <-ctx.Done():
			}
		}()

		if len(args) == 2 {
			return showTaskLogs(ctx, workflowRun, tasks, args[1], tailLines, follow)
		}

		showRunLogs(ctx, workflowRun, tasks, tailLines, follow)
		return nil
	},
}

// showRunLogs displays logs for all tasks in a run.
func showRunLogs(ctx context.Context, run *storage.Run, tasks []*storage.TaskExecution, tailLines int, follow bool) {
	fmt.Printf("=== Logs for Run '%s' (%s) ===\n\n", run.ID, run.WorkflowName)

	var wg sync.WaitGroup
	var mu sync.Mutex // ensure lines don't interleave

	for _, task := range tasks {
		t := task // copy for goroutine
		prefix := fmt.Sprintf("[%s] ", t.TaskID)

		// Print task header
		mu.Lock()
		fmt.Printf("%sStatus: %s | Exit Code: ", prefix, t.State)
		if t.ExitCode.Valid {
			fmt.Printf("%d\n", t.ExitCode.Int64)
		} else {
			fmt.Printf("N/A\n")
		}
		if t.ErrorMessage.Valid && t.ErrorMessage.String != "" {
			fmt.Printf("%sLast Error: %s\n", prefix, t.ErrorMessage.String)
		}
		mu.Unlock()

		if !t.LogPath.Valid || t.LogPath.String == "" {
			mu.Lock()
			fmt.Printf("%s(No logs recorded)\n\n", prefix)
			mu.Unlock()
			continue
		}

		// Stream or print last N lines
		wg.Add(1)
		go func(path, prefix string) {
			defer wg.Done()
			if err := streamFile(ctx, path, prefix, tailLines, follow, &mu); err != nil {
				logger.Warn("failed to stream log file", "run_id", run.ID, "task", prefix, "file", path, "error", err)
				mu.Lock()
				fmt.Printf("%s(Unable to read log: %v)\n\n", prefix, err)
				mu.Unlock()
			}
		}(t.LogPath.String, prefix)
	}

	// Wait for non-follow readers to finish. If follow is true, they return when context canceled.
	wg.Wait()

	logger.Info("displayed logs for run", "run_id", run.ID)
}

// showTaskLogs displays logs for a specific task.
func showTaskLogs(ctx context.Context, run *storage.Run, tasks []*storage.TaskExecution, taskID string, tailLines int, follow bool) error {
	var targetTaskExecutions []*storage.TaskExecution
	for i := range tasks {
		if tasks[i].TaskID == taskID {
			targetTaskExecutions = append(targetTaskExecutions, tasks[i])
		}
	}

	if len(targetTaskExecutions) == 0 {
		logger.Error("task not found in run", "run_id", run.ID, "task", taskID)
		return fmt.Errorf("task '%s' not found in run '%s'", taskID, run.ID)
	}

	last := targetTaskExecutions[len(targetTaskExecutions)-1]
	first := targetTaskExecutions[0]
	elapsed := last.EndTime.Time.Sub(first.StartTime.Time).Seconds()

	fmt.Printf("=== Logs for Task '%s' in Run '%s' ===\n\n", taskID, run.ID)
	fmt.Printf("Status:   %v\n", last.State)
	fmt.Printf("Attempts: %d\n", len(targetTaskExecutions))
	fmt.Printf("Started:  %s\n", first.StartTime.Time.Format("2006-01-02 15:04:05"))
	fmt.Printf("Ended:    %s\n", last.EndTime.Time.Format("2006-01-02 15:04:05"))
	fmt.Printf("Duration: %.3fs\n", elapsed)
	fmt.Printf("ExitCode: %d\n", last.ExitCode.Int64)

	fmt.Println("\n--- Output ---")

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, t := range targetTaskExecutions {
		prefix := fmt.Sprintf("[Attempt %d] ", t.Attempt)

		if t.LogPath.String == "" {
			mu.Lock()
			fmt.Printf("%s(No logs recorded)\n\n", prefix)
			mu.Unlock()
			continue
		}

		// Stream or print last N lines
		wg.Add(1)
		go func(path, prefix string) {
			defer wg.Done()
			if err := streamFile(ctx, path, prefix, tailLines, follow, &mu); err != nil {
				logger.Warn("failed to stream log file", "run_id", run.ID, "task", prefix, "file", path, "error", err)
				mu.Lock()
				fmt.Printf("%s(Unable to read log: %v)\n\n", prefix, err)
				mu.Unlock()
			}
		}(t.LogPath.String, prefix)
	}

	// Wait for non-follow readers to finish. If follow is true, they return when context canceled.
	wg.Wait()

	if targetTaskExecutions[len(targetTaskExecutions)-1].ErrorMessage.Valid && targetTaskExecutions[len(targetTaskExecutions)-1].ErrorMessage.String != "" {
		fmt.Println("\n--- Error ---")
		fmt.Println(targetTaskExecutions[len(targetTaskExecutions)-1].ErrorMessage.String)
	}

	logger.Info("displayed logs for task", "run_id", run.ID, "task", taskID)

	return nil
}

// streamFile prints the last `tail` lines of the file and, if follow is true, keeps streaming appended data until ctx is cancelled.
// prefix is printed before each output line (can be empty).
func streamFile(ctx context.Context, path, prefix string, tail int, follow bool, mu *sync.Mutex) error {
	// Quick existence check
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("log path is a directory")
	}

	// Print last N lines
	if tail > 0 {
		last, err := readLastLines(path, tail)
		if err != nil {
			return err
		}
		if last != "" {
			mu.Lock()
			for _, l := range strings.Split(last, "\n") {
				if l == "" {
					continue
				}
				if prefix != "" {
					fmt.Printf("%s%s\n", prefix, l)
				} else {
					fmt.Println(l)
				}
			}
			fmt.Println()
			mu.Unlock()
		}
	} else {
		// Print entire file if tail==0
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(content) > 0 {
			mu.Lock()
			for _, l := range strings.Split(strings.TrimRight(string(content), "\n"), "\n") {
				if prefix != "" {
					fmt.Printf("%s%s\n", prefix, l)
				} else {
					fmt.Println(l)
				}
			}
			fmt.Println()
			mu.Unlock()
		}
	}

	if !follow {
		return nil
	}

	// Follow mode: open file and seek to end, then read appended data.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Start at end of file
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	reader := bufio.NewReader(f)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// Attempt to read a line; if none, wait and retry
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					time.Sleep(200 * time.Millisecond)
					continue
				}
				return err
			}
			line = strings.TrimRight(line, "\n")
			mu.Lock()
			if prefix != "" {
				fmt.Printf("%s%s\n", prefix, line)
			} else {
				fmt.Println(line)
			}
			mu.Unlock()
		}
	}
}

// readLastLines reads the file and returns the last n lines joined by newline.
// Simple implementation: reads entire file into memory (suitable for typical log sizes).
func readLastLines(path string, n int) (string, error) {
	if n <= 0 {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return "", nil
	}
	if n >= len(lines) {
		return strings.Join(lines, "\n"), nil
	}
	return strings.Join(lines[len(lines)-n:], "\n"), nil
}

func init() {
	logsCmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output (like tail -f)")
	logsCmd.Flags().IntVarP(&tailLines, "tail", "n", 100, "Show last N lines (0 to show entire file)")
	rootCmd.AddCommand(logsCmd)
}
