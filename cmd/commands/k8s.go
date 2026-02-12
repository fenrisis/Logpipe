package commands

import (
	"fmt"
	"os"
	"time"

	"github.com/logpipe/logpipe/internal/config"
	"github.com/logpipe/logpipe/internal/k8s"
	"github.com/logpipe/logpipe/internal/logger"
	"github.com/logpipe/logpipe/internal/server"
	"github.com/logpipe/logpipe/internal/tui"
	"github.com/spf13/cobra"
)

var (
	k8sNamespaces []string
	k8sPods       []string
	k8sList       bool
)

var k8sCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Stream logs from Kubernetes and view in TUI",
	Long: `Stream logs from Kubernetes pods using kubectl and view them in the interactive TUI.

This command combines:
  - Log collection from K8s pods (via kubectl logs)
  - Local storage (SQLite)
  - Interactive TUI viewer

All in one command - no separate server needed.

Examples:
  logpipe k8s -n payments                    # All pods in 'payments' namespace
  logpipe k8s -n payments -n users           # Multiple namespaces
  logpipe k8s -n payments -p gateway         # Only pods matching 'gateway'
  logpipe k8s --list -n payments             # List pods without starting TUI`,
	RunE: runK8s,
}

func init() {
	k8sCmd.Flags().StringArrayVarP(&k8sNamespaces, "namespace", "n", nil, "Kubernetes namespace(s) to stream logs from")
	k8sCmd.Flags().StringArrayVarP(&k8sPods, "pod", "p", nil, "Filter pods by name (substring match)")
	k8sCmd.Flags().BoolVar(&k8sList, "list", false, "List pods in namespaces and exit")
}

func runK8s(cmd *cobra.Command, args []string) error {
	if len(k8sNamespaces) == 0 {
		return fmt.Errorf("specify at least one namespace with -n")
	}

	// Load or create config
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}

	// Create data directory
	if err := os.MkdirAll(cfg.Server.DataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Create storage directly (for list mode)
	storage, err := server.NewStorage(cfg.Server.DataDir)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	// Create K8s collector
	collector := k8s.NewCollector(storage, k8sNamespaces, k8sPods)

	// List mode - just show pods and exit
	if k8sList {
		return listPods(collector)
	}

	// Start full server in background (needed for TUI to connect via socket)
	srvCfg := server.Config{
		TCPPort:         cfg.Server.Port,
		DataDir:         cfg.Server.DataDir,
		Retention:       cfg.Server.Retention,
		CleanupInterval: cfg.Server.CleanupInterval,
	}

	// Close storage as server will create its own
	storage.Close()

	srv, err := server.New(srvCfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Start server in background
	go func() {
		srv.Run()
	}()

	// Wait for server API to be ready (check socket exists and is responding)
	socketPath := cfg.Server.DataDir + "/logpipe.sock"
	fmt.Printf("Logpipe K8s Mode\n")
	fmt.Printf("────────────────────────────────────────\n")
	fmt.Printf("Namespaces: %v\n", k8sNamespaces)
	if len(k8sPods) > 0 {
		fmt.Printf("Pod filter: %v\n", k8sPods)
	}
	fmt.Println()
	fmt.Print("Starting server...")

	// Wait for socket file to exist
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Give server more time to fully initialize
	time.Sleep(500 * time.Millisecond)
	fmt.Println(" done")

	// Create K8s collector with server's storage
	k8sCollector := k8s.NewCollector(srv.Storage(), k8sNamespaces, k8sPods)

	// Start K8s log streaming
	fmt.Print("Starting log collection...")
	if err := k8sCollector.Start(); err != nil {
		srv.Shutdown()
		return fmt.Errorf("failed to start K8s collector: %w", err)
	}
	fmt.Println(" done")

	// Wait for some logs to be collected
	fmt.Print("Collecting initial logs...")
	time.Sleep(2 * time.Second)
	fmt.Println(" done")

	fmt.Println()
	fmt.Println("Launching TUI... (press q to quit)")

	logger.Info("entering TUI mode")

	// Run TUI (blocking) - cleanup happens after TUI exits
	err = tui.Run()

	if err != nil {
		logger.Error("TUI error", "error", err)
	}

	// Cleanup
	k8sCollector.Stop()
	srv.Shutdown()

	return err
}

func listPods(collector *k8s.Collector) error {
	pods, err := collector.GetPods()
	if err != nil {
		return err
	}

	if len(pods) == 0 {
		fmt.Println("No pods found in specified namespaces")
		return nil
	}

	currentNs := ""
	for _, pod := range pods {
		if pod.Namespace != currentNs {
			if currentNs != "" {
				fmt.Println()
			}
			fmt.Printf("%s:\n", pod.Namespace)
			currentNs = pod.Namespace
		}

		status := "○"
		if pod.Status == "Running" {
			status = "●"
		}
		fmt.Printf("  %s %s (%s)\n", status, pod.Name, pod.Service)
	}

	return nil
}
