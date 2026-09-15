package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/fenrisis/logpipe/internal/logger"
	"github.com/fenrisis/logpipe/internal/protocol"
)

// API request/response types
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	ID     int             `json:"id"`
}

type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	ID     int             `json:"id"`
}

type API struct {
	listener   net.Listener
	storage    *Storage
	socketPath string

	wg          sync.WaitGroup
	shutdown    chan struct{}
	stopOnce    sync.Once
	connMu      sync.Mutex
	stopping    bool
	activeConns map[net.Conn]struct{}
}

func NewAPI(dataDir string, storage *Storage) *API {
	return &API{
		socketPath:  filepath.Join(dataDir, "logpipe.sock"),
		storage:     storage,
		shutdown:    make(chan struct{}),
		activeConns: make(map[net.Conn]struct{}),
	}
}

func (a *API) Start() error {
	// Remove existing socket file
	os.Remove(a.socketPath)

	listener, err := net.Listen("unix", a.socketPath)
	if err != nil {
		return fmt.Errorf("failed to create unix socket: %w", err)
	}
	a.listener = listener

	// Restrict socket to owner only (security)
	os.Chmod(a.socketPath, 0600)

	a.wg.Add(1)
	go a.acceptLoop()
	return nil
}

func (a *API) acceptLoop() {
	defer logger.RecoverAndLog("api.acceptLoop")
	defer a.wg.Done()

	for {
		conn, err := a.listener.Accept()
		if err != nil {
			select {
			case <-a.shutdown:
				return
			default:
				logger.Error("unix socket accept error", "error", err)
				continue
			}
		}

		if !a.registerConn(conn) {
			conn.Close()
			return
		}
		go a.handleConn(conn)
	}
}

func (a *API) registerConn(conn net.Conn) bool {
	a.connMu.Lock()
	defer a.connMu.Unlock()

	if a.stopping {
		return false
	}

	a.activeConns[conn] = struct{}{}
	a.wg.Add(1)
	return true
}

func (a *API) unregisterConn(conn net.Conn) {
	a.connMu.Lock()
	delete(a.activeConns, conn)
	a.connMu.Unlock()
	a.wg.Done()
}

func (a *API) handleConn(conn net.Conn) {
	defer logger.RecoverAndLog("api.handleConn")
	defer a.unregisterConn(conn)
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024) // 10MB max for large responses
	encoder := json.NewEncoder(conn)

	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			encoder.Encode(Response{Error: "invalid request", ID: req.ID})
			continue
		}

		resp := a.handleRequest(req)
		encoder.Encode(resp)
	}
}

func (a *API) handleRequest(req Request) Response {
	switch req.Method {
	case "getNamespaces":
		return a.handleGetNamespaces(req)
	case "getLogs":
		return a.handleGetLogs(req)
	case "getStats":
		return a.handleGetStats(req)
	case "subscribe":
		return a.handleSubscribe(req)
	default:
		return Response{Error: "unknown method", ID: req.ID}
	}
}

func (a *API) handleGetNamespaces(req Request) Response {
	namespaces, err := a.storage.GetNamespaces()
	if err != nil {
		return Response{Error: err.Error(), ID: req.ID}
	}

	result, _ := json.Marshal(namespaces)
	return Response{Result: result, ID: req.ID}
}

func (a *API) handleGetLogs(req Request) Response {
	var filter protocol.Filter
	if req.Params != nil {
		json.Unmarshal(req.Params, &filter)
	}

	if filter.Limit == 0 {
		filter.Limit = 100
	}

	logs, err := a.storage.Query(filter)
	if err != nil {
		return Response{Error: err.Error(), ID: req.ID}
	}

	result, _ := json.Marshal(logs)
	return Response{Result: result, ID: req.ID}
}

func (a *API) handleGetStats(req Request) Response {
	stats, err := a.storage.GetStats()
	if err != nil {
		return Response{Error: err.Error(), ID: req.ID}
	}

	result, _ := json.Marshal(stats)
	return Response{Result: result, ID: req.ID}
}

func (a *API) handleSubscribe(req Request) Response {
	// For now, just return an acknowledgment
	// Real streaming would need different handling (kept connection open)
	return Response{Result: json.RawMessage(`{"status":"subscribed"}`), ID: req.ID}
}

func (a *API) SocketPath() string {
	return a.socketPath
}

func (a *API) Stop() {
	a.stopOnce.Do(func() {
		close(a.shutdown)
		if a.listener != nil {
			a.listener.Close()
		}

		a.connMu.Lock()
		a.stopping = true
		for conn := range a.activeConns {
			conn.Close()
		}
		a.connMu.Unlock()
	})
	a.wg.Wait()
	os.Remove(a.socketPath)
}
