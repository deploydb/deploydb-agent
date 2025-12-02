package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// SupportedPGVersions lists the PostgreSQL versions we support installing.
var SupportedPGVersions = []string{"17", "16", "15", "14", "13"}

// DefaultPGVersion is the default PostgreSQL version to install.
const DefaultPGVersion = "16"

// PostgresInstaller handles PostgreSQL installation for different platforms.
type PostgresInstaller struct {
	OSInfo    *OSInfo
	PGVersion string
	Port      int
	DataDir   string
	Logger    *slog.Logger
}

// NewPostgresInstaller creates a new PostgreSQL installer.
func NewPostgresInstaller(osInfo *OSInfo, pgVersion string, logger *slog.Logger) *PostgresInstaller {
	if pgVersion == "" {
		pgVersion = DefaultPGVersion
	}
	return &PostgresInstaller{
		OSInfo:    osInfo,
		PGVersion: pgVersion,
		Port:      5432,
		Logger:    logger,
	}
}

// Install installs PostgreSQL on the system.
func (p *PostgresInstaller) Install(ctx context.Context) error {
	p.Logger.Info("installing PostgreSQL",
		"version", p.PGVersion,
		"os", p.OSInfo.String(),
		"package_manager", p.OSInfo.PackageManager,
	)

	switch p.OSInfo.PackageManager {
	case "apt":
		return p.installWithApt(ctx)
	case "dnf":
		return p.installWithDnf(ctx)
	case "yum":
		return p.installWithYum(ctx)
	case "brew":
		return p.installWithBrew(ctx)
	default:
		return fmt.Errorf("unsupported package manager: %s", p.OSInfo.PackageManager)
	}
}

// installWithApt installs PostgreSQL on Debian/Ubuntu using PGDG apt repository.
func (p *PostgresInstaller) installWithApt(ctx context.Context) error {
	// Step 1: Install prerequisites
	p.Logger.Info("installing prerequisites")
	if err := p.runCommand(ctx, "apt-get", "update"); err != nil {
		return fmt.Errorf("apt-get update: %w", err)
	}
	if err := p.runCommand(ctx, "apt-get", "install", "-y",
		"curl", "ca-certificates", "gnupg"); err != nil {
		return fmt.Errorf("install prerequisites: %w", err)
	}

	// Step 2: Add PostgreSQL GPG key
	p.Logger.Info("adding PostgreSQL repository GPG key")
	if err := p.runCommand(ctx, "sh", "-c",
		"curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor -o /usr/share/keyrings/postgresql-keyring.gpg"); err != nil {
		return fmt.Errorf("add GPG key: %w", err)
	}

	// Step 3: Add PGDG repository
	p.Logger.Info("adding PGDG apt repository")
	codename := p.OSInfo.VersionCodename
	if codename == "" {
		// Fallback for systems without codename
		switch p.OSInfo.Distro {
		case "ubuntu":
			codename = SupportedUbuntuVersions[p.OSInfo.Version]
		case "debian":
			majorVersion := strings.Split(p.OSInfo.Version, ".")[0]
			codename = SupportedDebianVersions[majorVersion]
		}
	}

	repoLine := fmt.Sprintf("deb [signed-by=/usr/share/keyrings/postgresql-keyring.gpg] https://apt.postgresql.org/pub/repos/apt %s-pgdg main", codename)
	if err := p.runCommand(ctx, "sh", "-c",
		fmt.Sprintf("echo '%s' > /etc/apt/sources.list.d/pgdg.list", repoLine)); err != nil {
		return fmt.Errorf("add repository: %w", err)
	}

	// Step 4: Update and install PostgreSQL
	p.Logger.Info("installing PostgreSQL", "version", p.PGVersion)
	if err := p.runCommand(ctx, "apt-get", "update"); err != nil {
		return fmt.Errorf("apt-get update: %w", err)
	}
	if err := p.runCommand(ctx, "apt-get", "install", "-y",
		fmt.Sprintf("postgresql-%s", p.PGVersion)); err != nil {
		return fmt.Errorf("install postgresql: %w", err)
	}

	p.Logger.Info("PostgreSQL installed successfully")
	return nil
}

// installWithDnf installs PostgreSQL on RHEL/Rocky/Alma/Fedora using PGDG yum repository.
func (p *PostgresInstaller) installWithDnf(ctx context.Context) error {
	majorVersion := strings.Split(p.OSInfo.Version, ".")[0]

	// Step 1: Install PGDG repository RPM
	p.Logger.Info("adding PGDG repository")
	var repoURL string
	if p.OSInfo.Distro == "fedora" {
		repoURL = fmt.Sprintf("https://download.postgresql.org/pub/repos/yum/reporpms/F-%s-x86_64/pgdg-fedora-repo-latest.noarch.rpm", majorVersion)
	} else {
		repoURL = fmt.Sprintf("https://download.postgresql.org/pub/repos/yum/reporpms/EL-%s-x86_64/pgdg-redhat-repo-latest.noarch.rpm", majorVersion)
	}

	if err := p.runCommand(ctx, "dnf", "install", "-y", repoURL); err != nil {
		return fmt.Errorf("install pgdg repo: %w", err)
	}

	// Step 2: Disable built-in PostgreSQL module (RHEL/CentOS)
	if p.OSInfo.Distro != "fedora" {
		p.Logger.Info("disabling built-in PostgreSQL module")
		// Ignore error - module may not exist
		_ = p.runCommand(ctx, "dnf", "-qy", "module", "disable", "postgresql")
	}

	// Step 3: Install PostgreSQL
	p.Logger.Info("installing PostgreSQL", "version", p.PGVersion)
	pkgName := fmt.Sprintf("postgresql%s-server", p.PGVersion)
	if err := p.runCommand(ctx, "dnf", "install", "-y", pkgName); err != nil {
		return fmt.Errorf("install postgresql: %w", err)
	}

	// Step 4: Initialize database
	p.Logger.Info("initializing PostgreSQL database")
	initCmd := fmt.Sprintf("/usr/pgsql-%s/bin/postgresql-%s-setup", p.PGVersion, p.PGVersion)
	if err := p.runCommand(ctx, initCmd, "initdb"); err != nil {
		return fmt.Errorf("initdb: %w", err)
	}

	// Step 5: Enable and start service
	p.Logger.Info("enabling PostgreSQL service")
	serviceName := fmt.Sprintf("postgresql-%s", p.PGVersion)
	if err := p.runCommand(ctx, "systemctl", "enable", serviceName); err != nil {
		return fmt.Errorf("enable service: %w", err)
	}
	if err := p.runCommand(ctx, "systemctl", "start", serviceName); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	p.Logger.Info("PostgreSQL installed successfully")
	return nil
}

// installWithYum installs PostgreSQL on older RHEL/CentOS using yum.
func (p *PostgresInstaller) installWithYum(ctx context.Context) error {
	// Same as dnf but with yum command
	majorVersion := strings.Split(p.OSInfo.Version, ".")[0]

	p.Logger.Info("adding PGDG repository")
	repoURL := fmt.Sprintf("https://download.postgresql.org/pub/repos/yum/reporpms/EL-%s-x86_64/pgdg-redhat-repo-latest.noarch.rpm", majorVersion)
	if err := p.runCommand(ctx, "yum", "install", "-y", repoURL); err != nil {
		return fmt.Errorf("install pgdg repo: %w", err)
	}

	p.Logger.Info("installing PostgreSQL", "version", p.PGVersion)
	pkgName := fmt.Sprintf("postgresql%s-server", p.PGVersion)
	if err := p.runCommand(ctx, "yum", "install", "-y", pkgName); err != nil {
		return fmt.Errorf("install postgresql: %w", err)
	}

	// Initialize and start
	p.Logger.Info("initializing PostgreSQL database")
	initCmd := fmt.Sprintf("/usr/pgsql-%s/bin/postgresql-%s-setup", p.PGVersion, p.PGVersion)
	if err := p.runCommand(ctx, initCmd, "initdb"); err != nil {
		return fmt.Errorf("initdb: %w", err)
	}

	p.Logger.Info("enabling PostgreSQL service")
	serviceName := fmt.Sprintf("postgresql-%s", p.PGVersion)
	if err := p.runCommand(ctx, "systemctl", "enable", serviceName); err != nil {
		return fmt.Errorf("enable service: %w", err)
	}
	if err := p.runCommand(ctx, "systemctl", "start", serviceName); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	p.Logger.Info("PostgreSQL installed successfully")
	return nil
}

// installWithBrew installs PostgreSQL on macOS using Homebrew.
func (p *PostgresInstaller) installWithBrew(ctx context.Context) error {
	// Check if Homebrew is installed
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("Homebrew is not installed. Install it from https://brew.sh")
	}

	p.Logger.Info("installing PostgreSQL via Homebrew", "version", p.PGVersion)

	// Install specific version
	formula := fmt.Sprintf("postgresql@%s", p.PGVersion)
	if err := p.runCommand(ctx, "brew", "install", formula); err != nil {
		return fmt.Errorf("brew install: %w", err)
	}

	// Start service
	p.Logger.Info("starting PostgreSQL service")
	if err := p.runCommand(ctx, "brew", "services", "start", formula); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	p.Logger.Info("PostgreSQL installed successfully")
	return nil
}

// runCommand executes a command and logs its output.
func (p *PostgresInstaller) runCommand(ctx context.Context, name string, args ...string) error {
	p.Logger.Debug("executing command", "cmd", name, "args", args)

	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()

	if len(output) > 0 {
		p.Logger.Debug("command output", "output", string(output))
	}

	if err != nil {
		return fmt.Errorf("%s: %w (output: %s)", name, err, string(output))
	}

	return nil
}

// IsPostgresInstalled checks if PostgreSQL is already installed.
func IsPostgresInstalled() bool {
	// Check for common PostgreSQL commands
	for _, cmd := range []string{"psql", "pg_isready"} {
		if _, err := exec.LookPath(cmd); err == nil {
			return true
		}
	}

	// Check for PostgreSQL data directories
	dataDirs := []string{
		"/var/lib/postgresql",
		"/var/lib/pgsql",
		"/usr/local/var/postgresql",
	}
	for _, dir := range dataDirs {
		if _, err := exec.Command("test", "-d", dir).Output(); err == nil {
			return true
		}
	}

	return false
}

// GetInstalledPGVersion attempts to detect the installed PostgreSQL version.
func GetInstalledPGVersion() (string, error) {
	cmd := exec.Command("psql", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Parse "psql (PostgreSQL) 16.1" format
	parts := strings.Fields(string(output))
	if len(parts) >= 3 {
		version := parts[2]
		// Extract major version
		majorParts := strings.Split(version, ".")
		if len(majorParts) >= 1 {
			return majorParts[0], nil
		}
	}

	return "", fmt.Errorf("could not parse PostgreSQL version from: %s", output)
}
