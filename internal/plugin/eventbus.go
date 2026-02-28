package plugin

import (
	"log/slog"
	"sync"
	"time"
)

// Event represents something that happened in the system.
type Event struct {
	Type    string                 `json:"type"`    // e.g. "host.created", "deploy.failed"
	Payload map[string]interface{} `json:"payload"` // event-specific data
	Source  string                 `json:"source"`  // originating plugin ID or "core"
	Time    time.Time              `json:"time"`
}

// EventHandler is a callback that processes an event.
type EventHandler func(event Event)

// EventBus is an in-memory publish/subscribe event bus.
type EventBus struct {
	mu       sync.RWMutex
	handlers map[string][]EventHandler
	logger   *slog.Logger
}

// NewEventBus creates a new EventBus.
func NewEventBus(logger *slog.Logger) *EventBus {
	return &EventBus{
		handlers: make(map[string][]EventHandler),
		logger:   logger,
	}
}

// Subscribe registers a handler for the given event type.
// Use "*" to subscribe to all events.
func (eb *EventBus) Subscribe(eventType string, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.handlers[eventType] = append(eb.handlers[eventType], handler)
}

// Publish dispatches an event to all matching subscribers.
// Handlers are invoked synchronously in registration order.
// A panicking handler is recovered and logged without affecting others.
func (eb *EventBus) Publish(event Event) {
	if event.Time.IsZero() {
		event.Time = time.Now()
	}

	eb.mu.RLock()
	// Collect handlers: specific + wildcard.
	handlers := make([]EventHandler, 0, len(eb.handlers[event.Type])+len(eb.handlers["*"]))
	handlers = append(handlers, eb.handlers[event.Type]...)
	handlers = append(handlers, eb.handlers["*"]...)
	eb.mu.RUnlock()

	for _, h := range handlers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					eb.logger.Error("event handler panicked",
						"event", event.Type,
						"source", event.Source,
						"panic", r,
					)
				}
			}()
			h(event)
		}()
	}
}
