package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/logpipe/logpipe/internal/logger"
	"github.com/logpipe/logpipe/internal/protocol"
)

type Collector struct {
	listener net.Listener
	storage  *Storage
	port     int

	wg       sync.WaitGroup
	shutdown chan struct{}
}

func NewCollector(port int, storage *Storage) *Collector {
	return &Collector{
		port:     port,
		storage:  storage,
		shutdown: make(chan struct{}),
	}
}

func (c *Collector) Start() error {
	addr := fmt.Sprintf(":%d", c.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start TCP listener: %w", err)
	}
	c.listener = listener

	go c.acceptLoop()
	return nil
}

func (c *Collector) acceptLoop() {
	defer logger.RecoverAndLog("collector.acceptLoop")

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

		c.wg.Add(1)
		go c.handleConn(conn)
	}
}

func (c *Collector) handleConn(conn net.Conn) {
	defer logger.RecoverAndLog("collector.handleConn")
	defer c.wg.Done()
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
	close(c.shutdown)
	if c.listener != nil {
		c.listener.Close()
	}
	c.wg.Wait()
}
