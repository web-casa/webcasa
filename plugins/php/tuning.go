package php

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// SystemInfo holds server resource information for tuning calculations.
type SystemInfo struct {
	TotalRAMMB   int `json:"total_ram_mb"`
	CPUCores     int `json:"cpu_cores"`
}

// GetSystemInfo reads server RAM and CPU info.
func GetSystemInfo() SystemInfo {
	info := SystemInfo{
		CPUCores: runtime.NumCPU(),
	}

	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if kb, err := strconv.Atoi(fields[1]); err == nil {
						info.TotalRAMMB = kb / 1024
					}
				}
				break
			}
		}
	}

	return info
}

// TuningPreset represents a named FPM tuning preset.
type TuningPreset struct {
	Name   string        `json:"name"`
	Label  string        `json:"label"`
	Config FPMPoolConfig `json:"config"`
}

// TuningPresets provides pre-configured FPM settings for common scenarios.
var TuningPresets = []TuningPreset{
	{
		Name:  "development",
		Label: "Development",
		Config: FPMPoolConfig{
			PM: "ondemand", MaxChildren: 5,
			MaxRequests: 500, IdleTimeout: 10,
		},
	},
	{
		Name:  "small_vps",
		Label: "Small VPS (1-2 GB)",
		Config: FPMPoolConfig{
			PM: "dynamic", MaxChildren: 12,
			StartServers: 4, MinSpareServers: 2, MaxSpareServers: 4,
			MaxRequests: 500,
		},
	},
	{
		Name:  "medium",
		Label: "Medium Server (4-8 GB)",
		Config: FPMPoolConfig{
			PM: "dynamic", MaxChildren: 40,
			StartServers: 8, MinSpareServers: 4, MaxSpareServers: 8,
			MaxRequests: 500,
		},
	},
	{
		Name:  "high_performance",
		Label: "High Performance (16+ GB)",
		Config: FPMPoolConfig{
			PM: "static", MaxChildren: 120,
			MaxRequests: 1000,
		},
	},
}

// CalculateOptimalFPM computes optimal FPM pool settings based on server resources.
// reservedMB is the amount of RAM reserved for the OS and other services.
// avgProcessMB is the average memory per PHP-FPM process (typically 50-80 MB).
func CalculateOptimalFPM(totalRAMMB, reservedMB, avgProcessMB, cpuCores int) FPMPoolConfig {
	// Guard against zero/negative RAM.
	if totalRAMMB <= 0 {
		return DefaultFPMPoolConfig()
	}
	if cpuCores <= 0 {
		cpuCores = 1
	}
	if reservedMB <= 0 {
		// Reserve ~20% for OS + other services, minimum 256 MB.
		reservedMB = totalRAMMB / 5
		if reservedMB < 256 {
			reservedMB = 256
		}
	}
	if avgProcessMB <= 0 {
		avgProcessMB = 80
	}

	availableRAM := totalRAMMB - reservedMB
	if availableRAM < 200 {
		availableRAM = 200
	}

	// 80% safety factor
	maxChildren := int(float64(availableRAM) / float64(avgProcessMB) * 0.8)
	if maxChildren < 5 {
		maxChildren = 5
	}
	if maxChildren > 512 {
		maxChildren = 512
	}

	startServers := cpuCores * 4
	if startServers > maxChildren/2 {
		startServers = maxChildren / 2
	}
	if startServers < 2 {
		startServers = 2
	}

	minSpare := cpuCores * 2
	if minSpare < 2 {
		minSpare = 2
	}
	if minSpare > startServers {
		minSpare = startServers
	}

	maxSpare := cpuCores * 4
	if maxSpare > maxChildren/2 {
		maxSpare = maxChildren / 2
	}
	if maxSpare < minSpare {
		maxSpare = minSpare
	}

	pm := "dynamic"
	if totalRAMMB >= 16384 {
		pm = "static"
	} else if totalRAMMB <= 1024 {
		pm = "ondemand"
	}

	return FPMPoolConfig{
		PM:              pm,
		MaxChildren:     maxChildren,
		StartServers:    startServers,
		MinSpareServers: minSpare,
		MaxSpareServers: maxSpare,
		MaxRequests:     500,
		IdleTimeout:     10,
	}
}

// AutoOptimize reads system info and returns optimal FPM config.
func AutoOptimize() FPMPoolConfig {
	info := GetSystemInfo()
	return CalculateOptimalFPM(info.TotalRAMMB, 0, 80, info.CPUCores)
}
