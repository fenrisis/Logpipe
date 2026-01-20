package server

import (
	"fmt"
	"log"
	"time"
)

type Config struct {
	TCPPort         int
	DataDir         string
	Retention       string
	CleanupInterval int // hours
}

type Server struct {
	config    Config
	storage   *Storage
	collector *Collector
	api       *API

	shutdown chan struct{}
}

func New(cfg Config) (*Server, error) {
	storage, err := NewStorage(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	collector := NewCollector(cfg.TCPPort, storage)
	api := NewAPI(cfg.DataDir, storage)

	return &Server{
		config:    cfg,
		storage:   storage,
		collector: collector,
		api:       api,
		shutdown:  make(chan struct{}),
	}, nil
}

func (s *Server) Run() error {
	// Start TCP collector
	if err := s.collector.Start(); err != nil {
		return fmt.Errorf("failed to start collector: %w", err)
	}
	log.Printf("TCP collector listening on :%d", s.config.TCPPort)

	// Start Unix socket API
	if err := s.api.Start(); err != nil {
		s.collector.Stop()
		return fmt.Errorf("failed to start API: %w", err)
	}
	log.Printf("Unix socket API at %s", s.api.SocketPath())

	// Start cleanup routine
	go s.cleanupLoop()

	// Wait for shutdown
	<-s.shutdown
	return nil
}

func (s *Server) cleanupLoop() {
	retention := parseRetention(s.config.Retention)

	// Calculate interval
	interval := time.Duration(s.config.CleanupInterval) * time.Hour
	if interval < time.Hour {
		interval = time.Hour
	}

	// Run initial cleanup
	s.runCleanup(retention)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.runCleanup(retention)
		case <-s.shutdown:
			return
		}
	}
}

func (s *Server) runCleanup(retention time.Duration) {
	deleted, err := s.storage.Cleanup(retention)
	if err != nil {
		log.Printf("Cleanup error: %v", err)
	} else if deleted > 0 {
		log.Printf("Cleanup: deleted %d old logs (retention: %s)", deleted, s.config.Retention)
	}
}

func (s *Server) Shutdown() {
	close(s.shutdown)
	s.collector.Stop()
	s.api.Stop()
	s.storage.Close()
}

// Storage returns the server's storage for direct access.
func (s *Server) Storage() *Storage {
	return s.storage
}

func parseRetention(retention string) time.Duration {
	// Parse retention string like "7d", "24h", "30d"
	var value int
	var unit string
	fmt.Sscanf(retention, "%d%s", &value, &unit)

	switch unit {
	case "d":
		return time.Duration(value) * 24 * time.Hour
	case "h":
		return time.Duration(value) * time.Hour
	case "m":
		return time.Duration(value) * time.Minute
	default:
		return 7 * 24 * time.Hour // Default: 7 days
	}
}
