package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// Collector gathers system and container metrics.
type Collector struct {
	logger *slog.Logger
}

// NewCollector creates a new Collector.
func NewCollector(logger *slog.Logger) *Collector {
	return &Collector{logger: logger}
}

// CollectSystem gathers system-level metrics using gopsutil.
func (c *Collector) CollectSystem() (*MetricSnapshot, error) {
	snap := &MetricSnapshot{}

	// CPU
	cpuPcts, err := cpu.Percent(0, false)
	if err == nil && len(cpuPcts) > 0 {
		snap.CPUPercent = cpuPcts[0]
	}

	// Load average
	loadAvg, err := load.Avg()
	if err == nil && loadAvg != nil {
		snap.LoadAvg1 = loadAvg.Load1
		snap.LoadAvg5 = loadAvg.Load5
		snap.LoadAvg15 = loadAvg.Load15
	}

	// Memory
	vmem, err := mem.VirtualMemory()
	if err == nil && vmem != nil {
		snap.MemTotal = vmem.Total
		snap.MemUsed = vmem.Used
		snap.MemPercent = vmem.UsedPercent
	}

	// Swap
	swap, err := mem.SwapMemory()
	if err == nil && swap != nil {
		snap.SwapTotal = swap.Total
		snap.SwapUsed = swap.Used
	}

	// Disk usage (root partition)
	diskUsage, err := disk.Usage("/")
	if err == nil && diskUsage != nil {
		snap.DiskTotal = diskUsage.Total
		snap.DiskUsed = diskUsage.Used
		snap.DiskPercent = diskUsage.UsedPercent
	}

	// Disk I/O (aggregate all disks)
	diskIO, err := disk.IOCounters()
	if err == nil {
		for _, d := range diskIO {
			snap.DiskReadBytes += d.ReadBytes
			snap.DiskWriteBytes += d.WriteBytes
		}
	}

	// Network I/O (aggregate all interfaces)
	netIO, err := net.IOCounters(false)
	if err == nil && len(netIO) > 0 {
		snap.NetRecvBytes = netIO[0].BytesRecv
		snap.NetSentBytes = netIO[0].BytesSent
	}

	return snap, nil
}

// dockerStatsEntry represents one entry from `docker stats --no-stream --format json`.
type dockerStatsEntry struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
	MemPerc  string `json:"MemPerc"`
}

// CollectContainers gathers per-container metrics via the Docker CLI.
func (c *Collector) CollectContainers() ([]ContainerMetric, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker stats: %w", err)
	}

	var metrics []ContainerMetric
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		var entry dockerStatsEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			c.logger.Warn("parse docker stats line", "err", err, "line", line)
			continue
		}

		cm := ContainerMetric{
			ID:     entry.ID,
			Name:   strings.TrimPrefix(entry.Name, "/"),
			Status: "running",
		}

		// Parse CPU percentage (e.g. "0.50%")
		cm.CPUPercent = parsePercent(entry.CPUPerc)

		// Parse memory percentage (e.g. "1.23%")
		cm.MemPercent = parsePercent(entry.MemPerc)

		// Parse memory usage (e.g. "50MiB / 512MiB")
		parts := strings.Split(entry.MemUsage, "/")
		if len(parts) == 2 {
			cm.MemUsage = parseBytes(strings.TrimSpace(parts[0]))
			cm.MemLimit = parseBytes(strings.TrimSpace(parts[1]))
		}

		metrics = append(metrics, cm)
	}

	return metrics, nil
}

// parsePercent parses a percentage string like "1.23%" to a float64.
func parsePercent(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v
}

// parseBytes parses a human-readable byte string like "50.5MiB" to uint64 bytes.
func parseBytes(s string) uint64 {
	s = strings.TrimSpace(s)
	var val float64
	var unit string
	fmt.Sscanf(s, "%f%s", &val, &unit)
	unit = strings.ToLower(unit)
	switch {
	case strings.HasPrefix(unit, "kib"), strings.HasPrefix(unit, "kb"):
		return uint64(val * 1024)
	case strings.HasPrefix(unit, "mib"), strings.HasPrefix(unit, "mb"):
		return uint64(val * 1024 * 1024)
	case strings.HasPrefix(unit, "gib"), strings.HasPrefix(unit, "gb"):
		return uint64(val * 1024 * 1024 * 1024)
	case strings.HasPrefix(unit, "tib"), strings.HasPrefix(unit, "tb"):
		return uint64(val * 1024 * 1024 * 1024 * 1024)
	default:
		return uint64(val)
	}
}
