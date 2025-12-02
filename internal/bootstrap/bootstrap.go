package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// BootstrapConfig is the configuration received from the control plane.
type BootstrapConfig struct {
	PostgresVersion string `json:"postgres_version"`
	DeploymentType  string `json:"deployment_type"` // single, primary, replica
	Port            int    `json:"port"`
	MaxConnections  int    `json:"max_connections"`
	SharedBuffers   string `json:"shared_buffers"`
	// For replicas
	PrimaryHost string `json:"primary_host,omitempty"`
	PrimaryPort int    `json:"primary_port,omitempty"`
}

// BootstrapResponse is the response from the control plane's bootstrap config endpoint.
type BootstrapResponse struct {
	ServerID   string          `json:"server_id"`
	ServerName string          `json:"server_name"`
	Token      string          `json:"token"` // Server token for agent config
	Config     BootstrapConfig `json:"config"`
}

// ProgressUpdate represents a progress update to send to the control plane.
type ProgressUpdate struct {
	Step    string `json:"step"`
	Status  string `json:"status"` // running, completed, failed
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// Bootstrap orchestrates the PostgreSQL installation process.
type Bootstrap struct {
	Token          string
	ControlPlaneURL string
	Logger         *slog.Logger
	HTTPClient     *http.Client

	// Populated after fetching config
	Config   *BootstrapResponse
	OSInfo   *OSInfo
}

// NewBootstrap creates a new Bootstrap instance.
func NewBootstrap(token, controlPlaneURL string, logger *slog.Logger) *Bootstrap {
	return &Bootstrap{
		Token:          token,
		ControlPlaneURL: controlPlaneURL,
		Logger:         logger,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Run executes the full bootstrap process.
func (b *Bootstrap) Run(ctx context.Context) error {
	// Step 1: Detect OS
	b.Logger.Info("detecting operating system")
	osInfo, err := DetectOS()
	if err != nil {
		return b.fail(ctx, "detect_os", err)
	}
	b.OSInfo = osInfo
	b.Logger.Info("detected OS", "os", osInfo.String())

	if !osInfo.IsSupported() {
		return b.fail(ctx, "detect_os", fmt.Errorf("unsupported OS: %s. Supported: Ubuntu 20.04/22.04/24.04, Debian 11/12, RHEL/Rocky/Alma 8/9, Fedora, macOS", osInfo.String()))
	}

	// Step 2: Check if PostgreSQL is already installed
	if IsPostgresInstalled() {
		version, _ := GetInstalledPGVersion()
		return b.fail(ctx, "check_existing", fmt.Errorf("PostgreSQL %s is already installed. Use 'deploydb-agent run' to connect monitoring to existing installation", version))
	}

	// Step 3: Fetch configuration from control plane
	b.Logger.Info("fetching configuration from control plane")
	if err := b.reportProgress(ctx, "fetch_config", "running", "Fetching configuration..."); err != nil {
		b.Logger.Warn("failed to report progress", "error", err)
	}

	config, err := b.fetchConfig(ctx)
	if err != nil {
		return b.fail(ctx, "fetch_config", err)
	}
	b.Config = config
	b.Logger.Info("configuration received",
		"server_name", config.ServerName,
		"pg_version", config.Config.PostgresVersion,
		"deployment_type", config.Config.DeploymentType,
	)

	if err := b.reportProgress(ctx, "fetch_config", "completed", "Configuration received"); err != nil {
		b.Logger.Warn("failed to report progress", "error", err)
	}

	// Step 4: Install PostgreSQL
	b.Logger.Info("installing PostgreSQL")
	if err := b.reportProgress(ctx, "install_postgres", "running",
		fmt.Sprintf("Installing PostgreSQL %s...", config.Config.PostgresVersion)); err != nil {
		b.Logger.Warn("failed to report progress", "error", err)
	}

	installer := NewPostgresInstaller(osInfo, config.Config.PostgresVersion, b.Logger)
	if err := installer.Install(ctx); err != nil {
		return b.fail(ctx, "install_postgres", err)
	}

	if err := b.reportProgress(ctx, "install_postgres", "completed", "PostgreSQL installed"); err != nil {
		b.Logger.Warn("failed to report progress", "error", err)
	}

	// Step 5: Configure PostgreSQL
	b.Logger.Info("configuring PostgreSQL")
	if err := b.reportProgress(ctx, "configure_postgres", "running", "Configuring PostgreSQL..."); err != nil {
		b.Logger.Warn("failed to report progress", "error", err)
	}

	if err := b.configurePostgres(ctx); err != nil {
		return b.fail(ctx, "configure_postgres", err)
	}

	if err := b.reportProgress(ctx, "configure_postgres", "completed", "PostgreSQL configured"); err != nil {
		b.Logger.Warn("failed to report progress", "error", err)
	}

	// Step 6: Write agent configuration
	b.Logger.Info("writing agent configuration")
	if err := b.reportProgress(ctx, "write_config", "running", "Writing agent configuration..."); err != nil {
		b.Logger.Warn("failed to report progress", "error", err)
	}

	if err := b.writeAgentConfig(ctx); err != nil {
		return b.fail(ctx, "write_config", err)
	}

	if err := b.reportProgress(ctx, "write_config", "completed", "Agent configured"); err != nil {
		b.Logger.Warn("failed to report progress", "error", err)
	}

	// Step 7: Report completion
	if err := b.reportProgress(ctx, "complete", "completed", "Bootstrap completed successfully"); err != nil {
		b.Logger.Warn("failed to report completion", "error", err)
	}

	b.Logger.Info("bootstrap completed successfully",
		"server_name", config.ServerName,
		"pg_version", config.Config.PostgresVersion,
	)

	return nil
}

// fetchConfig retrieves the bootstrap configuration from the control plane.
func (b *Bootstrap) fetchConfig(ctx context.Context) (*BootstrapResponse, error) {
	url := fmt.Sprintf("%s/api/v1/bootstrap/config?token=%s", b.ControlPlaneURL, b.Token)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := b.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d: %s", resp.StatusCode, string(body))
	}

	var config BootstrapResponse
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &config, nil
}

// reportProgress sends a progress update to the control plane.
func (b *Bootstrap) reportProgress(ctx context.Context, step, status, message string) error {
	url := fmt.Sprintf("%s/api/v1/bootstrap/progress", b.ControlPlaneURL)

	update := ProgressUpdate{
		Step:    step,
		Status:  status,
		Message: message,
	}

	body, err := json.Marshal(update)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url,
		io.NopCloser(strings.NewReader(string(body))))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.Token)

	resp, err := b.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// fail reports a failure and returns an error.
func (b *Bootstrap) fail(ctx context.Context, step string, err error) error {
	b.Logger.Error("bootstrap failed", "step", step, "error", err)

	update := ProgressUpdate{
		Step:    step,
		Status:  "failed",
		Message: "Bootstrap failed",
		Error:   err.Error(),
	}

	body, _ := json.Marshal(update)
	url := fmt.Sprintf("%s/api/v1/bootstrap/progress", b.ControlPlaneURL)

	req, reqErr := http.NewRequestWithContext(ctx, "POST", url,
		io.NopCloser(strings.NewReader(string(body))))
	if reqErr == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+b.Token)
		resp, _ := b.HTTPClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	}

	return fmt.Errorf("%s: %w", step, err)
}

// configurePostgres applies the configuration settings to PostgreSQL.
func (b *Bootstrap) configurePostgres(ctx context.Context) error {
	// This would configure postgresql.conf with:
	// - port
	// - max_connections
	// - shared_buffers
	// - For replicas: primary_conninfo
	//
	// For now, we'll use default configuration.
	// Full implementation would modify postgresql.conf

	b.Logger.Info("PostgreSQL configuration applied (using defaults)")
	return nil
}

// writeAgentConfig writes the agent configuration file.
func (b *Bootstrap) writeAgentConfig(ctx context.Context) error {
	configDir := "/etc/deploydb"
	configFile := configDir + "/config.yaml"

	// Create directory
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Determine PostgreSQL connection details
	pgHost := "localhost"
	pgPort := b.Config.Config.Port
	if pgPort == 0 {
		pgPort = 5432
	}

	config := fmt.Sprintf(`# DeployDB Agent Configuration
# Generated by bootstrap on %s

control_plane:
  url: %s/cable
  token: %s

postgres:
  host: %s
  port: %d
  user: postgres
  database: postgres

metrics:
  interval: 30s

log_level: info
`,
		time.Now().Format(time.RFC3339),
		b.ControlPlaneURL,
		b.Config.Token,
		pgHost,
		pgPort,
	)

	if err := os.WriteFile(configFile, []byte(config), 0600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	b.Logger.Info("agent configuration written", "path", configFile)
	return nil
}
