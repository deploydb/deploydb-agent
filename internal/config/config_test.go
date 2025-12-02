package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Create a temporary config file
	content := `
control_plane_url: "wss://api.deploydb.com/agent/ws"
token: "ddb_test_token_12345678901234567890"
postgres:
  host: "localhost"
  port: 5432
  user: "deploydb_agent"
  password: "secret"
  database: "testdb"
metrics_interval: 60s
log_level: debug
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify loaded values
	if cfg.ControlPlaneURL != "wss://api.deploydb.com/agent/ws" {
		t.Errorf("ControlPlaneURL = %v, want %v", cfg.ControlPlaneURL, "wss://api.deploydb.com/agent/ws")
	}

	if cfg.Token != "ddb_test_token_12345678901234567890" {
		t.Errorf("Token = %v, want %v", cfg.Token, "ddb_test_token_12345678901234567890")
	}

	if cfg.Postgres.Host != "localhost" {
		t.Errorf("Postgres.Host = %v, want %v", cfg.Postgres.Host, "localhost")
	}

	if cfg.Postgres.Port != 5432 {
		t.Errorf("Postgres.Port = %v, want %v", cfg.Postgres.Port, 5432)
	}

	if cfg.MetricsInterval != 60*time.Second {
		t.Errorf("MetricsInterval = %v, want %v", cfg.MetricsInterval, 60*time.Second)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, "debug")
	}
}

func TestLoadDefaults(t *testing.T) {
	// Minimal config with only required fields
	content := `
control_plane_url: "wss://api.deploydb.com/agent/ws"
token: "ddb_test_token_12345678901234567890"
postgres:
  user: "deploydb_agent"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify defaults are applied
	if cfg.MetricsInterval != 30*time.Second {
		t.Errorf("MetricsInterval default = %v, want %v", cfg.MetricsInterval, 30*time.Second)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default = %v, want %v", cfg.LogLevel, "info")
	}

	if cfg.Postgres.Host != "localhost" {
		t.Errorf("Postgres.Host default = %v, want %v", cfg.Postgres.Host, "localhost")
	}

	if cfg.Postgres.Port != 5432 {
		t.Errorf("Postgres.Port default = %v, want %v", cfg.Postgres.Port, 5432)
	}

	if cfg.Postgres.SSLMode != "prefer" {
		t.Errorf("Postgres.SSLMode default = %v, want %v", cfg.Postgres.SSLMode, "prefer")
	}

	if cfg.ReconnectBackoff.InitialInterval != 1*time.Second {
		t.Errorf("ReconnectBackoff.InitialInterval default = %v, want %v", cfg.ReconnectBackoff.InitialInterval, 1*time.Second)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name: "valid config",
			config: `
control_plane_url: "wss://api.deploydb.com/agent/ws"
token: "ddb_test"
postgres:
  user: "agent"
`,
			wantErr: false,
		},
		{
			name: "missing control_plane_url",
			config: `
token: "ddb_test"
postgres:
  user: "agent"
`,
			wantErr: true,
		},
		{
			name: "missing token",
			config: `
control_plane_url: "wss://api.deploydb.com/agent/ws"
postgres:
  user: "agent"
`,
			wantErr: true,
		},
		{
			name: "missing postgres user",
			config: `
control_plane_url: "wss://api.deploydb.com/agent/ws"
token: "ddb_test"
`,
			wantErr: true,
		},
		{
			name: "metrics interval too short",
			config: `
control_plane_url: "wss://api.deploydb.com/agent/ws"
token: "ddb_test"
postgres:
  user: "agent"
metrics_interval: 5s
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(tt.config), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			_, err := Load(configPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPostgresDSN(t *testing.T) {
	cfg := &Config{
		Postgres: PostgresConfig{
			Host:     "db.example.com",
			Port:     5433,
			User:     "myuser",
			Password: "mypass",
			Database: "mydb",
			SSLMode:  "require",
		},
	}

	dsn := cfg.PostgresDSN()
	expected := "host=db.example.com port=5433 user=myuser dbname=mydb sslmode=require password=mypass"

	if dsn != expected {
		t.Errorf("PostgresDSN() = %v, want %v", dsn, expected)
	}
}
