package plugin

import (
	"log/slog"
	"sync/atomic"
	"testing"
)

func TestEventBusPublishSubscribe(t *testing.T) {
	eb := NewEventBus(slog.Default())

	var called int32
	eb.Subscribe("test.event", func(e Event) {
		atomic.AddInt32(&called, 1)
		if e.Type != "test.event" {
			t.Errorf("expected type test.event, got %s", e.Type)
		}
		if e.Payload["key"] != "value" {
			t.Errorf("expected payload key=value, got %v", e.Payload["key"])
		}
	})

	eb.Publish(Event{
		Type:    "test.event",
		Payload: map[string]interface{}{"key": "value"},
		Source:  "test",
	})

	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("handler was not called")
	}
}

func TestEventBusWildcard(t *testing.T) {
	eb := NewEventBus(slog.Default())

	var count int32
	eb.Subscribe("*", func(e Event) {
		atomic.AddInt32(&count, 1)
	})

	eb.Publish(Event{Type: "a"})
	eb.Publish(Event{Type: "b"})

	if atomic.LoadInt32(&count) != 2 {
		t.Fatalf("expected wildcard handler called 2 times, got %d", count)
	}
}

func TestEventBusPanicRecovery(t *testing.T) {
	eb := NewEventBus(slog.Default())

	var secondCalled int32

	// First handler panics.
	eb.Subscribe("crash", func(e Event) {
		panic("boom")
	})

	// Second handler should still run.
	eb.Subscribe("crash", func(e Event) {
		atomic.AddInt32(&secondCalled, 1)
	})

	// Should not panic.
	eb.Publish(Event{Type: "crash"})

	if atomic.LoadInt32(&secondCalled) != 1 {
		t.Fatal("second handler should have been called despite first handler panicking")
	}
}

func TestEventBusNoSubscribers(t *testing.T) {
	eb := NewEventBus(slog.Default())
	// Should not panic.
	eb.Publish(Event{Type: "nobody.listens"})
}
