package bootstrap

import (
	"runtime"
	"testing"
)

func TestDetectOS(t *testing.T) {
	info, err := DetectOS()
	if err != nil {
		// On unsupported systems, this is expected
		if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
			t.Skipf("Skipping on unsupported OS: %s", runtime.GOOS)
		}
		t.Fatalf("DetectOS failed: %v", err)
	}

	if info.OS != runtime.GOOS {
		t.Errorf("Expected OS %s, got %s", runtime.GOOS, info.OS)
	}

	if info.Arch == "" {
		t.Error("Arch should not be empty")
	}

	if info.PackageManager == "" && runtime.GOOS != "linux" {
		// macOS should have brew
		if runtime.GOOS == "darwin" && info.PackageManager != "brew" {
			t.Errorf("Expected package manager 'brew' on macOS, got %s", info.PackageManager)
		}
	}
}

func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"amd64", "amd64"},
		{"arm64", "arm64"},
		{"386", "386"},
	}

	for _, tt := range tests {
		result := normalizeArch(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeArch(%s) = %s, expected %s", tt.input, result, tt.expected)
		}
	}
}

func TestOSInfoString(t *testing.T) {
	tests := []struct {
		info     OSInfo
		expected string
	}{
		{
			OSInfo{OS: "darwin", Distro: "macos", Arch: "arm64"},
			"macOS (arm64)",
		},
		{
			OSInfo{OS: "linux", Distro: "ubuntu", Version: "22.04", Arch: "amd64"},
			"ubuntu 22.04 (amd64)",
		},
	}

	for _, tt := range tests {
		result := tt.info.String()
		if result != tt.expected {
			t.Errorf("OSInfo.String() = %s, expected %s", result, tt.expected)
		}
	}
}

func TestIsSupported(t *testing.T) {
	tests := []struct {
		info      OSInfo
		supported bool
	}{
		// macOS is always supported
		{OSInfo{OS: "darwin", Distro: "macos"}, true},

		// Supported Ubuntu versions
		{OSInfo{OS: "linux", Distro: "ubuntu", Version: "22.04"}, true},
		{OSInfo{OS: "linux", Distro: "ubuntu", Version: "24.04"}, true},
		{OSInfo{OS: "linux", Distro: "ubuntu", Version: "20.04"}, true},

		// Unsupported Ubuntu versions
		{OSInfo{OS: "linux", Distro: "ubuntu", Version: "18.04"}, false},

		// Supported Debian versions
		{OSInfo{OS: "linux", Distro: "debian", Version: "12"}, true},
		{OSInfo{OS: "linux", Distro: "debian", Version: "11"}, true},

		// Unsupported Debian versions
		{OSInfo{OS: "linux", Distro: "debian", Version: "10"}, false},

		// Supported RHEL versions
		{OSInfo{OS: "linux", Distro: "rhel", Version: "9"}, true},
		{OSInfo{OS: "linux", Distro: "rhel", Version: "8"}, true},
		{OSInfo{OS: "linux", Distro: "rocky", Version: "9.2"}, true},

		// Unsupported RHEL versions
		{OSInfo{OS: "linux", Distro: "rhel", Version: "7"}, false},

		// Fedora (rolling, always supported)
		{OSInfo{OS: "linux", Distro: "fedora", Version: "40"}, true},

		// Unsupported distros
		{OSInfo{OS: "linux", Distro: "arch", Version: ""}, false},
		{OSInfo{OS: "linux", Distro: "gentoo", Version: ""}, false},
	}

	for _, tt := range tests {
		result := tt.info.IsSupported()
		if result != tt.supported {
			t.Errorf("IsSupported() for %s = %v, expected %v", tt.info.String(), result, tt.supported)
		}
	}
}
