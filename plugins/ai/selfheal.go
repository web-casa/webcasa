package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// SelfHealEngine listens for monitoring alert events and triggers AI diagnosis
// with optional automatic remediation.
type SelfHealEngine struct {
	svc          *Service
	coreAPI      pluginpkg.CoreAPI
	eventBus     *pluginpkg.EventBus
	logger       *slog.Logger
	lastHealTime time.Time // rate-limit: prevent repeated actions
}

// NewSelfHealEngine creates a new self-heal engine.
func NewSelfHealEngine(svc *Service, coreAPI pluginpkg.CoreAPI, eventBus *pluginpkg.EventBus, logger *slog.Logger) *SelfHealEngine {
	return &SelfHealEngine{
		svc:      svc,
		coreAPI:  coreAPI,
		eventBus: eventBus,
		logger:   logger,
	}
}

// Subscribe registers the event listener for monitoring alerts.
func (sh *SelfHealEngine) Subscribe() {
	if sh.eventBus == nil {
		return
	}
	sh.eventBus.Subscribe("monitoring.alert.fired", func(e pluginpkg.Event) {
		go sh.handleAlert(e)
	})
	sh.logger.Info("self-heal engine subscribed to monitoring.alert.fired")
}

// handleAlert processes a single alert event.
func (sh *SelfHealEngine) handleAlert(e pluginpkg.Event) {
	mode, _ := e.Payload["auto_heal_mode"].(string)
	if mode != "suggest" && mode != "auto" {
		return // notify-only mode, skip
	}

	// Rate limit: at most one heal cycle per 5 minutes.
	if time.Since(sh.lastHealTime) < 5*time.Minute {
		sh.logger.Info("self-heal: rate-limited, skipping", "last_heal", sh.lastHealTime)
		return
	}

	// rule_id is published as uint from alerter, but may also arrive as float64
	// after JSON round-tripping.
	var ruleID uint
	switch v := e.Payload["rule_id"].(type) {
	case uint:
		ruleID = v
	case float64:
		ruleID = uint(v)
	case int:
		ruleID = uint(v)
	}
	ruleName, _ := e.Payload["rule_name"].(string)
	metric, _ := e.Payload["metric"].(string)
	value, _ := e.Payload["value"].(float64)
	threshold, _ := e.Payload["threshold"].(float64)

	sh.logger.Info("self-heal processing alert",
		"rule", ruleName, "mode", mode, "metric", metric, "value", value)

	// Collect diagnostic context
	diagCtx := sh.collectDiagnosticContext(metric, value, threshold, ruleName)

	// Run AI diagnosis
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`System alert triggered: %s
Metric: %s, Current value: %.2f, Threshold: %.2f

%s

Analyze the situation and provide:
1. Root cause analysis
2. Recommended actions (be specific)
3. If mode is "auto", provide a JSON block with safe remediation actions from this whitelist: restart_caddy, reload_caddy, restart_container (with container_id)

Response format:
## Diagnosis
[your analysis]

## Recommended Actions
[specific steps]

## Auto-Heal Actions (JSON)
` + "```json" + `
{"actions": [{"type": "restart_caddy"} or {"type": "reload_caddy"} or {"type": "restart_container", "container_id": "xxx"}]}
` + "```", ruleName, metric, value, threshold, diagCtx)

	diagnosis, err := sh.svc.DiagnoseSync(ctx, DiagnoseRequest{
		Logs:    prompt,
		Context: "Self-heal diagnosis for monitoring alert",
	})
	if err != nil {
		sh.logger.Error("self-heal diagnosis failed", "rule", ruleName, "err", err)
		return
	}

	// Save diagnosis to the alert rule
	sh.coreAPI.GetDB().Table("plugin_monitoring_alert_rules").
		Where("id = ?", ruleID).
		Update("last_diagnosis", diagnosis)

	// Publish diagnosis event
	sh.eventBus.Publish(pluginpkg.Event{
		Type:   "system.selfheal.diagnosis",
		Source: "ai",
		Payload: map[string]interface{}{
			"rule_id":   ruleID,
			"rule_name": ruleName,
			"metric":    metric,
			"value":     value,
			"mode":      mode,
			"diagnosis": truncate(diagnosis, 500),
		},
		Time: time.Now(),
	})

	// If auto mode, attempt safe remediation
	if mode == "auto" {
		sh.executeAutoHeal(diagnosis, ruleID, ruleName)
	}
}

// collectDiagnosticContext gathers relevant system data for AI analysis.
func (sh *SelfHealEngine) collectDiagnosticContext(metric string, value, threshold float64, ruleName string) string {
	var sb strings.Builder
	sb.WriteString("## Diagnostic Context\n\n")

	// System metrics
	if metrics, err := sh.coreAPI.GetMetrics(); err == nil {
		sb.WriteString("### System Metrics\n")
		for k, v := range metrics {
			sb.WriteString(fmt.Sprintf("- %s: %v\n", k, v))
		}
		sb.WriteString("\n")
	}

	// Container status (relevant for container-related alerts)
	if containers, err := sh.coreAPI.DockerPS(); err == nil && len(containers) > 0 {
		sb.WriteString("### Docker Containers\n")
		for _, c := range containers {
			name, _ := c["name"].(string)
			state, _ := c["state"].(string)
			sb.WriteString(fmt.Sprintf("- %s: %s\n", name, state))
		}
		sb.WriteString("\n")
	}

	// Recent alerts
	if alerts, err := sh.coreAPI.GetRecentAlerts(); err == nil && len(alerts) > 0 {
		sb.WriteString("### Recent Alerts\n")
		for i, a := range alerts {
			if i >= 5 {
				break
			}
			name, _ := a["rule_name"].(string)
			msg, _ := a["message"].(string)
			sb.WriteString(fmt.Sprintf("- %s: %s\n", name, msg))
		}
	}

	return sb.String()
}

// autoHealAction represents a safe remediation action.
type autoHealAction struct {
	Type        string `json:"type"`
	ContainerID string `json:"container_id,omitempty"`
}

type autoHealActions struct {
	Actions []autoHealAction `json:"actions"`
}

// safeActions is the whitelist of automatically executable actions.
var safeActions = map[string]bool{
	"restart_caddy":     true,
	"reload_caddy":      true,
	"restart_container": true,
}

// executeAutoHeal parses AI-recommended actions and executes safe ones.
func (sh *SelfHealEngine) executeAutoHeal(diagnosis string, ruleID uint, ruleName string) {
	// Extract JSON block from diagnosis
	actions := sh.parseActions(diagnosis)
	if len(actions) == 0 {
		sh.logger.Info("self-heal: no actionable remediation found", "rule", ruleName)
		return
	}

	var executed []string
	for _, action := range actions {
		if !safeActions[action.Type] {
			sh.logger.Warn("self-heal: skipping unsafe action", "type", action.Type, "rule", ruleName)
			continue
		}

		var err error
		switch action.Type {
		case "restart_caddy":
			err = sh.coreAPI.RestartCaddy()
		case "reload_caddy":
			err = sh.coreAPI.ReloadCaddy()
		case "restart_container":
			if action.ContainerID != "" {
				err = sh.coreAPI.DockerManageContainer(action.ContainerID, "restart")
			}
		}

		if err != nil {
			sh.logger.Error("self-heal action failed", "type", action.Type, "err", err)
		} else {
			executed = append(executed, action.Type)
			sh.logger.Info("self-heal action executed", "type", action.Type, "rule", ruleName)
		}
	}

	if len(executed) > 0 {
		now := time.Now()
		sh.lastHealTime = now // rate-limit future auto-heal

		sh.coreAPI.GetDB().Table("plugin_monitoring_alert_rules").
			Where("id = ?", ruleID).
			Update("last_heal_at", now)

		sh.eventBus.Publish(pluginpkg.Event{
			Type:   "system.selfheal.executed",
			Source: "ai",
			Payload: map[string]interface{}{
				"rule_id":   ruleID,
				"rule_name": ruleName,
				"actions":   executed,
			},
			Time: time.Now(),
		})
	}
}

// parseActions extracts auto-heal actions from AI diagnosis response.
func (sh *SelfHealEngine) parseActions(diagnosis string) []autoHealAction {
	// Find JSON block between ```json and ```
	start := strings.Index(diagnosis, "```json")
	if start == -1 {
		start = strings.Index(diagnosis, "```\n{")
	}
	if start == -1 {
		return nil
	}

	// Skip the opening marker
	jsonStart := strings.Index(diagnosis[start:], "\n")
	if jsonStart == -1 {
		return nil
	}
	rest := diagnosis[start+jsonStart+1:]

	end := strings.Index(rest, "```")
	if end == -1 {
		return nil
	}
	jsonStr := strings.TrimSpace(rest[:end])

	var result autoHealActions
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		sh.logger.Warn("self-heal: failed to parse actions JSON", "err", err)
		return nil
	}

	return result.Actions
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
