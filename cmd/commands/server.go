package commands

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/logpipe/logpipe/internal/config"
	"github.com/logpipe/logpipe/internal/logger"
	"github.com/logpipe/logpipe/internal/server"
	"github.com/spf13/cobra"
)

var (
	serverPort    int
	serverDataDir string
	serverDaemon  bool
	retention     string
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the logpipe server daemon",
	Long:  `Starts the log collection server that accepts logs via TCP and stores them in SQLite.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load config file
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v, using defaults\n", err)
			cfg = config.Default()
		}

		// Command line flags override config file
		if cmd.Flags().Changed("port") {
			cfg.Server.Port = serverPort
		}
		if cmd.Flags().Changed("data") {
			cfg.Server.DataDir = serverDataDir
		}
		if cmd.Flags().Changed("retention") {
			cfg.Server.Retention = retention
		}

		// Create data directory if it doesn't exist
		if err := os.MkdirAll(cfg.Server.DataDir, 0755); err != nil {
			return fmt.Errorf("failed to create data directory: %w", err)
		}

		// Create default config if it doesn't exist
		config.CreateDefaultConfig()

		srvCfg := server.Config{
			TCPPort:         cfg.Server.Port,
			DataDir:         cfg.Server.DataDir,
			Retention:       cfg.Server.Retention,
			CleanupInterval: cfg.Server.CleanupInterval,
		}

		srv, err := server.New(srvCfg)
		if err != nil {
			return fmt.Errorf("failed to create server: %w", err)
		}

		// Handle shutdown gracefully
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigCh
			logger.Info("shutdown signal received")
			fmt.Println("\nShutting down...")
			srv.Shutdown()
		}()

		logger.Info("server starting",
			"port", cfg.Server.Port,
			"data_dir", cfg.Server.DataDir,
			"retention", cfg.Server.Retention,
		)

		fmt.Printf("Logpipe server starting\n")
		fmt.Printf("  TCP port:    %d\n", cfg.Server.Port)
		fmt.Printf("  Data dir:    %s\n", cfg.Server.DataDir)
		fmt.Printf("  Socket:      %s\n", filepath.Join(cfg.Server.DataDir, "logpipe.sock"))
		fmt.Printf("  Retention:   %s\n", cfg.Server.Retention)
		fmt.Printf("  Config:      %s\n", cfg.ConfigPath())
		fmt.Println()

		return srv.Run()
	},
}

func init() {
	serverCmd.Flags().IntVarP(&serverPort, "port", "p", 5555, "TCP port for log collection")
	serverCmd.Flags().StringVarP(&serverDataDir, "data", "d", "", "Data directory (default: ~/.logpipe)")
	serverCmd.Flags().StringVar(&retention, "retention", "7d", "Log retention period")
	serverCmd.Flags().BoolVar(&serverDaemon, "daemon", false, "Run as daemon (background)")
}
