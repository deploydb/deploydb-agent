//go:build darwin

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

	// Load averages via getloadavg (available on macOS)
	var loadavg [3]float64
	if err := getLoadAvg(&loadavg); err == nil {
		metrics["system_load_1m"] = loadavg[0]
		metrics["system_load_5m"] = loadavg[1]
		metrics["system_load_15m"] = loadavg[2]
	} else {
		// Fallback to zeros
		metrics["system_load_1m"] = 0
		metrics["system_load_5m"] = 0
		metrics["system_load_15m"] = 0
	}

	// Get memory info via sysctl
	totalMem, err := sysctlUint64("hw.memsize")
	if err != nil {
		return nil, fmt.Errorf("hw.memsize: %w", err)
	}
	metrics["system_memory_total_bytes"] = float64(totalMem)

	// Get page size
	pageSize, err := sysctlUint64("hw.pagesize")
	if err != nil {
		pageSize = 4096 // default
	}

	// Get free pages (approximation)
	freePages, err := sysctlUint64("vm.page_free_count")
	if err != nil {
		freePages = 0
	}
	metrics["system_memory_available_bytes"] = float64(freePages * pageSize)

	// Memory used percent
	if totalMem > 0 {
		available := freePages * pageSize
		used := totalMem - available
		metrics["system_memory_used_percent"] = float64(used) / float64(totalMem) * 100
	}

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

// getLoadAvg gets load averages using the C library function
func getLoadAvg(loadavg *[3]float64) error {
	var avg [3]float64
	// Use unix.Getloadavg which wraps getloadavg(3)
	avgs, err := unix.Sysctl("vm.loadavg")
	if err != nil {
		return err
	}
	// Parse the load average string - fallback to defaults if parsing fails
	_, _ = fmt.Sscanf(avgs, "{ %f %f %f }", &avg[0], &avg[1], &avg[2])
	loadavg[0] = avg[0]
	loadavg[1] = avg[1]
	loadavg[2] = avg[2]
	return nil
}

// sysctlUint64 gets a uint64 value from sysctl
func sysctlUint64(name string) (uint64, error) {
	val, err := unix.SysctlUint64(name)
	if err != nil {
		return 0, err
	}
	return val, nil
}
