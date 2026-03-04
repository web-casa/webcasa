package notify

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Notifier dispatches notifications to configured channels.
type Notifier struct {
	db     *gorm.DB
	logger *slog.Logger
}

// NewNotifier creates a new Notifier.
func NewNotifier(db *gorm.DB, logger *slog.Logger) *Notifier {
	return &Notifier{db: db, logger: logger}
}

// Send dispatches a notification event to all enabled channels that match the event type.
func (n *Notifier) Send(event NotifyEvent) {
	var channels []Channel
	if err := n.db.Where("enabled = ?", true).Find(&channels).Error; err != nil {
		n.logger.Error("load notify channels", "err", err)
		return
	}

	for _, ch := range channels {
		if !n.matchesEvent(ch, event.Type) {
			continue
		}

		go func(ch Channel) {
			var err error
			switch ch.Type {
			case "webhook":
				err = n.sendWebhook(ch, event)
			case "email":
				err = n.sendEmail(ch, event)
			case "discord":
				err = n.sendDiscord(ch, event)
			case "telegram":
				err = n.sendTelegram(ch, event)
			}
			if err != nil {
				n.logger.Error("notification failed", "channel", ch.Name, "type", ch.Type, "err", err)
			} else {
				n.logger.Info("notification sent", "channel", ch.Name, "event", event.Type)
			}
		}(ch)
	}
}

// matchesEvent checks if a channel's event patterns match the given event type.
func (n *Notifier) matchesEvent(ch Channel, eventType string) bool {
	if ch.Events == "" {
		return true // no filter = match all
	}

	var patterns []string
	if err := json.Unmarshal([]byte(ch.Events), &patterns); err != nil {
		return true // malformed = match all
	}

	for _, pattern := range patterns {
		if pattern == "*" || pattern == eventType {
			return true
		}
		// Wildcard matching: "deploy.*" matches "deploy.build.failed"
		if strings.HasSuffix(pattern, ".*") {
			prefix := strings.TrimSuffix(pattern, ".*")
			if strings.HasPrefix(eventType, prefix+".") {
				return true
			}
		}
	}
	return false
}

// sendWebhook posts the event to a webhook URL.
func (n *Notifier) sendWebhook(ch Channel, event NotifyEvent) error {
	var cfg WebhookConfig
	if err := json.Unmarshal([]byte(ch.Config), &cfg); err != nil {
		return fmt.Errorf("parse webhook config: %w", err)
	}
	if cfg.URL == "" {
		return fmt.Errorf("webhook URL is empty")
	}

	body, err := json.Marshal(event)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "WebCasa-Notifier/1.0")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// sendEmail sends an email notification via SMTP.
func (n *Notifier) sendEmail(ch Channel, event NotifyEvent) error {
	var cfg EmailConfig
	if err := json.Unmarshal([]byte(ch.Config), &cfg); err != nil {
		return fmt.Errorf("parse email config: %w", err)
	}
	if cfg.SMTPHost == "" || cfg.To == "" {
		return fmt.Errorf("email config incomplete")
	}

	from := cfg.From
	if from == "" {
		from = cfg.Username
	}

	subject := fmt.Sprintf("[Web.Casa] %s", event.Title)
	body := fmt.Sprintf("Event: %s\nTime: %s\n\n%s",
		event.Type,
		event.Time.Format("2006-01-02 15:04:05"),
		event.Message,
	)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, cfg.To, subject, body)

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)

	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)
	}

	recipients := strings.Split(cfg.To, ",")
	for i := range recipients {
		recipients[i] = strings.TrimSpace(recipients[i])
	}

	if cfg.UseTLS {
		return n.sendEmailTLS(addr, auth, from, recipients, []byte(msg), cfg.SMTPHost)
	}
	return smtp.SendMail(addr, auth, from, recipients, []byte(msg))
}

// sendEmailTLS sends email over TLS.
func (n *Notifier) sendEmailTLS(addr string, auth smtp.Auth, from string, to []string, msg []byte, host string) error {
	tlsConfig := &tls.Config{ServerName: host}
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if auth != nil {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}

	if err = client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err = client.Rcpt(recipient); err != nil {
			return err
		}
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = w.Write(msg)
	if err != nil {
		return err
	}
	return w.Close()
}

// sendDiscord posts the event to a Discord webhook as an embed.
func (n *Notifier) sendDiscord(ch Channel, event NotifyEvent) error {
	var cfg DiscordConfig
	if err := json.Unmarshal([]byte(ch.Config), &cfg); err != nil {
		return fmt.Errorf("parse discord config: %w", err)
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("discord webhook URL is empty")
	}

	// Determine embed color based on event type.
	color := 3447003 // blue (default)
	if strings.Contains(event.Type, "success") {
		color = 3066993 // green
	} else if strings.Contains(event.Type, "failed") || strings.Contains(event.Type, "error") {
		color = 15158332 // red
	} else if strings.Contains(event.Type, "warning") {
		color = 15105570 // orange
	}

	// Build description, truncate to Discord limit.
	description := event.Message
	if len(description) > 2000 {
		description = description[:2000] + "..."
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       event.Title,
				"description": description,
				"color":       color,
				"footer": map[string]interface{}{
					"text": "Web.Casa • " + event.Type,
				},
				"timestamp": event.Time.Format(time.RFC3339),
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// sendTelegram sends the event via Telegram Bot API.
func (n *Notifier) sendTelegram(ch Channel, event NotifyEvent) error {
	var cfg TelegramConfig
	if err := json.Unmarshal([]byte(ch.Config), &cfg); err != nil {
		return fmt.Errorf("parse telegram config: %w", err)
	}
	if cfg.BotToken == "" || cfg.ChatID == "" {
		return fmt.Errorf("telegram config incomplete: bot_token and chat_id required")
	}

	// Build Markdown message.
	message := fmt.Sprintf("*%s*\n\n%s\n\n`%s` • %s",
		escapeMarkdown(event.Title),
		escapeMarkdown(event.Message),
		event.Type,
		event.Time.Format("2006-01-02 15:04:05"),
	)

	// Truncate to Telegram limit (4096 chars).
	if len(message) > 4000 {
		message = message[:4000] + "..."
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	payload := map[string]interface{}{
		"chat_id":    cfg.ChatID,
		"text":       message,
		"parse_mode": "Markdown",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}
	return nil
}

// escapeMarkdown escapes special Markdown characters for Telegram.
func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"`", "\\`",
	)
	return replacer.Replace(s)
}

// TestChannel sends a test notification to verify channel configuration.
func (n *Notifier) TestChannel(ch Channel) error {
	event := NotifyEvent{
		Type:    "test",
		Title:   "Test Notification",
		Message: "This is a test notification from Web.Casa. If you received this, your notification channel is configured correctly.",
		Time:    time.Now(),
	}

	switch ch.Type {
	case "webhook":
		return n.sendWebhook(ch, event)
	case "email":
		return n.sendEmail(ch, event)
	case "discord":
		return n.sendDiscord(ch, event)
	case "telegram":
		return n.sendTelegram(ch, event)
	default:
		return fmt.Errorf("unknown channel type: %s", ch.Type)
	}
}

// --- CRUD helpers ---

// ListChannels returns all notification channels.
func (n *Notifier) ListChannels() ([]Channel, error) {
	var channels []Channel
	err := n.db.Order("created_at desc").Find(&channels).Error
	return channels, err
}

// GetChannel returns a channel by ID.
func (n *Notifier) GetChannel(id uint) (*Channel, error) {
	var ch Channel
	err := n.db.First(&ch, id).Error
	return &ch, err
}

// CreateChannel creates a new notification channel.
func (n *Notifier) CreateChannel(ch *Channel) error {
	return n.db.Create(ch).Error
}

// UpdateChannel updates an existing notification channel.
func (n *Notifier) UpdateChannel(id uint, updates map[string]interface{}) error {
	return n.db.Model(&Channel{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteChannel deletes a notification channel.
func (n *Notifier) DeleteChannel(id uint) error {
	return n.db.Delete(&Channel{}, id).Error
}
