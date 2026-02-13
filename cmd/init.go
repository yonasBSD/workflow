package cmd

import (
	"fmt"
	"os"
	"path"

	"github.com/joelfokou/workflow/internal/config"
	"github.com/joelfokou/workflow/internal/logger"
	"github.com/joelfokou/workflow/internal/run"
	"github.com/spf13/cobra"
)

// initCmd initialises a new workflow project by creating necessary directories based on the configuration settings and initialises the SQLite database and creates example files.
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialise workflow directories and database",
	Long:  "Create necessary directories and initialise the SQLite database for run tracking",
	RunE: func(cmd *cobra.Command, args []string) error {
		dirs := []string{
			config.Get().Paths.Workflows,
			config.Get().Paths.Logs,
		}

		for _, dir := range dirs {
			if err := os.MkdirAll(dir, 0755); err != nil {
				logger.Error("failed to create directory", "path", dir, "error", err)
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
			logger.Debug("directory created or already exists", "path", dir)
		}

		// Initialise SQLite database
		dbPath := config.Get().Paths.Database
		store, err := run.NewStore(dbPath)
		if err != nil {
			logger.Error("failed to initialise database", "path", dbPath, "error", err)
			return fmt.Errorf("failed to initialise database: %w", err)
		}
		store.Close()

		// Initialise config file & parent directories, if they don't exist
		cfgFile := config.ConfigFile()
		cfgDir := path.Dir(cfgFile)

		if err := os.MkdirAll(cfgDir, 0755); err != nil {
			logger.L().Error("failed to create platform configuration directory for user config", zap.String("path", cfgDir), zap.Error(err))

			return fmt.Errorf("failed to create platform configuration directory for user config: %w", err)
		}

		if _, err := os.Stat(cfgFile); os.IsNotExist(err) {
			defaultConfig := config.DefaultConfig()
			if err := os.WriteFile(cfgFile, []byte(defaultConfig), 0644); err != nil {
				logger.Error("failed to write config file", "path", cfgFile, "error", err)
				return fmt.Errorf("failed to write config file: %w", err)
			}
			logger.Info("config file created", "path", cfgFile)
		} else {
			logger.Info("config file already exists, skipping creation", "path", cfgFile)
		}

		// Print summary
		fmt.Println("\n✓ Project initialised successfully")
		fmt.Printf("  Config file: %s\n", cfgFile)
		fmt.Printf("  Workflows:  %s\n", config.Get().Paths.Workflows)
		fmt.Printf("  Logs:       %s\n", config.Get().Paths.Logs)
		fmt.Printf("  Database:   %s\n", dbPath)
		fmt.Println("\nConfigure paths via environment variables or config file.")

		logger.Info("project initialised",
			"config_file", cfgFile,
			"workflows_dir", config.Get().Paths.Workflows,
			"logs_dir", config.Get().Paths.Logs,
			"database", dbPath,
		)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
