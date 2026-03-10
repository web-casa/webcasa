package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

// InspectionService runs periodic system health inspections and generates
// AI-powered summary reports.
type InspectionService struct {
	svc         *Service
	coreAPI     pluginpkg.CoreAPI
	configStore *pluginpkg.ConfigStore
	eventBus    *pluginpkg.EventBus
	logger      *slog.Logger
	db          *gorm.DB
	mu          sync.Mutex
	stopCh      chan struct{}
	running     bool
}

// InspectionReport is the result of a single health inspection.
type InspectionReport struct {
	Timestamp    time.Time                `json:"timestamp"`
	Metrics      map[string]interface{}   `json:"metrics"`
	Hosts        []map[string]interface{} `json:"hosts"`
	Containers   []map[string]interface{} `json:"containers"`
	Alerts       []map[string]interface{} `json:"alerts"`
	Findings     []InspectionFinding      `json:"findings"`
	AISummary    string                   `json:"ai_summary"`
	OverallScore string                   `json:"overall_score"` // healthy, warning, critical
}

// InspectionFinding is a single issue found during inspection.
type InspectionFinding struct {
	Severity    string `json:"severity"`    // info, warning, critical
	Category    string `json:"category"`    // ssl, disk, container, config, memory
	Title       string `json:"title"`
	Description string `json:"description"`
}

// NewInspectionService creates a new inspection service.
func NewInspectionService(svc *Service, coreAPI pluginpkg.CoreAPI, configStore *pluginpkg.ConfigStore, eventBus *pluginpkg.EventBus, db *gorm.DB, logger *slog.Logger) *InspectionService {
	return &InspectionService{
		svc:         svc,
		coreAPI:     coreAPI,
		configStore: configStore,
		eventBus:    eventBus,
		db:          db,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}
}

// Start begins the daily inspection scheduler.
func (is *InspectionService) Start() {
	is.mu.Lock()
	defer is.mu.Unlock()

	enabled := is.configStore.Get("inspection_enabled")
	if enabled != "true" {
		is.logger.Info("inspection scheduler disabled")
		return
	}

	is.running = true
	go is.scheduleLoop()
	is.logger.Info("inspection scheduler started")
}

// Stop terminates the scheduler. Safe to call multiple times.
func (is *InspectionService) Stop() {
	is.mu.Lock()
	defer is.mu.Unlock()

	if !is.running {
		return
	}
	close(is.stopCh)
	is.running = false
}

// Reschedule stops the current scheduler (if running) and restarts based on
// the current configuration. Call after updating inspection_enabled.
func (is *InspectionService) Reschedule() {
	is.Stop()

	is.mu.Lock()
	// Reset the stop channel for a fresh start.
	is.stopCh = make(chan struct{})
	is.mu.Unlock()

	is.Start()
}

// scheduleLoop runs the inspection at the configured hour each day.
func (is *InspectionService) scheduleLoop() {
	for {
		now := time.Now()
		hour := is.getHour()
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		wait := time.Until(next)

		select {
		case <-is.stopCh:
			return
		case <-time.After(wait):
			report, err := is.RunInspection()
			if err != nil {
				is.logger.Error("scheduled inspection failed", "err", err)
			} else {
				is.logger.Info("scheduled inspection completed", "score", report.OverallScore, "findings", len(report.Findings))
			}
		}
	}
}

// getHour returns the configured inspection hour (0-23), default 8.
func (is *InspectionService) getHour() int {
	h := is.configStore.Get("inspection_hour")
	if h == "" {
		return 8
	}
	hour, err := strconv.Atoi(h)
	if err != nil || hour < 0 || hour > 23 {
		return 8
	}
	return hour
}

// RunInspection executes a full system health inspection.
func (is *InspectionService) RunInspection() (*InspectionReport, error) {
	report := &InspectionReport{
		Timestamp: time.Now(),
	}

	// 1. Collect system metrics
	if metrics, err := is.coreAPI.GetMetrics(); err == nil {
		report.Metrics = metrics
	} else {
		is.logger.Warn("inspection: failed to get metrics", "err", err)
	}

	// 2. Collect hosts
	if hosts, err := is.coreAPI.ListHosts(); err == nil {
		report.Hosts = hosts
	} else {
		is.logger.Warn("inspection: failed to list hosts", "err", err)
	}

	// 3. Collect containers
	if containers, err := is.coreAPI.DockerPS(); err == nil {
		report.Containers = containers
	} else {
		is.logger.Warn("inspection: failed to list containers", "err", err)
	}

	// 4. Collect recent alerts
	if alerts, err := is.coreAPI.GetRecentAlerts(); err == nil {
		report.Alerts = alerts
	} else {
		is.logger.Warn("inspection: failed to get alerts", "err", err)
	}

	// 5. Rule-based checks
	report.Findings = is.runChecks(report)
	report.OverallScore = is.scoreFindings(report.Findings)

	// 6. Generate AI summary (optional)
	aiEnabled := is.configStore.Get("inspection_ai_summary")
	if aiEnabled != "false" {
		summary, err := is.generateAISummary(report)
		if err != nil {
			is.logger.Warn("inspection: AI summary failed", "err", err)
		} else {
			report.AISummary = summary
		}
	}

	// 7. Persist to DB
	is.saveRecord(report)

	// 8. Publish event for notification
	if is.eventBus != nil {
		is.eventBus.Publish(pluginpkg.Event{
			Type:   "system.inspection.completed",
			Source: "ai",
			Payload: map[string]interface{}{
				"overall_score": report.OverallScore,
				"findings":      len(report.Findings),
				"ai_summary":    report.AISummary,
			},
			Time: time.Now(),
		})
	}

	return report, nil
}

// runChecks performs rule-based checks on collected data.
func (is *InspectionService) runChecks(report *InspectionReport) []InspectionFinding {
	var findings []InspectionFinding

	if report.Metrics != nil {
		// Disk usage check — keys: disk_total, disk_used, disk_available (strings in bytes)
		diskTotal := parseStringToFloat(report.Metrics["disk_total"])
		diskUsed := parseStringToFloat(report.Metrics["disk_used"])
		if diskTotal > 0 {
			diskPct := (diskUsed / diskTotal) * 100
			if diskPct > 85 {
				sev := "warning"
				if diskPct > 95 {
					sev = "critical"
				}
				findings = append(findings, InspectionFinding{
					Severity:    sev,
					Category:    "disk",
					Title:       "High disk usage",
					Description: fmt.Sprintf("Disk usage is %.1f%%, exceeding 85%% threshold", diskPct),
				})
			}
		}

		// Memory usage check — keys: mem_total_kb, mem_available_kb (strings in KB)
		memTotal := parseStringToFloat(report.Metrics["mem_total_kb"])
		memAvail := parseStringToFloat(report.Metrics["mem_available_kb"])
		if memTotal > 0 {
			memPct := ((memTotal - memAvail) / memTotal) * 100
			if memPct > 90 {
				sev := "warning"
				if memPct > 95 {
					sev = "critical"
				}
				findings = append(findings, InspectionFinding{
					Severity:    sev,
					Category:    "memory",
					Title:       "High memory usage",
					Description: fmt.Sprintf("Memory usage is %.1f%%, exceeding 90%% threshold", memPct),
				})
			}
		}

		// Load average check — key: load_1 (string)
		if load1 := parseStringToFloat(report.Metrics["load_1"]); load1 > 5 {
			findings = append(findings, InspectionFinding{
				Severity:    "warning",
				Category:    "cpu",
				Title:       "High load average",
				Description: fmt.Sprintf("1-minute load average is %.2f", load1),
			})
		}
	}

	// Container health checks
	for _, c := range report.Containers {
		state, _ := c["state"].(string)
		name, _ := c["name"].(string)
		if state != "" && state != "running" && state != "exited" {
			findings = append(findings, InspectionFinding{
				Severity:    "warning",
				Category:    "container",
				Title:       fmt.Sprintf("Container '%s' in abnormal state", name),
				Description: fmt.Sprintf("Container %s is in '%s' state", name, state),
			})
		}
	}

	// Recent alerts check
	if len(report.Alerts) > 5 {
		findings = append(findings, InspectionFinding{
			Severity:    "warning",
			Category:    "monitoring",
			Title:       "High number of recent alerts",
			Description: fmt.Sprintf("%d alerts triggered recently — review monitoring rules", len(report.Alerts)),
		})
	}

	return findings
}

// scoreFindings determines the overall health score from findings.
func (is *InspectionService) scoreFindings(findings []InspectionFinding) string {
	for _, f := range findings {
		if f.Severity == "critical" {
			return "critical"
		}
	}
	for _, f := range findings {
		if f.Severity == "warning" {
			return "warning"
		}
	}
	return "healthy"
}

// generateAISummary calls the AI service to generate a human-readable summary.
func (is *InspectionService) generateAISummary(report *InspectionReport) (string, error) {
	// Build a context string for AI
	var sb strings.Builder
	sb.WriteString("System Health Inspection Report\n\n")

	if report.Metrics != nil {
		sb.WriteString("## System Metrics\n")
		for k, v := range report.Metrics {
			sb.WriteString(fmt.Sprintf("- %s: %v\n", k, v))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("## Sites: %d configured\n", len(report.Hosts)))
	sb.WriteString(fmt.Sprintf("## Containers: %d running\n", len(report.Containers)))
	sb.WriteString(fmt.Sprintf("## Recent Alerts: %d\n\n", len(report.Alerts)))

	if len(report.Findings) > 0 {
		sb.WriteString("## Findings\n")
		for _, f := range report.Findings {
			sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", f.Severity, f.Title, f.Description))
		}
	} else {
		sb.WriteString("## Findings: None — all checks passed\n")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := is.svc.DiagnoseSync(ctx, DiagnoseRequest{
		Logs:    sb.String(),
		Context: "Generate a concise daily health inspection summary in the user's preferred language. Include: overall status, key findings, and recommended actions. Keep it under 500 words.",
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

// saveRecord persists the inspection report to the database.
func (is *InspectionService) saveRecord(report *InspectionReport) {
	findingsJSON, _ := json.Marshal(report.Findings)
	record := InspectionRecord{
		Timestamp:    report.Timestamp,
		OverallScore: report.OverallScore,
		FindingsJSON: string(findingsJSON),
		AISummary:    report.AISummary,
	}
	if err := is.db.Create(&record).Error; err != nil {
		is.logger.Error("failed to save inspection record", "err", err)
	}
}

// GetHistory returns recent inspection records.
func (is *InspectionService) GetHistory(limit int) ([]InspectionRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	var records []InspectionRecord
	err := is.db.Order("timestamp DESC").Limit(limit).Find(&records).Error
	return records, err
}

// parseStringToFloat converts a string or numeric value to float64.
// Returns 0 if conversion fails or value is nil.
func parseStringToFloat(v interface{}) float64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	case float64:
		return n
	case int:
		return float64(n)
	case uint64:
		return float64(n)
	}
	return 0
}

// toFloat64 converts common numeric types to float64.
func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
