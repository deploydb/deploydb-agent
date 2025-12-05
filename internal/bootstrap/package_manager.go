package bootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

type PackageManager interface {
	Install(pgVersion int) error
	IsInstalled() bool
}

func DetectPackageManager() (PackageManager, error) {
	switch runtime.GOOS {
	case "linux":
		// Check for apt (Debian/Ubuntu)
		if commandExists("apt-get") {
			return &AptPackageManager{}, nil
		}
		// Check for dnf (Fedora/RHEL 8+)
		if commandExists("dnf") {
			return &DnfPackageManager{}, nil
		}
		// Check for yum (RHEL 7)
		if commandExists("yum") {
			return &YumPackageManager{}, nil
		}
		return nil, fmt.Errorf("no supported package manager found (apt, dnf, yum)")
	case "darwin":
		if commandExists("brew") {
			return &BrewPackageManager{}, nil
		}
		return nil, fmt.Errorf("homebrew not found - install from https://brew.sh")
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// Apt Package Manager (Ubuntu/Debian)
type AptPackageManager struct{}

func (a *AptPackageManager) IsInstalled() bool {
	return commandExists("psql") || commandExists("pg_ctl")
}

func (a *AptPackageManager) Install(pgVersion int) error {
	// Add PGDG repository
	if err := a.addPGDGRepo(); err != nil {
		return fmt.Errorf("failed to add PGDG repository: %w", err)
	}

	// Update package list
	if err := runCommand("apt-get", "update"); err != nil {
		return fmt.Errorf("apt-get update failed: %w", err)
	}

	// Install PostgreSQL
	pkg := fmt.Sprintf("postgresql-%d", pgVersion)
	if err := runCommand("apt-get", "install", "-y", pkg); err != nil {
		return fmt.Errorf("failed to install %s: %w", pkg, err)
	}

	return nil
}

func (a *AptPackageManager) addPGDGRepo() error {
	// Install prerequisites
	if err := runCommand("apt-get", "install", "-y", "curl", "ca-certificates", "gnupg"); err != nil {
		return err
	}

	// Add GPG key
	if err := runCommand("sh", "-c",
		"curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor -o /usr/share/keyrings/pgdg-archive-keyring.gpg"); err != nil {
		return err
	}

	// Add repository
	if err := runCommand("sh", "-c",
		"echo 'deb [signed-by=/usr/share/keyrings/pgdg-archive-keyring.gpg] https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main' > /etc/apt/sources.list.d/pgdg.list"); err != nil {
		return err
	}

	return nil
}

// DNF Package Manager (Fedora/RHEL 8+)
type DnfPackageManager struct{}

func (d *DnfPackageManager) IsInstalled() bool {
	return commandExists("psql") || commandExists("pg_ctl")
}

func (d *DnfPackageManager) Install(pgVersion int) error {
	// Add PGDG repository
	repo := "https://download.postgresql.org/pub/repos/yum/reporpms/EL-8-x86_64/pgdg-redhat-repo-latest.noarch.rpm"
	if err := runCommand("dnf", "install", "-y", repo); err != nil {
		return fmt.Errorf("failed to add PGDG repo: %w", err)
	}

	// Disable built-in PostgreSQL module
	_ = runCommand("dnf", "-qy", "module", "disable", "postgresql")

	// Install PostgreSQL
	pkg := fmt.Sprintf("postgresql%d-server", pgVersion)
	if err := runCommand("dnf", "install", "-y", pkg); err != nil {
		return fmt.Errorf("failed to install %s: %w", pkg, err)
	}

	return nil
}

// Yum Package Manager (RHEL 7)
type YumPackageManager struct{}

func (y *YumPackageManager) IsInstalled() bool {
	return commandExists("psql") || commandExists("pg_ctl")
}

func (y *YumPackageManager) Install(pgVersion int) error {
	// Similar to DNF but uses yum
	repo := "https://download.postgresql.org/pub/repos/yum/reporpms/EL-7-x86_64/pgdg-redhat-repo-latest.noarch.rpm"
	if err := runCommand("yum", "install", "-y", repo); err != nil {
		return fmt.Errorf("failed to add PGDG repo: %w", err)
	}

	pkg := fmt.Sprintf("postgresql%d-server", pgVersion)
	if err := runCommand("yum", "install", "-y", pkg); err != nil {
		return fmt.Errorf("failed to install %s: %w", pkg, err)
	}

	return nil
}

// Brew Package Manager (macOS)
type BrewPackageManager struct{}

func (b *BrewPackageManager) IsInstalled() bool {
	return commandExists("psql") || commandExists("pg_ctl")
}

func (b *BrewPackageManager) Install(pgVersion int) error {
	pkg := fmt.Sprintf("postgresql@%d", pgVersion)
	if err := runCommand("brew", "install", pkg); err != nil {
		return fmt.Errorf("failed to install %s: %w", pkg, err)
	}
	return nil
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
