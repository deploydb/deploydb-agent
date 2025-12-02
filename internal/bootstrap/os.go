// Package bootstrap handles PostgreSQL installation and initial configuration.
package bootstrap

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// OSInfo contains information about the operating system.
type OSInfo struct {
	// OS is the operating system type (linux, darwin)
	OS string

	// Distro is the Linux distribution ID (ubuntu, debian, rhel, rocky, almalinux, fedora)
	// Empty for non-Linux systems.
	Distro string

	// Version is the distribution version (e.g., "22.04", "9")
	Version string

	// VersionCodename is the version codename (e.g., "jammy", "bookworm")
	// Only available on Debian/Ubuntu.
	VersionCodename string

	// Arch is the system architecture (amd64, arm64)
	Arch string

	// PackageManager is the package manager to use (apt, dnf, yum, brew)
	PackageManager string
}

// SupportedDistros lists the distributions we support for PostgreSQL installation.
var SupportedDistros = map[string]bool{
	"ubuntu":    true,
	"debian":    true,
	"rhel":      true,
	"rocky":     true,
	"almalinux": true,
	"fedora":    true,
	"centos":    true, // CentOS Stream not fully supported, but we'll try
}

// SupportedUbuntuVersions lists Ubuntu versions with PGDG support.
var SupportedUbuntuVersions = map[string]string{
	"20.04": "focal",
	"22.04": "jammy",
	"24.04": "noble",
}

// SupportedDebianVersions lists Debian versions with PGDG support.
var SupportedDebianVersions = map[string]string{
	"11": "bullseye",
	"12": "bookworm",
}

// SupportedRHELVersions lists RHEL/Rocky/Alma versions with PGDG support.
var SupportedRHELVersions = []string{"8", "9"}

// DetectOS detects the current operating system and returns OSInfo.
func DetectOS() (*OSInfo, error) {
	info := &OSInfo{
		OS:   runtime.GOOS,
		Arch: normalizeArch(runtime.GOARCH),
	}

	switch runtime.GOOS {
	case "linux":
		if err := detectLinuxDistro(info); err != nil {
			return nil, err
		}
	case "darwin":
		info.Distro = "macos"
		info.PackageManager = "brew"
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	return info, nil
}

// detectLinuxDistro reads /etc/os-release to detect the Linux distribution.
func detectLinuxDistro(info *OSInfo) error {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return fmt.Errorf("cannot detect Linux distribution: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := strings.Trim(parts[1], `"`)

		switch key {
		case "ID":
			info.Distro = value
		case "VERSION_ID":
			info.Version = value
		case "VERSION_CODENAME":
			info.VersionCodename = value
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading os-release: %w", err)
	}

	// Determine package manager
	switch info.Distro {
	case "ubuntu", "debian":
		info.PackageManager = "apt"
	case "rhel", "rocky", "almalinux", "centos":
		// RHEL 8+ uses dnf, older uses yum
		if info.Version >= "8" {
			info.PackageManager = "dnf"
		} else {
			info.PackageManager = "yum"
		}
	case "fedora":
		info.PackageManager = "dnf"
	default:
		return fmt.Errorf("unsupported Linux distribution: %s", info.Distro)
	}

	return nil
}

// normalizeArch converts Go's GOARCH to the format used by PostgreSQL packages.
func normalizeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return goarch
	}
}

// IsSupported returns true if the OS is supported for PostgreSQL installation.
func (o *OSInfo) IsSupported() bool {
	if o.OS == "darwin" {
		return true
	}

	if !SupportedDistros[o.Distro] {
		return false
	}

	// Check version support
	switch o.Distro {
	case "ubuntu":
		_, ok := SupportedUbuntuVersions[o.Version]
		return ok
	case "debian":
		// Extract major version
		majorVersion := strings.Split(o.Version, ".")[0]
		_, ok := SupportedDebianVersions[majorVersion]
		return ok
	case "rhel", "rocky", "almalinux", "centos":
		majorVersion := strings.Split(o.Version, ".")[0]
		for _, v := range SupportedRHELVersions {
			if majorVersion == v {
				return true
			}
		}
		return false
	case "fedora":
		// Fedora is rolling, generally supported
		return true
	}

	return false
}

// String returns a human-readable description of the OS.
func (o *OSInfo) String() string {
	if o.OS == "darwin" {
		return fmt.Sprintf("macOS (%s)", o.Arch)
	}
	return fmt.Sprintf("%s %s (%s)", o.Distro, o.Version, o.Arch)
}
