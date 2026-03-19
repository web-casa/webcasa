package appstore

import (
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// Updater periodically syncs app sources and checks for updates.
type Updater struct {
	db       *gorm.DB
	sources  *SourceManager
	logger   *slog.Logger
	interval time.Duration
	stopCh   chan struct{}
}

// NewUpdater creates an Updater.
func NewUpdater(db *gorm.DB, sources *SourceManager, logger *slog.Logger) *Updater {
	return &Updater{
		db:       db,
		sources:  sources,
		logger:   logger,
		interval: 6 * time.Hour,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the background sync loop.
func (u *Updater) Start() {
	go func() {
		// Brief delay to let routes finish registering, then sync immediately
		select {
		case <-time.After(5 * time.Second):
		case <-u.stopCh:
			return
		}

		u.logger.Info("starting initial source sync")
		u.sources.SyncAllSources()

		ticker := time.NewTicker(u.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				u.logger.Info("periodic source sync")
				u.sources.SyncAllSources()
			case <-u.stopCh:
				return
			}
		}
	}()
}

// Stop terminates the background sync loop.
func (u *Updater) Stop() {
	select {
	case <-u.stopCh:
		// already closed
	default:
		close(u.stopCh)
	}
}
