package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/logpipe/logpipe/internal/client"
	"github.com/logpipe/logpipe/internal/protocol"
	"github.com/spf13/cobra"
)

var (
	logsFollow    bool
	logsNamespace string
	logsService   string
	logsLevel     []string
	logsLimit     int
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorGray   = "\033[90m"
	colorCyan   = "\033[36m"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail logs from the server",
	Long:  `Stream logs in real-time, similar to tail -f.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New()
		if err != nil {
			return err
		}
		defer c.Close()

		filter := protocol.Filter{
			Namespace: logsNamespace,
			Service:   logsService,
			Limit:     logsLimit,
		}

		// Convert level strings to LogLevel
		for _, l := range logsLevel {
			filter.Levels = append(filter.Levels, protocol.LogLevel(strings.ToUpper(l)))
		}

		logs, err := c.GetLogs(filter)
		if err != nil {
			return fmt.Errorf("failed to fetch logs: %w", err)
		}

		// Print logs in reverse order (oldest first)
		for i := len(logs) - 1; i >= 0; i-- {
			printLog(logs[i])
		}

		if logsFollow {
			fmt.Println(colorGray + "── streaming ──" + colorReset)
			// Poll for new logs
			lastID := int64(0)
			if len(logs) > 0 {
				lastID = logs[0].ID
			}

			for {
				time.Sleep(500 * time.Millisecond)

				filter.Limit = 50
				newLogs, err := c.GetLogs(filter)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error fetching logs: %v\n", err)
					continue
				}

				// Print new logs
				for i := len(newLogs) - 1; i >= 0; i-- {
					if newLogs[i].ID > lastID {
						printLog(newLogs[i])
						lastID = newLogs[i].ID
					}
				}
			}
		}

		return nil
	},
}

func printLog(entry protocol.LogEntry) {
	levelColor := colorGray
	switch entry.Level {
	case protocol.LevelError:
		levelColor = colorRed
	case protocol.LevelWarn:
		levelColor = colorYellow
	case protocol.LevelInfo:
		levelColor = colorBlue
	case protocol.LevelDebug:
		levelColor = colorGray
	}

	ts := entry.Timestamp.Format("15:04:05")
	level := fmt.Sprintf("%-5s", entry.Level)
	source := fmt.Sprintf("[%s/%s]", entry.Namespace, entry.Service)

	fmt.Printf("%s%s%s %s%s%s %s%-20s%s %s\n",
		colorGray, ts, colorReset,
		levelColor, level, colorReset,
		colorCyan, source, colorReset,
		entry.Message,
	)
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow logs in real-time")
	logsCmd.Flags().StringVarP(&logsNamespace, "namespace", "n", "", "Filter by namespace")
	logsCmd.Flags().StringVarP(&logsService, "service", "s", "", "Filter by service")
	logsCmd.Flags().StringSliceVarP(&logsLevel, "level", "l", nil, "Filter by level (error,warn,info,debug)")
	logsCmd.Flags().IntVar(&logsLimit, "limit", 100, "Number of historical logs to show")
}
