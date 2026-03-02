package monitoring

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// WSBroadcaster manages WebSocket clients and broadcasts metric updates.
type WSBroadcaster struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
	logger  *slog.Logger
}

// NewWSBroadcaster creates a new WSBroadcaster.
func NewWSBroadcaster(logger *slog.Logger) *WSBroadcaster {
	return &WSBroadcaster{
		clients: make(map[*websocket.Conn]bool),
		logger:  logger,
	}
}

// AddClient registers a WebSocket connection.
func (b *WSBroadcaster) AddClient(conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clients[conn] = true
}

// RemoveClient unregisters a WebSocket connection.
func (b *WSBroadcaster) RemoveClient(conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients, conn)
}

// Broadcast sends a metrics update to all connected WebSocket clients.
func (b *WSBroadcaster) Broadcast(snap *MetricSnapshot, containers []ContainerMetric) {
	payload := map[string]interface{}{
		"system":     snap,
		"containers": containers,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		b.logger.Error("marshal ws payload", "err", err)
		return
	}

	b.mu.RLock()
	clients := make([]*websocket.Conn, 0, len(b.clients))
	for c := range b.clients {
		clients = append(clients, c)
	}
	b.mu.RUnlock()

	for _, conn := range clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			b.RemoveClient(conn)
			conn.Close()
		}
	}
}

// HasClients returns true if there are connected WebSocket clients.
func (b *WSBroadcaster) HasClients() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients) > 0
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		return strings.HasSuffix(origin, "://"+r.Host)
	},
}
