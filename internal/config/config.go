package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the logpipe configuration
type Config struct {
	Server ServerConfig `yaml:"server"`
	UI     UIConfig     `yaml:"ui"`

	// Resolved paths (not serialized)
	configDir string `yaml:"-"`
}

type ServerConfig struct {
	Port      int    `yaml:"port"`
	DataDir   string `yaml:"data_dir"`
	Retention string `yaml:"retention"`
	// Cleanup interval in hours
	CleanupInterval int `yaml:"cleanup_interval"`
}

type UIConfig struct {
	Theme      string `yaml:"theme"`      // dark, light
	Timestamps string `yaml:"timestamps"` // relative, absolute
	PageSize   int    `yaml:"page_size"`  // logs per page
}

// XDG directory helpers

// xdgConfigHome returns $XDG_CONFIG_HOME or ~/.config
func xdgConfigHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

// xdgDataHome returns $XDG_DATA_HOME or ~/.local/share
func xdgDataHome() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

// XDGStateHome returns $XDG_STATE_HOME or ~/.local/state
func XDGStateHome() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state")
}

// Default configuration using XDG Base Directory paths
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Port:            5555,
			DataDir:         filepath.Join(xdgDataHome(), "logpipe"),
			Retention:       "7d",
			CleanupInterval: 1, // 1 hour
		},
		UI: UIConfig{
			Theme:      "dark",
			Timestamps: "relative",
			PageSize:   100,
		},
		configDir: filepath.Join(xdgConfigHome(), "logpipe"),
	}
}

// Load loads config from file, falling back to defaults
func Load() (*Config, error) {
	cfg := Default()

	configPath := cfg.ConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Try legacy path ~/.logpipe/config.yaml
			home, _ := os.UserHomeDir()
			legacyPath := filepath.Join(home, ".logpipe", "config.yaml")
			data, err = os.ReadFile(legacyPath)
			if err != nil {
				// No config found, use defaults
				return cfg, nil
			}
		} else {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Expand ~ in data_dir
	if cfg.Server.DataDir == "" {
		cfg.Server.DataDir = Default().Server.DataDir
	} else if cfg.Server.DataDir[0] == '~' {
		home, _ := os.UserHomeDir()
		cfg.Server.DataDir = filepath.Join(home, cfg.Server.DataDir[1:])
	}

	// Migrate legacy ~/.logpipe data_dir to XDG
	home, _ := os.UserHomeDir()
	if cfg.Server.DataDir == filepath.Join(home, ".logpipe") {
		cfg.Server.DataDir = Default().Server.DataDir
	}

	return cfg, nil
}

// ConfigPath returns the path to the config file
func (c *Config) ConfigPath() string {
	configDir := c.configDir
	if configDir == "" {
		configDir = filepath.Join(xdgConfigHome(), "logpipe")
	}
	return filepath.Join(configDir, "config.yaml")
}

// Save saves the config to file
func (c *Config) Save() error {
	configDir := filepath.Dir(c.ConfigPath())
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	configPath := c.ConfigPath()
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// RetentionDuration parses the retention string into a duration
func (c *Config) RetentionDuration() time.Duration {
	return ParseRetention(c.Server.Retention)
}

// ParseRetention parses retention string like "7d", "24h", "30d"
func ParseRetention(retention string) time.Duration {
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

// CreateDefaultConfig creates a default config file if it doesn't exist
func CreateDefaultConfig() error {
	cfg := Default()
	configPath := cfg.ConfigPath()

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		return nil // Already exists
	}

	// Create config directory
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	// Write default config with comments
	content := fmt.Sprintf(`# Logpipe Configuration
# Location: %s

server:
  # TCP port for log collection
  port: 5555

  # Data directory for SQLite database and socket (XDG_DATA_HOME)
  # data_dir: %s

  # Log retention period (e.g., 7d, 24h, 30d)
  retention: 7d

  # Cleanup interval in hours
  cleanup_interval: 1

ui:
  # Theme: dark or light
  theme: dark

  # Timestamp format: relative or absolute
  timestamps: relative

  # Number of logs per page
  page_size: 100
`, configPath, cfg.Server.DataDir)

	return os.WriteFile(configPath, []byte(content), 0644)
}
