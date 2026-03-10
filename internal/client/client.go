package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/logpipe/logpipe/internal/protocol"
)

type Client struct {
	socketPath string
	conn       net.Conn
	encoder    *json.Encoder
	decoder    *bufio.Scanner
	requestID  atomic.Int64
	mu         sync.Mutex // Protect concurrent access
}

type Request struct {
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
	ID     int64       `json:"id"`
}

type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	ID     int64           `json:"id"`
}

func New() (*Client, error) {
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dataDir = filepath.Join(home, ".local", "share")
	}

	socketPath := filepath.Join(dataDir, "logpipe", "logpipe.sock")

	// Retry connection a few times
	var client *Client
	var lastErr error
	for i := 0; i < 5; i++ {
		client, lastErr = NewWithSocket(socketPath)
		if lastErr == nil {
			return client, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, lastErr
}

func NewWithSocket(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to logpipe server: %w (is 'logpipe server' running?)", err)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024) // 10MB max for large log responses

	return &Client{
		socketPath: socketPath,
		conn:       conn,
		encoder:    json.NewEncoder(conn),
		decoder:    scanner,
	}, nil
}

func (c *Client) call(method string, params interface{}, result interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.requestID.Add(1)

	req := Request{
		Method: method,
		Params: params,
		ID:     id,
	}

	if err := c.encoder.Encode(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	if !c.decoder.Scan() {
		if err := c.decoder.Err(); err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}
		return fmt.Errorf("connection closed")
	}

	var resp Response
	if err := json.Unmarshal(c.decoder.Bytes(), &resp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("server error: %s", resp.Error)
	}

	if result != nil && resp.Result != nil {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("failed to parse result: %w", err)
		}
	}

	return nil
}

func (c *Client) GetNamespaces() ([]protocol.Namespace, error) {
	var namespaces []protocol.Namespace
	err := c.call("getNamespaces", nil, &namespaces)
	return namespaces, err
}

func (c *Client) GetLogs(filter protocol.Filter) ([]protocol.LogEntry, error) {
	var logs []protocol.LogEntry
	err := c.call("getLogs", filter, &logs)
	return logs, err
}

func (c *Client) GetStats() (protocol.Stats, error) {
	var stats protocol.Stats
	err := c.call("getStats", nil, &stats)
	return stats, err
}

func (c *Client) Close() error {
	return c.conn.Close()
}
