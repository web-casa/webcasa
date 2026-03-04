package deploy

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// CronScheduler manages scheduled tasks for deploy projects.
type CronScheduler struct {
	db     *gorm.DB
	logger *slog.Logger
	runner *cron.Cron
	// Map from CronJob.ID → cron.EntryID for dynamic add/remove.
	mu       sync.Mutex
	entries  map[uint]cron.EntryID
	dataDir  string
}

// NewCronScheduler creates a new cron scheduler.
func NewCronScheduler(db *gorm.DB, logger *slog.Logger, dataDir string) *CronScheduler {
	return &CronScheduler{
		db:      db,
		logger:  logger,
		runner:  cron.New(cron.WithSeconds()),
		entries: make(map[uint]cron.EntryID),
		dataDir: dataDir,
	}
}

// Start loads all enabled cron jobs from DB and starts the scheduler.
func (cs *CronScheduler) Start() {
	var jobs []CronJob
	cs.db.Where("enabled = ?", true).Find(&jobs)

	for _, job := range jobs {
		cs.addJob(job)
	}

	cs.runner.Start()
	cs.logger.Info("cron scheduler started", "jobs", len(jobs))
}

// Stop stops the scheduler.
func (cs *CronScheduler) Stop() {
	cs.runner.Stop()
}

// AddJob registers a cron job with the scheduler.
func (cs *CronScheduler) AddJob(job CronJob) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Remove existing entry if any.
	if entryID, ok := cs.entries[job.ID]; ok {
		cs.runner.Remove(entryID)
		delete(cs.entries, job.ID)
	}

	if job.Enabled {
		cs.addJob(job)
	}
}

// RemoveJob removes a cron job from the scheduler.
func (cs *CronScheduler) RemoveJob(jobID uint) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if entryID, ok := cs.entries[jobID]; ok {
		cs.runner.Remove(entryID)
		delete(cs.entries, jobID)
	}
}

// addJob is the internal method that adds a job to the runner (caller must hold lock or call during init).
func (cs *CronScheduler) addJob(job CronJob) {
	// robfig/cron with WithSeconds expects 6 fields; standard 5-field crons need prefix "0 ".
	schedule := job.Schedule
	if len(schedule) > 0 {
		// Count fields: if 5, prefix with "0" for second=0.
		parts := 0
		inSpace := true
		for _, ch := range schedule {
			if ch == ' ' || ch == '\t' {
				inSpace = true
			} else if inSpace {
				parts++
				inSpace = false
			}
		}
		if parts == 5 {
			schedule = "0 " + schedule
		}
	}

	entryID, err := cs.runner.AddFunc(schedule, func() {
		cs.executeJob(job.ID)
	})
	if err != nil {
		cs.logger.Warn("failed to schedule cron job", "job_id", job.ID, "name", job.Name, "schedule", job.Schedule, "err", err)
		return
	}
	cs.entries[job.ID] = entryID
}

// executeJob runs the cron job command in the project's source directory.
func (cs *CronScheduler) executeJob(jobID uint) {
	var job CronJob
	if err := cs.db.First(&job, jobID).Error; err != nil {
		cs.logger.Error("cron job not found", "job_id", jobID, "err", err)
		return
	}

	if !job.Enabled {
		return
	}

	// Determine working directory from project source dir.
	workDir := fmt.Sprintf("%s/sources/project_%d", cs.dataDir, job.ProjectID)

	cs.logger.Info("executing cron job", "job_id", job.ID, "name", job.Name, "command", job.Command)

	cmd := exec.Command("bash", "-c", job.Command)
	cmd.Dir = workDir

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	now := time.Now()
	err := cmd.Run()

	status := "success"
	if err != nil {
		status = "failed"
		cs.logger.Warn("cron job failed", "job_id", job.ID, "name", job.Name, "err", err, "output", output.String())
	}

	cs.db.Model(&CronJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"last_run_at": now,
		"last_status": status,
	})
}
