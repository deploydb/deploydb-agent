// Command deploydb-agent is the DeployDb monitoring and maintenance agent.
//
// It connects to the DeployDb control plane via WebSocket, reports PostgreSQL
// metrics, and executes signed maintenance commands.
//
// Usage:
//
//	deploydb-agent run --config=/etc/deploydb/config.yaml    # Run monitoring daemon
//	deploydb-agent bootstrap --token=xxx                      # Install PostgreSQL and configure agent
//	deploydb-agent version                                    # Show version information
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	_ "github.com/lib/pq"

	"github.com/deploydb/agent/internal/bootstrap"
	"github.com/deploydb/agent/internal/collector"
	"github.com/deploydb/agent/internal/config"
	"github.com/deploydb/agent/internal/connection"
)

// Build-time variables set by ldflags
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

const defaultControlPlaneURL = "https://app.deploydb.com"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Handle subcommands
	switch os.Args[1] {
	case "run":
		runCmd()
	case "bootstrap":
		bootstrapCmd()
	case "version", "--version", "-v":
		printVersion()
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

// runCmd implements the 'run' subcommand - starts the monitoring daemon.
func runCmd() {
	runFlags := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := runFlags.String("config", "/etc/deploydb/config.yaml", "Path to configuration file")

	if err := runFlags.Parse(os.Args[2:]); err != nil {
		os.Exit(1)
	}

	// Set up structured logging
	logger := setupLogger("info")
	slog.SetDefault(logger)

	logger.Info("starting deploydb-agent",
		"version", version,
		"commit", commit,
		"go_version", runtime.Version(),
		"os", runtime.GOOS,
		"arch", runtime.GOARCH,
	)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Update logger with configured log level
	logger = setupLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	logger.Info("configuration loaded",
		"control_plane_url", cfg.ControlPlaneURL,
		"postgres_host", cfg.Postgres.Host,
		"metrics_interval", cfg.MetricsInterval,
	)

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received shutdown signal", "signal", sig)
		cancel()
	}()

	// Run the agent
	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("agent error", "error", err)
		os.Exit(1)
	}

	logger.Info("agent shutdown complete")
}

// bootstrapCmd implements the 'bootstrap' subcommand - installs PostgreSQL.
func bootstrapCmd() {
	bootstrapFlags := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	token := bootstrapFlags.String("token", "", "Bootstrap token from DeployDB (required)")
	controlPlaneURL := bootstrapFlags.String("url", defaultControlPlaneURL, "Control plane URL")

	if err := bootstrapFlags.Parse(os.Args[2:]); err != nil {
		os.Exit(1)
	}

	if *token == "" {
		fmt.Fprintln(os.Stderr, "Error: --token is required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: deploydb-agent bootstrap --token=YOUR_TOKEN")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Get your token from the DeployDB dashboard:")
		fmt.Fprintln(os.Stderr, "  https://app.deploydb.com/deploy")
		os.Exit(1)
	}

	// Check for root privileges (required for package installation)
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Error: bootstrap requires root privileges")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Run with sudo:")
		fmt.Fprintln(os.Stderr, "  sudo deploydb-agent bootstrap --token=YOUR_TOKEN")
		os.Exit(1)
	}

	logger := setupLogger("info")
	slog.SetDefault(logger)

	logger.Info("starting bootstrap",
		"version", version,
		"os", runtime.GOOS,
		"arch", runtime.GOARCH,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received shutdown signal", "signal", sig)
		cancel()
	}()

	// Run bootstrap
	b := bootstrap.NewBootstrap(*token, *controlPlaneURL, logger)
	if err := b.Run(ctx); err != nil {
		logger.Error("bootstrap failed", "error", err)
		os.Exit(1)
	}

	// Print success message
	fmt.Println("")
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  ✓ PostgreSQL installed and configured successfully!         ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  1. Start the monitoring agent:")
	fmt.Println("     sudo systemctl start deploydb-agent")
	fmt.Println("")
	fmt.Println("  2. Check status:")
	fmt.Println("     sudo systemctl status deploydb-agent")
	fmt.Println("")
	fmt.Println("  3. View your server in DeployDB:")
	fmt.Println("     https://app.deploydb.com/servers")
	fmt.Println("")
}

func run(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	// Connect to PostgreSQL
	db, err := sql.Open("postgres", cfg.PostgresDSN())
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer db.Close()

	// Test the connection
	if err := db.PingContext(ctx); err != nil {
		logger.Warn("PostgreSQL not available, metrics will be limited", "error", err)
		db = nil
	} else {
		logger.Info("connected to PostgreSQL", "host", cfg.Postgres.Host)
	}

	// Get PostgreSQL version if connected
	var pgVersion string
	if db != nil {
		if err := db.QueryRowContext(ctx, "SELECT version()").Scan(&pgVersion); err == nil {
			logger.Info("PostgreSQL version", "version", pgVersion)
		}
	}

	// Create metrics collector
	metricsCollector := collector.New(collector.Config{
		DB:      db,
		DataDir: "/var/lib/postgresql", // Default PG data directory
	})

	// Create connection manager
	connConfig := connection.Config{
		URL:             cfg.ControlPlaneURL,
		Token:           cfg.Token,
		AgentVersion:    version,
		Hostname:        getHostname(),
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		PostgresVersion: pgVersion,
		InitialBackoff:  cfg.ReconnectBackoff.InitialInterval,
		MaxBackoff:      cfg.ReconnectBackoff.MaxInterval,
		BackoffFactor:   cfg.ReconnectBackoff.Multiplier,
		PingInterval:    30 * cfg.MetricsInterval / 100, // Ping at ~30% of metrics interval
	}

	manager := connection.NewManager(connConfig)
	manager.SetLogger(logger)

	// Handle state changes
	manager.OnStateChange(func(state connection.State) {
		logger.Info("connection state changed", "state", state.String())
	})

	// Handle commands
	manager.OnCommand(func(cmd connection.Command) {
		logger.Info("received command",
			"id", cmd.ID,
			"command", cmd.Command,
			"server_id", cmd.ServerID,
		)
		// TODO: Execute command and send result
		// For now, just acknowledge receipt
	})

	// Set up metrics handler
	manager.SetMetricsHandler(func() map[string]float64 {
		metrics, err := metricsCollector.Collect(ctx)
		if err != nil {
			logger.Error("failed to collect metrics", "error", err)
			return nil
		}
		logger.Debug("collected metrics", "count", len(metrics))
		return metrics
	})

	// Start connection manager
	logger.Info("starting connection manager")
	manager.Start(ctx)

	// Wait for shutdown signal
	<-ctx.Done()

	// Stop connection manager gracefully
	logger.Info("stopping connection manager")
	manager.Stop()

	return nil
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func setupLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})

	return slog.New(handler)
}

func printVersion() {
	fmt.Printf("deploydb-agent %s\n", version)
	fmt.Printf("  commit:     %s\n", commit)
	fmt.Printf("  built:      %s\n", buildTime)
	fmt.Printf("  go version: %s\n", runtime.Version())
	fmt.Printf("  platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

func printUsage() {
	fmt.Println("DeployDB Agent - PostgreSQL monitoring and deployment agent")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  deploydb-agent <command> [options]")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  run        Start the monitoring daemon (connects to existing PostgreSQL)")
	fmt.Println("  bootstrap  Install PostgreSQL and configure the agent")
	fmt.Println("  version    Show version information")
	fmt.Println("  help       Show this help message")
	fmt.Println("")
	fmt.Println("Run 'deploydb-agent <command> --help' for more information on a command.")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  # Start monitoring daemon")
	fmt.Println("  deploydb-agent run --config=/etc/deploydb/config.yaml")
	fmt.Println("")
	fmt.Println("  # Install PostgreSQL (requires root)")
	fmt.Println("  sudo deploydb-agent bootstrap --token=YOUR_TOKEN")
}
