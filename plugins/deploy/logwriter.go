package deploy

import (
	"os"
	"sync"
)

// LogWriter writes build logs to a file and optionally broadcasts to subscribers.
type LogWriter struct {
	mu          sync.Mutex
	file        *os.File
	subscribers []chan []byte
}

// NewLogWriter creates a new LogWriter that writes to the given file path.
func NewLogWriter(path string) (*LogWriter, error) {
	if path == "" {
		return &LogWriter{}, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	return &LogWriter{file: f}, nil
}

// Write implements io.Writer.
func (lw *LogWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	// Write to file if open
	if lw.file != nil {
		if _, err := lw.file.Write(p); err != nil {
			return 0, err
		}
	}

	// Broadcast to subscribers (non-blocking)
	data := make([]byte, len(p))
	copy(data, p)
	for _, ch := range lw.subscribers {
		select {
		case ch <- data:
		default: // drop if subscriber is slow
		}
	}

	return len(p), nil
}

// Subscribe returns a channel that receives log data.
func (lw *LogWriter) Subscribe() chan []byte {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	ch := make(chan []byte, 64)
	lw.subscribers = append(lw.subscribers, ch)
	return ch
}

// Unsubscribe removes a subscriber channel.
func (lw *LogWriter) Unsubscribe(ch chan []byte) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	for i, sub := range lw.subscribers {
		if sub == ch {
			lw.subscribers = append(lw.subscribers[:i], lw.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// Close closes the underlying file.
func (lw *LogWriter) Close() error {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	// Close all subscriber channels
	for _, ch := range lw.subscribers {
		close(ch)
	}
	lw.subscribers = nil
	if lw.file != nil {
		return lw.file.Close()
	}
	return nil
}
