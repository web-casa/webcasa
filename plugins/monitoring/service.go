package monitoring

import (
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// Service orchestrates metric collection, alerting, and WebSocket broadcasting.
type Service struct {
	db          *gorm.DB
	logger      *slog.Logger
	collector   *Collector
	alerter     *Alerter
	broadcaster *WSBroadcaster
	stopCh      chan struct{}
}

// NewService creates a new monitoring Service.
func NewService(db *gorm.DB, logger *slog.Logger) *Service {
	return &Service{
		db:          db,
		logger:      logger,
		collector:   NewCollector(logger),
		alerter:     NewAlerter(db, logger),
		broadcaster: NewWSBroadcaster(logger),
	}
}

// Start begins the periodic metric collection goroutine.
func (s *Service) Start() {
	s.stopCh = make(chan struct{})

	// Collection loop — every 60 seconds.
	go func() {
		// Collect immediately on start.
		s.collectOnce()

		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.collectOnce()
			case <-s.stopCh:
				return
			}
		}
	}()

	// Cleanup loop — every 24 hours, remove records older than 30 days.
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.cleanup()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop signals the background goroutines to exit.
func (s *Service) Stop() {
	if s.stopCh != nil {
		close(s.stopCh)
	}
}

// collectOnce performs a single collection cycle.
func (s *Service) collectOnce() {
	snap, err := s.collector.CollectSystem()
	if err != nil {
		s.logger.Error("collect system metrics", "err", err)
		return
	}

	// Persist to database.
	record := MetricRecord{
		Timestamp:      time.Now(),
		CPUPercent:     snap.CPUPercent,
		LoadAvg1:       snap.LoadAvg1,
		LoadAvg5:       snap.LoadAvg5,
		LoadAvg15:      snap.LoadAvg15,
		MemTotal:       snap.MemTotal,
		MemUsed:        snap.MemUsed,
		MemPercent:     snap.MemPercent,
		SwapTotal:      snap.SwapTotal,
		SwapUsed:       snap.SwapUsed,
		DiskTotal:      snap.DiskTotal,
		DiskUsed:       snap.DiskUsed,
		DiskPercent:    snap.DiskPercent,
		DiskReadBytes:  snap.DiskReadBytes,
		DiskWriteBytes: snap.DiskWriteBytes,
		NetRecvBytes:   snap.NetRecvBytes,
		NetSentBytes:   snap.NetSentBytes,
	}
	if err := s.db.Create(&record).Error; err != nil {
		s.logger.Error("persist metric record", "err", err)
	}

	// Evaluate alert rules.
	s.alerter.Evaluate(snap)

	// Broadcast to WebSocket clients.
	if s.broadcaster.HasClients() {
		containers, _ := s.collector.CollectContainers()
		s.broadcaster.Broadcast(snap, containers)
	}
}

// cleanup removes metric records older than 30 days.
func (s *Service) cleanup() {
	cutoff := time.Now().AddDate(0, 0, -30)
	result := s.db.Where("timestamp < ?", cutoff).Delete(&MetricRecord{})
	if result.Error != nil {
		s.logger.Error("cleanup old metrics", "err", result.Error)
	} else if result.RowsAffected > 0 {
		s.logger.Info("cleaned up old metrics", "deleted", result.RowsAffected)
	}
}

// ── Query Methods ──

// GetCurrent returns the latest metric snapshot.
func (s *Service) GetCurrent() (*MetricSnapshot, error) {
	snap, err := s.collector.CollectSystem()
	if err != nil {
		return nil, err
	}
	return snap, nil
}

// GetContainers returns current container metrics.
func (s *Service) GetContainers() ([]ContainerMetric, error) {
	return s.collector.CollectContainers()
}

// GetHistory returns historical metric records for the given period.
// Supports: 1h, 6h, 24h, 7d, 30d.
func (s *Service) GetHistory(period string) ([]MetricRecord, error) {
	var since time.Duration
	var sampleInterval time.Duration

	switch period {
	case "1h":
		since = 1 * time.Hour
		sampleInterval = 0 // all records
	case "6h":
		since = 6 * time.Hour
		sampleInterval = 0
	case "24h":
		since = 24 * time.Hour
		sampleInterval = 5 * time.Minute
	case "7d":
		since = 7 * 24 * time.Hour
		sampleInterval = 30 * time.Minute
	case "30d":
		since = 30 * 24 * time.Hour
		sampleInterval = 2 * time.Hour
	default:
		since = 1 * time.Hour
		sampleInterval = 0
	}

	cutoff := time.Now().Add(-since)

	var records []MetricRecord
	if err := s.db.Where("timestamp >= ?", cutoff).Order("timestamp ASC").Find(&records).Error; err != nil {
		return nil, err
	}

	// Downsample if needed.
	if sampleInterval > 0 && len(records) > 0 {
		records = downsample(records, sampleInterval)
	}

	return records, nil
}

// downsample picks one record per interval from the sorted slice.
func downsample(records []MetricRecord, interval time.Duration) []MetricRecord {
	if len(records) == 0 {
		return records
	}

	result := []MetricRecord{records[0]}
	lastTime := records[0].Timestamp

	for i := 1; i < len(records); i++ {
		if records[i].Timestamp.Sub(lastTime) >= interval {
			result = append(result, records[i])
			lastTime = records[i].Timestamp
		}
	}

	return result
}

// Broadcaster returns the WebSocket broadcaster.
func (s *Service) Broadcaster() *WSBroadcaster {
	return s.broadcaster
}

// ── Alert CRUD ──

// ListAlertRules returns all alert rules.
func (s *Service) ListAlertRules() ([]AlertRule, error) {
	var rules []AlertRule
	if err := s.db.Order("id ASC").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

// CreateAlertRule creates a new alert rule.
func (s *Service) CreateAlertRule(req *CreateAlertRequest) (*AlertRule, error) {
	rule := &AlertRule{
		Name:        req.Name,
		Metric:      req.Metric,
		Operator:    req.Operator,
		Threshold:   req.Threshold,
		Duration:    req.Duration,
		Enabled:     true,
		NotifyType:  req.NotifyType,
		NotifyURL:   req.NotifyURL,
		NotifyEmail: req.NotifyEmail,
		CooldownMin: req.CooldownMin,
	}

	if rule.Operator == "" {
		rule.Operator = ">"
	}
	if rule.Duration < 1 {
		rule.Duration = 1
	}
	if rule.NotifyType == "" {
		rule.NotifyType = "webhook"
	}
	if rule.CooldownMin < 1 {
		rule.CooldownMin = 30
	}

	if err := s.db.Create(rule).Error; err != nil {
		return nil, err
	}
	return rule, nil
}

// UpdateAlertRule updates an existing alert rule.
func (s *Service) UpdateAlertRule(id uint, req *UpdateAlertRequest) (*AlertRule, error) {
	var rule AlertRule
	if err := s.db.First(&rule, id).Error; err != nil {
		return nil, err
	}

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Metric != "" {
		updates["metric"] = req.Metric
	}
	if req.Operator != "" {
		updates["operator"] = req.Operator
	}
	if req.Threshold != 0 {
		updates["threshold"] = req.Threshold
	}
	if req.Duration > 0 {
		updates["duration"] = req.Duration
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.NotifyType != "" {
		updates["notify_type"] = req.NotifyType
	}
	if req.NotifyURL != "" {
		updates["notify_url"] = req.NotifyURL
	}
	if req.NotifyEmail != "" {
		updates["notify_email"] = req.NotifyEmail
	}
	if req.CooldownMin > 0 {
		updates["cooldown_min"] = req.CooldownMin
	}

	if len(updates) > 0 {
		if err := s.db.Model(&rule).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	if err := s.db.First(&rule, id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

// DeleteAlertRule removes an alert rule.
func (s *Service) DeleteAlertRule(id uint) error {
	return s.db.Delete(&AlertRule{}, id).Error
}

// ListAlertHistory returns alert trigger history.
func (s *Service) ListAlertHistory(limit int) ([]AlertHistory, error) {
	if limit <= 0 {
		limit = 50
	}
	var history []AlertHistory
	if err := s.db.Order("created_at DESC").Limit(limit).Find(&history).Error; err != nil {
		return nil, err
	}
	return history, nil
}
