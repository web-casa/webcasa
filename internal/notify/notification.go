package notify

import "time"

// Notification is the interface for structured notification events.
// Each event type implements formatting methods for each delivery channel.
type Notification interface {
	EventType() string          // e.g., "deploy.build.success", "backup.complete"
	Title() string              // short title for the notification
	Message() string            // detailed message body
	Time() time.Time            // when the event occurred
	Severity() string           // info, warning, error
	ToWebhookPayload() map[string]interface{}
}

// BaseNotification provides a default implementation of the Notification interface.
type BaseNotification struct {
	Type     string    `json:"type"`
	Ttl      string    `json:"title"`
	Msg      string    `json:"message"`
	At       time.Time `json:"time"`
	Sev      string    `json:"severity"`
}

func (n *BaseNotification) EventType() string  { return n.Type }
func (n *BaseNotification) Title() string      { return n.Ttl }
func (n *BaseNotification) Message() string    { return n.Msg }
func (n *BaseNotification) Time() time.Time    { return n.At }
func (n *BaseNotification) Severity() string   { return n.Sev }

func (n *BaseNotification) ToWebhookPayload() map[string]interface{} {
	return map[string]interface{}{
		"type":     n.Type,
		"title":    n.Ttl,
		"message":  n.Msg,
		"time":     n.At.Format(time.RFC3339),
		"severity": n.Sev,
	}
}

// NewNotification creates a BaseNotification with the given parameters.
func NewNotification(eventType, title, message, severity string) *BaseNotification {
	return &BaseNotification{
		Type: eventType,
		Ttl:  title,
		Msg:  message,
		At:   time.Now(),
		Sev:  severity,
	}
}
