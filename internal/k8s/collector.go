package k8s

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/logpipe/logpipe/internal/protocol"
	"github.com/logpipe/logpipe/internal/server"
)

// Collector streams logs from Kubernetes pods and stores them directly.
type Collector struct {
	namespaces []string
	podFilter  []string
	storage    *server.Storage

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Track running kubectl processes for cleanup
	procMu    sync.Mutex
	processes []*exec.Cmd
}

// Pod represents a Kubernetes pod.
type Pod struct {
	Name      string
	Service   string
	Namespace string
	Status    string
}

// NewCollector creates a new K8s log collector.
func NewCollector(storage *server.Storage, namespaces []string, podFilter []string) *Collector {
	ctx, cancel := context.WithCancel(context.Background())
	return &Collector{
		namespaces: namespaces,
		podFilter:  podFilter,
		storage:    storage,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start begins collecting logs from all specified namespaces.
func (c *Collector) Start() error {
	for _, ns := range c.namespaces {
		pods, err := c.getPods(ns)
		if err != nil {
			log.Printf("Warning: failed to get pods in %s: %v", ns, err)
			continue
		}

		for _, pod := range pods {
			if pod.Status != "Running" {
				continue
			}

			// Apply pod filter if specified
			if len(c.podFilter) > 0 {
				matched := false
				for _, filter := range c.podFilter {
					if strings.Contains(pod.Name, filter) || strings.Contains(pod.Service, filter) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}

			c.wg.Add(1)
			go c.streamPodLogs(pod)
		}
	}

	return nil
}

// Stop stops all log streaming.
func (c *Collector) Stop() {
	c.cancel()

	// Kill all running kubectl processes immediately
	c.procMu.Lock()
	for _, cmd := range c.processes {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}
	c.procMu.Unlock()

	c.wg.Wait()
}

// GetPods returns pods across all namespaces (for display).
func (c *Collector) GetPods() ([]Pod, error) {
	var allPods []Pod
	for _, ns := range c.namespaces {
		pods, err := c.getPods(ns)
		if err != nil {
			continue
		}
		allPods = append(allPods, pods...)
	}
	return allPods, nil
}

func (c *Collector) getPods(namespace string) ([]Pod, error) {
	cmd := exec.CommandContext(c.ctx, "kubectl", "get", "pods", "-n", namespace, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return nil, err
	}

	var pods []Pod
	for _, item := range result.Items {
		pods = append(pods, Pod{
			Name:      item.Metadata.Name,
			Service:   extractServiceName(item.Metadata.Name),
			Namespace: item.Metadata.Namespace,
			Status:    item.Status.Phase,
		})
	}

	return pods, nil
}

// Patterns to filter out (health checks, probes, etc.)
var skipPatterns = []string{
	"/health/live",
	"/health/ready",
	"/healthz",
	"/readyz",
	"/livez",
	"/health",
	"/ready",
	"/ping",
	"/metrics",
}

func shouldSkipLog(message string) bool {
	msgLower := strings.ToLower(message)
	for _, pattern := range skipPatterns {
		if strings.Contains(msgLower, pattern) {
			return true
		}
	}
	return false
}

func (c *Collector) streamPodLogs(pod Pod) {
	defer c.wg.Done()

	log.Printf("Streaming logs from %s/%s", pod.Namespace, pod.Name)

	cmd := exec.CommandContext(c.ctx, "kubectl", "logs", "-f", "--tail=100", "-n", pod.Namespace, pod.Name)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Failed to get stdout for %s: %v", pod.Name, err)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start kubectl logs for %s: %v", pod.Name, err)
		return
	}

	// Track process for cleanup
	c.procMu.Lock()
	c.processes = append(c.processes, cmd)
	c.procMu.Unlock()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		// Skip health checks and probes
		if shouldSkipLog(line) {
			continue
		}

		extraJSON, _ := json.Marshal(map[string]interface{}{"pod": pod.Name})
		entry := protocol.LogEntry{
			Timestamp: time.Now(),
			Namespace: pod.Namespace,
			Service:   pod.Service,
			Level:     parseLogLevel(line),
			Message:   line,
			Extra:     extraJSON,
		}

		if err := c.storage.Insert(entry); err != nil {
			log.Printf("Failed to store log: %v", err)
		}
	}

	cmd.Wait()
}

// extractServiceName extracts service name from pod name.
// e.g., "gateway-7f8b9c6d5-x2j4k" -> "gateway"
func extractServiceName(podName string) string {
	parts := strings.Split(podName, "-")
	if len(parts) >= 3 {
		// Check if last two parts look like K8s suffixes (random strings)
		last := parts[len(parts)-1]
		secondLast := parts[len(parts)-2]
		if len(last) <= 5 && len(secondLast) <= 10 {
			return strings.Join(parts[:len(parts)-2], "-")
		}
	}
	return podName
}

var levelPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\[?(DEBUG|INFO|WARN|WARNING|ERROR|ERR|CRITICAL|FATAL)\]?[:\s]`),
	regexp.MustCompile(`(?i)"level"\s*:\s*"?(DEBUG|INFO|WARN|WARNING|ERROR|ERR|CRITICAL|FATAL)"?`),
	regexp.MustCompile(`(?i)\b(DEBUG|INFO|WARN|WARNING|ERROR|ERR|CRITICAL|FATAL)\b.*?[:\-\|]`),
}

func parseLogLevel(message string) protocol.LogLevel {
	for _, pattern := range levelPatterns {
		if match := pattern.FindStringSubmatch(message); len(match) > 1 {
			level := strings.ToUpper(match[1])
			switch level {
			case "WARNING", "WARN":
				return protocol.LevelWarn
			case "ERR", "CRITICAL", "FATAL", "ERROR":
				return protocol.LevelError
			case "DEBUG":
				return protocol.LevelDebug
			case "INFO":
				return protocol.LevelInfo
			}
		}
	}

	// Check for error indicators
	msgLower := strings.ToLower(message)
	if strings.Contains(msgLower, "exception") ||
		strings.Contains(msgLower, "traceback") ||
		strings.Contains(msgLower, "panic:") ||
		strings.Contains(msgLower, "fatal:") {
		return protocol.LevelError
	}

	return protocol.LevelInfo
}
