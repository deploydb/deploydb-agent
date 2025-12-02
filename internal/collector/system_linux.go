//go:build linux

package collector

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
)

// CollectSystem collects system metrics (memory, CPU, load).
func (c *Collector) CollectSystem() (map[string]float64, error) {
	metrics := make(map[string]float64)

	// CPU count
	metrics["system_cpu_count"] = float64(runtime.NumCPU())

	// System info via sysinfo
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return nil, fmt.Errorf("sysinfo: %w", err)
	}

	// Memory metrics
	unit := uint64(info.Unit)
	metrics["system_memory_total_bytes"] = float64(info.Totalram * unit)
	metrics["system_memory_available_bytes"] = float64(info.Freeram * unit)

	// Memory used percent
	if info.Totalram > 0 {
		used := info.Totalram - info.Freeram
		metrics["system_memory_used_percent"] = float64(used) / float64(info.Totalram) * 100
	}

	// Load averages (scaled by 65536)
	metrics["system_load_1m"] = float64(info.Loads[0]) / 65536.0
	metrics["system_load_5m"] = float64(info.Loads[1]) / 65536.0
	metrics["system_load_15m"] = float64(info.Loads[2]) / 65536.0

	return metrics, nil
}

// CollectDisk collects disk metrics for the data directory.
func (c *Collector) CollectDisk() (map[string]float64, error) {
	metrics := make(map[string]float64)

	var stat unix.Statfs_t
	if err := unix.Statfs(c.config.DataDir, &stat); err != nil {
		return nil, fmt.Errorf("statfs %s: %w", c.config.DataDir, err)
	}

	blockSize := uint64(stat.Bsize)
	metrics["system_disk_total_bytes"] = float64(stat.Blocks * blockSize)
	metrics["system_disk_available_bytes"] = float64(stat.Bavail * blockSize)
	metrics["system_disk_used_bytes"] = float64((stat.Blocks - stat.Bfree) * blockSize)

	if stat.Blocks > 0 {
		used := stat.Blocks - stat.Bfree
		metrics["system_disk_used_percent"] = float64(used) / float64(stat.Blocks) * 100
	}

	return metrics, nil
}
