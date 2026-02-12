package commands

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/logpipe/logpipe/internal/logger"
	"github.com/logpipe/logpipe/internal/tui"
	"github.com/spf13/cobra"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:   "logpipe",
	Short: "A terminal UI for viewing logs in real-time",
	Long: `Logpipe is a k9s-style terminal interface for logs.

Run 'logpipe server' to start the log collection daemon.
Run 'logpipe' (no args) to launch the interactive TUI.
Run 'logpipe logs -f' to tail logs in the terminal.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.Run()
	},
}

func Execute() {
	// Initialize logger
	cfg := logger.DefaultConfig()
	if verbose {
		cfg.Level = slog.LevelDebug
	}
	if err := logger.Init(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logger: %v\n", err)
	}
	defer logger.Close()

	logger.Info("logpipe starting", "version", "0.1.0", "verbose", verbose)

	if err := rootCmd.Execute(); err != nil {
		logger.Error("command failed", "error", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	logger.Info("logpipe shutdown complete")
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")

	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(k8sCmd)
}
