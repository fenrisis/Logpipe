package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/fenrisis/logpipe/internal/logger"
	"github.com/fenrisis/logpipe/internal/protocol"
)

type Collector struct {
	listener net.Listener
	storage  *Storage
	host     string
	port     int

	wg          sync.WaitGroup
	shutdown    chan struct{}
	stopOnce    sync.Once
	connMu      sync.Mutex
	stopping    bool
	activeConns map[net.Conn]struct{}
}

func NewCollector(host string, port int, storage *Storage) *Collector {
	return &Collector{
		host:        host,
		port:        port,
		storage:     storage,
		shutdown:    make(chan struct{}),
		activeConns: make(map[net.Conn]struct{}),
	}
}

func (c *Collector) Start() error {
	addr := net.JoinHostPort(c.host, fmt.Sprintf("%d", c.port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start TCP listener: %w", err)
	}
	c.listener = listener

	c.wg.Add(1)
	go c.acceptLoop()
	return nil
}

func (c *Collector) acceptLoop() {
	defer logger.RecoverAndLog("collector.acceptLoop")
	defer c.wg.Done()

	for {
		conn, err := c.listener.Accept()
		if err != nil {
			select {
			case <-c.shutdown:
				return
			default:
				logger.Error("accept failed", "error", err)
				continue
			}
		}

		if !c.registerConn(conn) {
			conn.Close()
			return
		}
		go c.handleConn(conn)
	}
}

func (c *Collector) registerConn(conn net.Conn) bool {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.stopping {
		return false
	}

	c.activeConns[conn] = struct{}{}
	c.wg.Add(1)
	return true
}

func (c *Collector) unregisterConn(conn net.Conn) {
	c.connMu.Lock()
	delete(c.activeConns, conn)
	c.connMu.Unlock()
	c.wg.Done()
}

func (c *Collector) handleConn(conn net.Conn) {
	defer logger.RecoverAndLog("collector.handleConn")
	defer c.unregisterConn(conn)
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	logger.Debug("new connection", "remote", remoteAddr)

	scanner := bufio.NewScanner(conn)
	// Increase buffer size for large log messages
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-c.shutdown:
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry protocol.LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			logger.Warn("invalid JSON received", "remote", remoteAddr, "error", err)
			continue
		}

		// Set timestamp if not provided
		if entry.Timestamp.IsZero() {
			entry.Timestamp = time.Now()
		}

		// Validate required fields
		if entry.Namespace == "" {
			entry.Namespace = "default"
		}
		if entry.Service == "" {
			entry.Service = "unknown"
		}
		if entry.Level == "" {
			entry.Level = protocol.LevelInfo
		}

		if err := c.storage.Insert(entry); err != nil {
			logger.Error("failed to store log", "error", err)
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		logger.Error("scanner error", "remote", remoteAddr, "error", err)
	}

	logger.Debug("connection closed", "remote", remoteAddr)
}

func (c *Collector) Stop() {
	c.stopOnce.Do(func() {
		close(c.shutdown)
		if c.listener != nil {
			c.listener.Close()
		}

		c.connMu.Lock()
		c.stopping = true
		for conn := range c.activeConns {
			conn.Close()
		}
		c.connMu.Unlock()
	})
	c.wg.Wait()
}
