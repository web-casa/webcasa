package monitoring

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/web-casa/webcasa/internal/notify"
	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

// Alerter evaluates alert rules against collected metrics.
type Alerter struct {
	db         *gorm.DB
	logger     *slog.Logger
	eventBus   *pluginpkg.EventBus
	mu         sync.Mutex
	violations map[uint]int // ruleID -> consecutive violation count
}

// NewAlerter creates a new Alerter.
func NewAlerter(db *gorm.DB, logger *slog.Logger, eventBus *pluginpkg.EventBus) *Alerter {
	return &Alerter{
		db:         db,
		logger:     logger,
		eventBus:   eventBus,
		violations: make(map[uint]int),
	}
}

// Evaluate checks all enabled alert rules against the current metric snapshot.
func (a *Alerter) Evaluate(snap *MetricSnapshot) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var rules []AlertRule
	if err := a.db.Where("enabled = ?", true).Find(&rules).Error; err != nil {
		a.logger.Error("load alert rules", "err", err)
		return
	}

	for _, rule := range rules {
		value := a.extractMetric(snap, rule.Metric)
		violated := a.checkThreshold(value, rule.Operator, rule.Threshold)

		if violated {
			a.violations[rule.ID]++
		} else {
			a.violations[rule.ID] = 0
			continue
		}

		// Check if consecutive violations reach the duration requirement.
		if a.violations[rule.ID] < rule.Duration {
			continue
		}

		// Check cooldown.
		if !rule.LastFiredAt.IsZero() && time.Since(rule.LastFiredAt) < time.Duration(rule.CooldownMin)*time.Minute {
			continue
		}

		// Fire alert.
		msg := fmt.Sprintf("%s: %s %.2f %s %.2f (actual: %.2f)",
			rule.Name, rule.Metric, rule.Threshold, rule.Operator, rule.Threshold, value)

		history := AlertHistory{
			RuleID:    rule.ID,
			RuleName:  rule.Name,
			Metric:    rule.Metric,
			Value:     value,
			Threshold: rule.Threshold,
			Message:   msg,
		}

		// Send notification.
		switch rule.NotifyType {
		case "webhook":
			if rule.NotifyURL != "" {
				if err := a.sendWebhook(rule.NotifyURL, &history); err != nil {
					a.logger.Error("webhook failed", "rule", rule.Name, "err", err)
				} else {
					history.Notified = true
				}
			}
		}

		// Save history.
		a.db.Create(&history)

		// Update last fired time.
		a.db.Model(&AlertRule{}).Where("id = ?", rule.ID).Update("last_fired_at", time.Now())

		// Reset violation counter.
		a.violations[rule.ID] = 0

		a.logger.Warn("alert fired", "rule", rule.Name, "metric", rule.Metric, "value", value)

		// Publish event for self-heal and notification integrations.
		if a.eventBus != nil {
			a.eventBus.Publish(pluginpkg.Event{
				Type:   "monitoring.alert.fired",
				Source: "monitoring",
				Payload: map[string]interface{}{
					"rule_id":        rule.ID,
					"rule_name":      rule.Name,
					"metric":         rule.Metric,
					"value":          value,
					"threshold":      rule.Threshold,
					"auto_heal_mode": rule.AutoHealMode,
				},
				Time: time.Now(),
			})
		}
	}
}

// extractMetric returns the value of a named metric from the snapshot.
func (a *Alerter) extractMetric(snap *MetricSnapshot, metric string) float64 {
	switch metric {
	case "cpu_percent":
		return snap.CPUPercent
	case "mem_percent":
		return snap.MemPercent
	case "disk_percent":
		return snap.DiskPercent
	case "load_avg_1":
		return snap.LoadAvg1
	case "load_avg_5":
		return snap.LoadAvg5
	case "load_avg_15":
		return snap.LoadAvg15
	default:
		return 0
	}
}

// checkThreshold evaluates whether the value violates the threshold.
func (a *Alerter) checkThreshold(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	default:
		return value > threshold
	}
}

// sendWebhook posts alert info to a webhook URL.
func (a *Alerter) sendWebhook(url string, alert *AlertHistory) error {
	payload := map[string]interface{}{
		"rule_name": alert.RuleName,
		"metric":    alert.Metric,
		"value":     alert.Value,
		"threshold": alert.Threshold,
		"message":   alert.Message,
		"time":      time.Now().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// SSRF guard: admin-configured webhook URLs can accidentally (or
	// intentionally) target loopback / metadata endpoints. Validate the
	// literal URL and also re-validate at dial time via SafeDialContext
	// to catch DNS rebinding. Mirrors internal/notify/notifier.go.
	if err := notify.ValidateWebhookURL(url); err != nil {
		return fmt.Errorf("alert webhook URL blocked: %w", err)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		// Redirects to internal IPs would bypass the URL-level SSRF check.
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{DialContext: notify.SafeDialContext},
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
