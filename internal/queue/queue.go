// Package queue provides an async task queue abstraction with Valkey (Redis-compatible) backend.
// Falls back to in-memory processing when Valkey is unavailable.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// TaskType identifies the kind of async task.
const (
	TypeBuild   = "deploy:build"
	TypePreview = "deploy:preview"
	TypeCleanup = "deploy:cleanup"
	TypeBackup  = "backup:run"
)

// Task represents an async task to be processed.
type Task struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Handler processes tasks of a given type.
type Handler func(ctx context.Context, payload json.RawMessage) error

// Queue manages async task processing with bounded concurrency.
type Queue struct {
	mu       sync.Mutex
	handlers map[string]Handler
	logger   *slog.Logger
	addr     string // Valkey address (empty = in-memory only)

	// Bounded worker pool to prevent goroutine leaks.
	sem      chan struct{}
	done     chan struct{}
	closed   atomic.Bool
	closeOnce sync.Once
}

// New creates a new Queue. If valkeyAddr is empty, uses in-memory mode.
// maxWorkers limits concurrent task goroutines (default 10).
func New(valkeyAddr string, logger *slog.Logger, maxWorkers ...int) *Queue {
	workers := 10
	if len(maxWorkers) > 0 && maxWorkers[0] > 0 {
		workers = maxWorkers[0]
	}

	q := &Queue{
		handlers: make(map[string]Handler),
		logger:   logger,
		addr:     valkeyAddr,
		sem:      make(chan struct{}, workers),
		done:     make(chan struct{}),
	}

	if valkeyAddr != "" {
		if pingValkey(valkeyAddr) {
			logger.Info("queue: Valkey reachable (future asynq integration)", "addr", valkeyAddr)
		} else {
			logger.Warn("queue: Valkey unreachable, using in-memory mode", "addr", valkeyAddr)
		}
	} else {
		logger.Info("queue: in-memory mode (no Valkey configured)")
	}

	return q
}

// Register registers a handler for a task type.
func (q *Queue) Register(taskType string, handler Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[taskType] = handler
}

// Enqueue submits a task for async processing with bounded concurrency.
func (q *Queue) Enqueue(taskType string, payload interface{}) error {
	if q.closed.Load() {
		return fmt.Errorf("queue is shut down")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	q.mu.Lock()
	handler, ok := q.handlers[taskType]
	q.mu.Unlock()

	if !ok {
		return fmt.Errorf("no handler registered for task type: %s", taskType)
	}

	// Acquire worker slot (blocks if pool is full).
	select {
	case q.sem <- struct{}{}:
	case <-q.done:
		return fmt.Errorf("queue is shut down")
	}

	go func() {
		defer func() { <-q.sem }() // release worker slot
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := handler(ctx, data); err != nil {
			q.logger.Error("task failed", "type", taskType, "err", err)
		}
	}()

	return nil
}

// Shutdown signals all workers to stop accepting new tasks. Safe to call multiple times.
func (q *Queue) Shutdown() {
	q.closeOnce.Do(func() {
		q.closed.Store(true)
		close(q.done)
	})
}

// IsValkeyConfigured returns whether a Valkey address was provided.
func (q *Queue) IsValkeyConfigured() bool {
	return q.addr != ""
}

// Status returns queue status information.
func (q *Queue) Status() map[string]interface{} {
	return map[string]interface{}{
		"mode":        "memory", // always memory until asynq is integrated
		"valkey_addr": q.addr,
		"workers":     cap(q.sem),
		"active":      len(q.sem),
	}
}

func pingValkey(addr string) bool {
	conn, err := dialTCP(addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
