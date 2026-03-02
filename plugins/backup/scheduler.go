package backup

import (
	"log/slog"
	"sync"

	"github.com/robfig/cron/v3"
)

// Scheduler manages cron-based scheduled backups.
type Scheduler struct {
	cron    *cron.Cron
	entryID cron.EntryID
	logger  *slog.Logger
	mu      sync.Mutex
	onTick  func() // callback to run when cron fires
}

// NewScheduler creates a new backup Scheduler.
func NewScheduler(logger *slog.Logger) *Scheduler {
	return &Scheduler{
		cron:   cron.New(),
		logger: logger,
	}
}

// Start begins the cron scheduler.
func (s *Scheduler) Start() {
	s.cron.Start()
}

// Stop shuts down the cron scheduler.
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// SetCallback sets the function to call when the cron job fires.
func (s *Scheduler) SetCallback(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTick = fn
}

// UpdateSchedule replaces the current cron schedule.
// Pass enabled=false to remove the schedule without adding a new one.
func (s *Scheduler) UpdateSchedule(cronExpr string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing entry.
	if s.entryID != 0 {
		s.cron.Remove(s.entryID)
		s.entryID = 0
	}

	if !enabled || cronExpr == "" {
		return nil
	}

	id, err := s.cron.AddFunc(cronExpr, func() {
		s.mu.Lock()
		fn := s.onTick
		s.mu.Unlock()
		if fn != nil {
			fn()
		}
	})
	if err != nil {
		return err
	}

	s.entryID = id
	s.logger.Info("backup schedule updated", "cron", cronExpr)
	return nil
}

// NextRun returns the next scheduled run time, or nil if no schedule is active.
func (s *Scheduler) NextRun() *cron.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entryID == 0 {
		return nil
	}
	entry := s.cron.Entry(s.entryID)
	return &entry
}
