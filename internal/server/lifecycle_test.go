package server

import (
	"net"
	"os"
	"testing"
	"time"
)

func TestCollectorBindsToLoopback(t *testing.T) {
	storage, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}
	defer storage.Close()

	collector := NewCollector("127.0.0.1", 0, storage)
	if err := collector.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer collector.Stop()

	addr, ok := collector.listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address type = %T, want *net.TCPAddr", collector.listener.Addr())
	}
	if !addr.IP.IsLoopback() {
		t.Fatalf("listener address = %s, want loopback address", addr.IP)
	}
}

func TestCollectorStopClosesIdleConnections(t *testing.T) {
	storage, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}
	defer storage.Close()

	collector := NewCollector("127.0.0.1", 0, storage)
	if err := collector.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	conn, err := net.Dial("tcp", collector.listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	waitForCondition(t, func() bool {
		collector.connMu.Lock()
		defer collector.connMu.Unlock()
		return len(collector.activeConns) == 1
	})

	assertStopsPromptly(t, collector.Stop)
	assertStopsPromptly(t, collector.Stop)
}

func TestAPIStopClosesIdleConnections(t *testing.T) {
	dataDir, err := os.MkdirTemp("/tmp", "logpipe-test-")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dataDir)
	})

	storage, err := NewStorage(dataDir)
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}
	defer storage.Close()

	api := NewAPI(dataDir, storage)
	if err := api.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	conn, err := net.Dial("unix", api.SocketPath())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	waitForCondition(t, func() bool {
		api.connMu.Lock()
		defer api.connMu.Unlock()
		return len(api.activeConns) == 1
	})

	assertStopsPromptly(t, api.Stop)
	assertStopsPromptly(t, api.Stop)
}

func waitForCondition(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("condition was not met before timeout")
}

func assertStopsPromptly(t *testing.T, stop func()) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return before timeout")
	}
}
