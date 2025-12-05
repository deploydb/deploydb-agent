package bootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func CollectSystemInfo() (map[string]interface{}, error) {
	info := make(map[string]interface{})

	// OS and architecture
	info["os"] = getOSName()
	info["arch"] = runtime.GOARCH

	// RAM in GB
	if ram, err := getTotalRAM(); err == nil {
		info["ram_gb"] = ram
	}

	// CPU count
	info["cpus"] = runtime.NumCPU()

	// Disk space (optional - for root filesystem)
	if disk, err := getTotalDisk(); err == nil {
		info["disk_gb"] = disk
	}

	return info, nil
}

func getOSName() string {
	switch runtime.GOOS {
	case "linux":
		// Try to get distribution name from /etc/os-release
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					name := strings.TrimPrefix(line, "PRETTY_NAME=")
					name = strings.Trim(name, "\"")
					return name
				}
			}
		}
		return "Linux"
	case "darwin":
		return "macOS"
	default:
		return runtime.GOOS
	}
}

func getTotalRAM() (int, error) {
	switch runtime.GOOS {
	case "linux":
		// Read /proc/meminfo
		data, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return 0, err
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					kb, _ := strconv.Atoi(fields[1])
					return kb / 1024 / 1024, nil // Convert KB to GB
				}
			}
		}
	case "darwin":
		// Use sysctl on macOS
		cmd := exec.Command("sysctl", "-n", "hw.memsize")
		output, err := cmd.Output()
		if err != nil {
			return 0, err
		}
		bytes, _ := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
		return int(bytes / 1024 / 1024 / 1024), nil
	}
	return 0, fmt.Errorf("unsupported OS")
}

func getTotalDisk() (int, error) {
	switch runtime.GOOS {
	case "linux", "darwin":
		cmd := exec.Command("df", "-BG", "/")
		output, err := cmd.Output()
		if err != nil {
			return 0, err
		}
		lines := strings.Split(string(output), "\n")
		if len(lines) > 1 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 2 {
				sizeStr := strings.TrimSuffix(fields[1], "G")
				size, _ := strconv.Atoi(sizeStr)
				return size, nil
			}
		}
	}
	return 0, fmt.Errorf("unsupported OS")
}
