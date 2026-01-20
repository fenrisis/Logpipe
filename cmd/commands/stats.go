package commands

import (
	"fmt"

	"github.com/logpipe/logpipe/internal/client"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show server statistics",
	Long:  `Display statistics about namespaces, services, log counts, and storage usage.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New()
		if err != nil {
			return err
		}
		defer c.Close()

		stats, err := c.GetStats()
		if err != nil {
			return fmt.Errorf("failed to get stats: %w", err)
		}

		fmt.Println("Logpipe Server Statistics")
		fmt.Println("─────────────────────────")
		fmt.Printf("Namespaces:   %d\n", stats.Namespaces)
		fmt.Printf("Services:     %d\n", stats.Services)
		fmt.Printf("Total logs:   %d\n", stats.TotalLogs)
		fmt.Printf("Logs today:   %d\n", stats.TodayLogs)
		fmt.Printf("Errors today: %d\n", stats.TodayErrors)

		return nil
	},
}
