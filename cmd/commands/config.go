package commands

import (
	"fmt"

	"github.com/fenrisis/logpipe/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Logpipe configuration",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a default configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.CreateDefaultConfig(); err != nil {
			return fmt.Errorf("failed to create config: %w", err)
		}

		fmt.Printf("Configuration available at %s\n", config.Default().ConfigPath())
		return nil
	},
}

func init() {
	configCmd.AddCommand(configInitCmd)
}
