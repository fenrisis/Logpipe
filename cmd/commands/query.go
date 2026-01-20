package commands

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/logpipe/logpipe/internal/client"
	"github.com/logpipe/logpipe/internal/protocol"
	"github.com/spf13/cobra"
)

var (
	queryNamespace string
	queryService   string
	queryLevel     []string
	queryFrom      string
	queryTo        string
	queryLimit     int
	queryJSON      bool
)

var queryCmd = &cobra.Command{
	Use:   "query [search term]",
	Short: "Search logs by text or filters",
	Long:  `Query historical logs with full-text search and filters.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New()
		if err != nil {
			return err
		}
		defer c.Close()

		filter := protocol.Filter{
			Namespace: queryNamespace,
			Service:   queryService,
			Limit:     queryLimit,
		}

		// Search term
		if len(args) > 0 {
			filter.Search = strings.Join(args, " ")
		}

		// Convert level strings
		for _, l := range queryLevel {
			filter.Levels = append(filter.Levels, protocol.LogLevel(strings.ToUpper(l)))
		}

		// Parse time filters
		if queryFrom != "" {
			t, err := parseTime(queryFrom)
			if err != nil {
				return fmt.Errorf("invalid --from time: %w", err)
			}
			filter.From = t
		}

		if queryTo != "" {
			t, err := parseTime(queryTo)
			if err != nil {
				return fmt.Errorf("invalid --to time: %w", err)
			}
			filter.To = t
		}

		logs, err := c.GetLogs(filter)
		if err != nil {
			return fmt.Errorf("failed to query logs: %w", err)
		}

		if queryJSON {
			// JSON output for scripting
			for _, log := range logs {
				data, _ := json.Marshal(log)
				fmt.Println(string(data))
			}
		} else {
			// Pretty output
			fmt.Printf("Found %d logs\n\n", len(logs))
			for i := len(logs) - 1; i >= 0; i-- {
				printLog(logs[i])
			}
		}

		return nil
	},
}

func parseTime(s string) (time.Time, error) {
	now := time.Now()

	// Handle relative times like "1h ago", "30m ago"
	if strings.HasSuffix(s, " ago") {
		s = strings.TrimSuffix(s, " ago")
		d, err := time.ParseDuration(s)
		if err != nil {
			return time.Time{}, err
		}
		return now.Add(-d), nil
	}

	// Handle "now"
	if s == "now" {
		return now, nil
	}

	// Try various formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		t, err := time.Parse(format, s)
		if err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

func init() {
	queryCmd.Flags().StringVarP(&queryNamespace, "namespace", "n", "", "Filter by namespace")
	queryCmd.Flags().StringVarP(&queryService, "service", "s", "", "Filter by service")
	queryCmd.Flags().StringSliceVarP(&queryLevel, "level", "l", nil, "Filter by level")
	queryCmd.Flags().StringVar(&queryFrom, "from", "", "Start time (e.g., '1h ago', '2024-01-01')")
	queryCmd.Flags().StringVar(&queryTo, "to", "", "End time (e.g., 'now', '2024-01-02')")
	queryCmd.Flags().IntVar(&queryLimit, "limit", 100, "Maximum results")
	queryCmd.Flags().BoolVar(&queryJSON, "json", false, "Output as JSON")
}
