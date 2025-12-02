// Package config handles agent configuration loading and validation.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the agent configuration.
type Config struct {
	ControlPlaneURL string         `yaml:"control_plane_url"`
	Token           string         `yaml:"token"`
	Postgres        PostgresConfig `yaml:"postgres"`

	// Optional fields with defaults
	MetricsInterval  time.Duration `yaml:"metrics_interval"`
	ReconnectBackoff BackoffConfig `yaml:"reconnect_backoff"`
	LogLevel         string        `yaml:"log_level"`

	// Set by control plane during connection
	ServerID         string `yaml:"-"`
	SigningPublicKey []byte `yaml:"-"`
	CommandsEnabled  bool   `yaml:"-"`
}

// PostgresConfig holds PostgreSQL connection settings.
type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	SSLMode  string `yaml:"sslmode"`
}

// BackoffConfig controls reconnection backoff behavior.
type BackoffConfig struct {
	InitialInterval time.Duration `yaml:"initial_interval"`
	MaxInterval     time.Duration `yaml:"max_interval"`
	Multiplier      float64       `yaml:"multiplier"`
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.setDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// setDefaults applies default values to unset fields.
func (c *Config) setDefaults() {
	if c.MetricsInterval == 0 {
		c.MetricsInterval = 30 * time.Second
	}

	if c.ReconnectBackoff.InitialInterval == 0 {
		c.ReconnectBackoff.InitialInterval = 1 * time.Second
	}
	if c.ReconnectBackoff.MaxInterval == 0 {
		c.ReconnectBackoff.MaxInterval = 5 * time.Minute
	}
	if c.ReconnectBackoff.Multiplier == 0 {
		c.ReconnectBackoff.Multiplier = 2.0
	}

	if c.LogLevel == "" {
		c.LogLevel = "info"
	}

	if c.Postgres.Host == "" {
		c.Postgres.Host = "localhost"
	}
	if c.Postgres.Port == 0 {
		c.Postgres.Port = 5432
	}
	if c.Postgres.SSLMode == "" {
		c.Postgres.SSLMode = "prefer"
	}
}

// Validate checks that required configuration is present and valid.
func (c *Config) Validate() error {
	if c.ControlPlaneURL == "" {
		return fmt.Errorf("control_plane_url is required")
	}

	if c.Token == "" {
		return fmt.Errorf("token is required")
	}

	if c.Postgres.User == "" {
		return fmt.Errorf("postgres.user is required")
	}

	if c.MetricsInterval < 10*time.Second {
		return fmt.Errorf("metrics_interval must be at least 10 seconds")
	}

	return nil
}

// PostgresDSN returns a connection string for the PostgreSQL database.
func (c *Config) PostgresDSN() string {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s dbname=%s sslmode=%s",
		c.Postgres.Host,
		c.Postgres.Port,
		c.Postgres.User,
		c.Postgres.Database,
		c.Postgres.SSLMode,
	)

	if c.Postgres.Password != "" {
		dsn += fmt.Sprintf(" password=%s", c.Postgres.Password)
	}

	return dsn
}
