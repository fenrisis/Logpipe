package k8s

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fenrisis/logpipe/internal/logger"
	"github.com/fenrisis/logpipe/internal/protocol"
	"github.com/fenrisis/logpipe/internal/server"
)

// Collector streams logs from Kubernetes pods and stores them directly.
type Collector struct {
	namespaces []string
	podFilter  []string
	storage    *server.Storage

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	discoveryInterval time.Duration
	podSource         func(namespace string) ([]Pod, error)
	streamLogs        func(context.Context, Pod, string, bool)

	streamMu      sync.Mutex
	activeStreams map[string]activeStream
	seenStreams   map[string]struct{}
	stopping      bool
	stopOnce      sync.Once
}

// Pod represents a Kubernetes pod.
type Pod struct {
	Name       string
	UID        string
	Service    string
	Namespace  string
	Status     string
	Containers []string
}

type activeStream struct {
	namespace string
	cancel    context.CancelFunc
}

const defaultDiscoveryInterval = 10 * time.Second

// NewCollector creates a new K8s log collector.
func NewCollector(storage *server.Storage, namespaces []string, podFilter []string) *Collector {
	ctx, cancel := context.WithCancel(context.Background())
	collector := &Collector{
		namespaces:        namespaces,
		podFilter:         podFilter,
		storage:           storage,
		ctx:               ctx,
		cancel:            cancel,
		discoveryInterval: defaultDiscoveryInterval,
		activeStreams:     make(map[string]activeStream),
		seenStreams:       make(map[string]struct{}),
	}
	collector.podSource = collector.getPods
	collector.streamLogs = collector.streamContainerLogs
	return collector
}

// Start begins collecting logs from all specified namespaces.
func (c *Collector) Start() error {
	c.refreshPods()

	c.wg.Add(1)
	go c.discoveryLoop()
	return nil
}

func (c *Collector) discoveryLoop() {
	defer logger.RecoverAndLog("k8s.discoveryLoop")
	defer c.wg.Done()

	ticker := time.NewTicker(c.discoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.refreshPods()
		}
	}
}

func (c *Collector) refreshPods() {
	for _, ns := range c.namespaces {
		pods, err := c.podSource(ns)
		if err != nil {
			logger.Warn("failed to get pods", "namespace", ns, "error", err)
			continue
		}
		c.reconcileNamespace(ns, pods)
	}
}

func (c *Collector) reconcileNamespace(namespace string, pods []Pod) {
	type target struct {
		pod       Pod
		container string
	}

	desired := make(map[string]target)
	for _, pod := range pods {
		if pod.Status != "Running" || !c.matchesPodFilter(pod) {
			continue
		}
		for _, container := range pod.Containers {
			key := podStreamKey(pod, container)
			desired[key] = target{pod: pod, container: container}
		}
	}

	c.streamMu.Lock()
	defer c.streamMu.Unlock()

	if c.stopping {
		return
	}

	for key, stream := range c.activeStreams {
		if stream.namespace == namespace {
			if _, ok := desired[key]; !ok {
				stream.cancel()
			}
		}
	}

	for key, target := range desired {
		if _, ok := c.activeStreams[key]; ok {
			continue
		}

		_, restart := c.seenStreams[key]
		c.seenStreams[key] = struct{}{}
		streamCtx, cancel := context.WithCancel(c.ctx)
		c.activeStreams[key] = activeStream{namespace: namespace, cancel: cancel}
		c.wg.Add(1)
		go c.runStream(key, target.pod, target.container, restart, streamCtx)
	}
}

func (c *Collector) matchesPodFilter(pod Pod) bool {
	if len(c.podFilter) == 0 {
		return true
	}
	for _, filter := range c.podFilter {
		if strings.Contains(pod.Name, filter) || strings.Contains(pod.Service, filter) {
			return true
		}
	}
	return false
}

func podStreamKey(pod Pod, container string) string {
	podIdentity := pod.UID
	if podIdentity == "" {
		podIdentity = pod.Name
	}
	return pod.Namespace + "/" + podIdentity + "/" + container
}

func (c *Collector) runStream(key string, pod Pod, container string, restart bool, ctx context.Context) {
	defer logger.RecoverAndLog("k8s.runStream")
	defer c.wg.Done()
	defer func() {
		c.streamMu.Lock()
		delete(c.activeStreams, key)
		c.streamMu.Unlock()
	}()

	c.streamLogs(ctx, pod, container, restart)
}

// Stop stops all log streaming.
func (c *Collector) Stop() {
	c.stopOnce.Do(func() {
		c.cancel()

		c.streamMu.Lock()
		c.stopping = true
		for _, stream := range c.activeStreams {
			stream.cancel()
		}
		c.streamMu.Unlock()
	})

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

	return parsePods(output)
}

func parsePods(output []byte) ([]Pod, error) {
	var result struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
				UID       string `json:"uid"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Name string `json:"name"`
				} `json:"containers"`
			} `json:"spec"`
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
		containers := make([]string, 0, len(item.Spec.Containers))
		for _, container := range item.Spec.Containers {
			containers = append(containers, container.Name)
		}
		pods = append(pods, Pod{
			Name:       item.Metadata.Name,
			UID:        item.Metadata.UID,
			Service:    extractServiceName(item.Metadata.Name),
			Namespace:  item.Metadata.Namespace,
			Status:     item.Status.Phase,
			Containers: containers,
		})
	}

	return pods, nil
}

// logBuffer accumulates multiline logs (stack traces) before storing
type logBuffer struct {
	entry     *protocol.LogEntry
	pod       Pod
	container string
	timer     *time.Timer
	mu        sync.Mutex
	storage   *server.Storage
	flushTime time.Duration
}

func newLogBuffer(storage *server.Storage, pod Pod, container string, flushTime time.Duration) *logBuffer {
	return &logBuffer{
		storage:   storage,
		pod:       pod,
		container: container,
		flushTime: flushTime,
	}
}

func (b *logBuffer) hasEntry() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.entry != nil
}

func (b *logBuffer) start(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	extraJSON, _ := json.Marshal(map[string]interface{}{
		"pod":       b.pod.Name,
		"container": b.container,
	})
	b.entry = &protocol.LogEntry{
		Timestamp: time.Now(),
		Namespace: b.pod.Namespace,
		Service:   b.pod.Service,
		Level:     parseLogLevel(line),
		Message:   line,
		Extra:     extraJSON,
	}
}

func (b *logBuffer) append(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entry != nil {
		b.entry.Message += "\n" + line
		// Upgrade level if continuation contains error indicators
		if b.entry.Level != protocol.LevelError {
			lineLevel := parseLogLevel(line)
			if lineLevel == protocol.LevelError {
				b.entry.Level = protocol.LevelError
			}
		}
	}
}

func (b *logBuffer) flush() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}

	if b.entry != nil {
		if err := b.storage.Insert(*b.entry); err != nil {
			logger.Error("failed to store log", "error", err)
		}
		b.entry = nil
	}
}

func (b *logBuffer) resetTimer() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(b.flushTime, func() {
		b.flush()
	})
}

// isContinuation detects if a line is a continuation of a multiline log (stack trace)
func isContinuation(line string) bool {
	if len(line) == 0 {
		return false
	}

	// Lines starting with whitespace are continuations
	if line[0] == ' ' || line[0] == '\t' {
		return true
	}

	// Known stack trace patterns
	continuationPrefixes := []string{
		"File \"",       // Python: File "path", line N
		"Traceback ",    // Python: Traceback (most recent call last):
		"goroutine ",    // Go: goroutine 1 [running]:
		"panic: ",       // Go panic
		"    at ",       // Java: at com.package.Class.method
		"Caused by: ",   // Java chained exceptions
		"Exception in ", // Java
		"... ",          // Java: ... 15 more
	}

	for _, prefix := range continuationPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}

	return false
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

func (c *Collector) streamContainerLogs(ctx context.Context, pod Pod, container string, restart bool) {
	logger.Info("streaming logs", "namespace", pod.Namespace, "pod", pod.Name, "container", container)

	cmd := exec.CommandContext(ctx, "kubectl", kubectlLogArgs(pod, container, restart)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("failed to get stdout", "pod", pod.Name, "container", container, "error", err)
		return
	}

	if err := cmd.Start(); err != nil {
		logger.Error("failed to start kubectl logs", "pod", pod.Name, "container", container, "error", err)
		return
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	// Buffer for multiline log aggregation (stack traces)
	buffer := newLogBuffer(c.storage, pod, container, 500*time.Millisecond)
	defer buffer.flush()

scanLoop:
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			break scanLoop
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

		// Check if this line is a continuation of previous (stack trace)
		if isContinuation(line) && buffer.hasEntry() {
			buffer.append(line)
		} else {
			// Flush previous entry and start new one
			buffer.flush()
			buffer.start(line)
		}
		buffer.resetTimer()
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		logger.Warn("log stream ended with read error", "pod", pod.Name, "container", container, "error", err)
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		logger.Warn("kubectl logs exited", "pod", pod.Name, "container", container, "error", err)
	}
}

func kubectlLogArgs(pod Pod, container string, restart bool) []string {
	tail := "100"
	if restart {
		tail = "0"
	}
	return []string{"logs", "-f", "--tail=" + tail, "-n", pod.Namespace, pod.Name, "-c", container}
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
