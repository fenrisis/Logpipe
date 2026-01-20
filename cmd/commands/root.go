package commands

import (
	"fmt"
	"os"

	"github.com/logpipe/logpipe/internal/tui"
	"github.com/spf13/cobra"
)

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
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(k8sCmd)
}
