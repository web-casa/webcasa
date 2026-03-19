package cronjob

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

const maxOutputBytes = 64 * 1024 // 64KB

// Service manages cron job lifecycle, scheduling, and execution.
type Service struct {
	db         *gorm.DB
	logger     *slog.Logger
	eventBus   *pluginpkg.EventBus
	runner     *cron.Cron
	mu         sync.Mutex
	entries    map[uint]cron.EntryID // taskID → cron entryID
	running    map[uint]bool         // prevent concurrent execution per task
	runMu      sync.Mutex
	stopCh     chan struct{}
	subscribed bool // guard: EventBus handlers registered only once
}

// NewService creates a new cron job service.
func NewService(db *gorm.DB, logger *slog.Logger, eventBus *pluginpkg.EventBus) *Service {
	return &Service{
		db:       db,
		logger:   logger,
		eventBus: eventBus,
		runner:   cron.New(),
		entries:  make(map[uint]cron.EntryID),
		running:  make(map[uint]bool),
		stopCh:   make(chan struct{}),
	}
}

// Start loads enabled tasks, starts the scheduler, subscribes to events, and begins log cleanup.
func (s *Service) Start() {
	// Reset scheduler state for safe restart (disable → enable cycle).
	s.mu.Lock()
	s.runner = cron.New()
	s.entries = make(map[uint]cron.EntryID)
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	// Reset concurrent-execution guard so tasks aren't permanently "skipped" after restart.
	s.runMu.Lock()
	s.running = make(map[uint]bool)
	s.runMu.Unlock()

	var tasks []CronTask
	s.db.Where("enabled = ?", true).Find(&tasks)

	for _, task := range tasks {
		s.addToScheduler(task)
	}

	s.runner.Start()
	s.logger.Info("cron job scheduler started", "tasks", len(tasks))

	// Subscribe to EventBus ONLY ONCE (EventBus has no Unsubscribe, so guard with flag).
	if s.eventBus != nil && !s.subscribed {
		s.subscribed = true

		s.eventBus.Subscribe("cronjob.trigger", func(e pluginpkg.Event) {
			// Guard: ignore events if scheduler is stopped.
			select {
			case <-s.stopCh:
				return
			default:
			}

			if taskID, ok := toUint(e.Payload["task_id"]); ok {
				go s.TriggerTask(taskID)
			}
			if tag, ok := e.Payload["tag"].(string); ok && tag != "" {
				tasks, _ := s.listTasksByExactTag(tag)
				for _, t := range tasks {
					go s.TriggerTask(t.ID)
				}
			}
		})

		// Subscribe to reload events (triggered when CoreAPI creates/updates/deletes tasks).
		s.eventBus.Subscribe("cronjob.reload", func(e pluginpkg.Event) {
			select {
			case <-s.stopCh:
				return
			default:
			}

			taskID, ok := toUint(e.Payload["task_id"])
			if !ok {
				return
			}
			action, _ := e.Payload["action"].(string)

			switch action {
			case "delete":
				s.removeFromScheduler(taskID)
			default: // "create", "update"
				s.removeFromScheduler(taskID)
				var task CronTask
				if err := s.db.First(&task, taskID).Error; err == nil && task.Enabled {
					s.addToScheduler(task)
				}
				s.updateNextRunAt(taskID)
			}
		})
	}

	// Background log cleanup (every 24 hours, delete logs older than 30 days).
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cutoff := time.Now().AddDate(0, 0, -30)
				s.db.Where("started_at < ?", cutoff).Delete(&CronLog{})
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop stops the scheduler and cleanup goroutine.
func (s *Service) Stop() {
	s.runner.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.stopCh:
		// Already closed — nothing to do.
	default:
		close(s.stopCh)
	}
}

// ── CRUD ──

// ListTasks returns all tasks, optionally filtered by tag.
func (s *Service) ListTasks(tag string) ([]CronTask, error) {
	var tasks []CronTask
	q := s.db.Order("id ASC")
	if tag != "" {
		q = q.Where("tags LIKE ?", fmt.Sprintf(`%%"%s"%%`, tag))
	}
	if err := q.Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return tasks, nil
}

// listTasksByExactTag returns tasks that have an exact tag match (not partial LIKE).
func (s *Service) listTasksByExactTag(tag string) ([]CronTask, error) {
	var all []CronTask
	if err := s.db.Where("enabled = ?", true).Find(&all).Error; err != nil {
		return nil, err
	}
	var matched []CronTask
	for _, t := range all {
		for _, tt := range t.GetTags() {
			if tt == tag {
				matched = append(matched, t)
				break
			}
		}
	}
	return matched, nil
}

// GetTask returns a single task by ID.
func (s *Service) GetTask(id uint) (*CronTask, error) {
	var task CronTask
	if err := s.db.First(&task, id).Error; err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}
	return &task, nil
}

// CreateTask creates a new cron task and registers it with the scheduler.
func (s *Service) CreateTask(req *CreateTaskRequest) (*CronTask, error) {
	// Validate cron expression.
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if _, err := parser.Parse(req.Expression); err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	task := CronTask{
		Name:            req.Name,
		Expression:      req.Expression,
		Command:         req.Command,
		WorkingDir:      req.WorkingDir,
		Enabled:         true,
		TimeoutSec:      req.TimeoutSec,
		MaxRetries:      req.MaxRetries,
		NotifyOnFailure: req.NotifyOnFailure,
	}
	if req.Enabled != nil {
		task.Enabled = *req.Enabled
	}
	if task.TimeoutSec <= 0 {
		task.TimeoutSec = 300
	}
	if task.TimeoutSec > 86400 {
		task.TimeoutSec = 86400 // max 24 hours
	}
	if task.MaxRetries < 0 {
		task.MaxRetries = 0
	}
	if task.MaxRetries > 10 {
		task.MaxRetries = 10
	}
	task.SetTags(req.Tags)

	if err := s.db.Create(&task).Error; err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	if task.Enabled {
		s.addToScheduler(task)
	}
	s.updateNextRunAt(task.ID)

	return &task, nil
}

// UpdateTask updates an existing task and re-registers it with the scheduler.
func (s *Service) UpdateTask(id uint, req *UpdateTaskRequest) (*CronTask, error) {
	task, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}

	if req.Expression != nil {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		if _, err := parser.Parse(*req.Expression); err != nil {
			return nil, fmt.Errorf("invalid cron expression: %w", err)
		}
		task.Expression = *req.Expression
	}
	if req.Name != nil {
		task.Name = *req.Name
	}
	if req.Command != nil {
		task.Command = *req.Command
	}
	if req.WorkingDir != nil {
		task.WorkingDir = *req.WorkingDir
	}
	if req.Enabled != nil {
		task.Enabled = *req.Enabled
	}
	if req.Tags != nil {
		task.SetTags(req.Tags)
	}
	if req.TimeoutSec != nil {
		task.TimeoutSec = *req.TimeoutSec
		if task.TimeoutSec <= 0 {
			task.TimeoutSec = 300
		}
		if task.TimeoutSec > 86400 {
			task.TimeoutSec = 86400
		}
	}
	if req.MaxRetries != nil {
		task.MaxRetries = *req.MaxRetries
		if task.MaxRetries < 0 {
			task.MaxRetries = 0
		}
		if task.MaxRetries > 10 {
			task.MaxRetries = 10
		}
	}
	if req.NotifyOnFailure != nil {
		task.NotifyOnFailure = *req.NotifyOnFailure
	}

	if err := s.db.Save(task).Error; err != nil {
		return nil, fmt.Errorf("update task: %w", err)
	}

	// Re-register with scheduler.
	s.removeFromScheduler(task.ID)
	if task.Enabled {
		s.addToScheduler(*task)
	}
	s.updateNextRunAt(task.ID)

	return task, nil
}

// DeleteTask removes a task and its logs.
func (s *Service) DeleteTask(id uint) error {
	s.removeFromScheduler(id)
	s.db.Where("task_id = ?", id).Delete(&CronLog{})
	result := s.db.Delete(&CronTask{}, id)
	if result.Error != nil {
		return fmt.Errorf("delete task: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("task not found: %d", id)
	}
	return nil
}

// TriggerTask runs a task immediately, regardless of schedule.
func (s *Service) TriggerTask(id uint) (*CronLog, error) {
	return s.executeTask(id, true)
}

// ── Logs ──

// ListLogs returns execution logs for a specific task.
func (s *Service) ListLogs(taskID uint, limit int) ([]CronLog, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var logs []CronLog
	if err := s.db.Where("task_id = ?", taskID).Order("id DESC").Limit(limit).Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("list logs: %w", err)
	}
	return logs, nil
}

// ListAllLogs returns recent logs across all tasks.
func (s *Service) ListAllLogs(limit int) ([]CronLog, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var logs []CronLog
	if err := s.db.Order("id DESC").Limit(limit).Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("list all logs: %w", err)
	}
	return logs, nil
}

// ── Scheduler internals ──

func (s *Service) addToScheduler(task CronTask) {
	s.mu.Lock()
	defer s.mu.Unlock()

	taskID := task.ID
	entryID, err := s.runner.AddFunc(task.Expression, func() {
		s.executeTask(taskID, false)
	})
	if err != nil {
		s.logger.Warn("failed to schedule cron task",
			"task_id", task.ID, "name", task.Name,
			"expression", task.Expression, "err", err)
		return
	}
	s.entries[task.ID] = entryID
}

func (s *Service) removeFromScheduler(taskID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.entries[taskID]; ok {
		s.runner.Remove(entryID)
		delete(s.entries, taskID)
	}
}

func (s *Service) updateNextRunAt(taskID uint) {
	s.mu.Lock()
	entryID, ok := s.entries[taskID]
	s.mu.Unlock()

	if !ok {
		s.db.Model(&CronTask{}).Where("id = ?", taskID).Update("next_run_at", nil)
		return
	}

	entry := s.runner.Entry(entryID)
	if !entry.Next.IsZero() {
		s.db.Model(&CronTask{}).Where("id = ?", taskID).Update("next_run_at", entry.Next)
	}
}

// executeTask runs the task's command with timeout and records the result.
func (s *Service) executeTask(taskID uint, isManual bool) (*CronLog, error) {
	var task CronTask
	if err := s.db.First(&task, taskID).Error; err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	if !task.Enabled && !isManual {
		return nil, nil
	}

	// Prevent concurrent execution of the same task.
	s.runMu.Lock()
	if s.running[taskID] {
		s.runMu.Unlock()
		logEntry := &CronLog{
			TaskID:     taskID,
			TaskName:   task.Name,
			StartedAt:  time.Now(),
			FinishedAt: time.Now(),
			Status:     "skipped",
			Output:     "previous execution still running",
		}
		s.db.Create(logEntry)
		return logEntry, nil
	}
	s.running[taskID] = true
	s.runMu.Unlock()
	defer func() {
		s.runMu.Lock()
		delete(s.running, taskID)
		s.runMu.Unlock()
	}()

	return s.runCommand(task)
}

func (s *Service) runCommand(task CronTask) (*CronLog, error) {
	timeout := time.Duration(task.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	var lastLog *CronLog
	attempts := 1 + task.MaxRetries
	if attempts > 10 {
		attempts = 10 // hard cap on retries
	}

	for attempt := 0; attempt < attempts; attempt++ {
		// Wait before retry (skip wait on first attempt).
		if attempt > 0 {
			retryDelay := time.Duration(attempt*2) * time.Second
			time.Sleep(retryDelay)
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		cmd := exec.CommandContext(ctx, "bash", "-c", task.Command)
		if task.WorkingDir != "" {
			cmd.Dir = task.WorkingDir
		}

		// Use a bounded buffer to prevent OOM on large outputs.
		output := &boundedBuffer{max: maxOutputBytes}
		cmd.Stdout = output
		cmd.Stderr = output

		startedAt := time.Now()
		err := cmd.Run()
		finishedAt := time.Now()
		cancel()

		exitCode := 0
		status := "success"
		if ctx.Err() == context.DeadlineExceeded {
			status = "timeout"
			exitCode = -1
		} else if err != nil {
			status = "failed"
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}

		outStr := output.String()

		lastLog = &CronLog{
			TaskID:     task.ID,
			TaskName:   task.Name,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			ExitCode:   exitCode,
			Output:     outStr,
			Status:     status,
			DurationMs: finishedAt.Sub(startedAt).Milliseconds(),
		}
		s.db.Create(lastLog)

		// Update task state.
		now := time.Now()
		s.db.Model(&CronTask{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
			"last_run_at": now,
			"last_status": status,
		})
		s.updateNextRunAt(task.ID)

		// Publish event.
		payload := map[string]interface{}{
			"task_id":     task.ID,
			"task_name":   task.Name,
			"status":      status,
			"exit_code":   exitCode,
			"duration_ms": lastLog.DurationMs,
		}
		if s.eventBus != nil {
			s.eventBus.Publish(pluginpkg.Event{
				Type:    "cronjob.task.executed",
				Source:  "cronjob",
				Payload: payload,
			})
		}

		if status == "success" {
			break
		}

		// On final failure, publish failure event.
		if attempt == attempts-1 {
			if s.eventBus != nil {
				tailOutput := outStr
				if len(tailOutput) > 1024 {
					tailOutput = tailOutput[len(tailOutput)-1024:]
				}
				s.eventBus.Publish(pluginpkg.Event{
					Type:   "cronjob.task.failed",
					Source: "cronjob",
					Payload: map[string]interface{}{
						"task_id":     task.ID,
						"task_name":   task.Name,
						"exit_code":   exitCode,
						"output_tail": tailOutput,
					},
				})
			}
		}
	}

	return lastLog, nil
}

// boundedBuffer is a writer that keeps only the last `max` bytes.
type boundedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n, err := b.buf.Write(p)
	// If buffer exceeds max, trim the head.
	if b.buf.Len() > b.max {
		data := b.buf.Bytes()
		keep := data[len(data)-b.max:]
		// Skip to the next valid UTF-8 boundary to avoid cut runes.
		for i, r := range string(keep) {
			if r != 0xFFFD {
				keep = keep[i:]
				break
			}
		}
		b.buf.Reset()
		b.buf.Write(keep)
	}
	return n, err
}

func (b *boundedBuffer) String() string {
	return b.buf.String()
}

// toUint converts various numeric types to uint.
func toUint(v interface{}) (uint, bool) {
	switch n := v.(type) {
	case float64:
		return uint(n), true
	case int:
		return uint(n), true
	case uint:
		return n, true
	case int64:
		return uint(n), true
	case uint64:
		return uint(n), true
	default:
		return 0, false
	}
}
