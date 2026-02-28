package filemanager

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
)

const maxSessions = 20 // Maximum concurrent terminal sessions

// TerminalManager manages PTY terminal sessions.
type TerminalManager struct {
	sessions map[string]*TerminalSession
	mu       sync.RWMutex
	logger   *slog.Logger
}

// TerminalSession represents an active PTY session.
type TerminalSession struct {
	ID      string
	PTY     *os.File
	Cmd     *exec.Cmd
	Created time.Time
}

// TerminalInput is the message format from client to server.
type TerminalInput struct {
	Type string `json:"type"` // "data" or "resize"
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// NewTerminalManager creates a new terminal manager.
func NewTerminalManager(logger *slog.Logger) *TerminalManager {
	return &TerminalManager{
		sessions: make(map[string]*TerminalSession),
		logger:   logger,
	}
}

// Create starts a new PTY session.
func (tm *TerminalManager) Create(cols, rows uint16) (*TerminalSession, error) {
	tm.mu.Lock()
	if len(tm.sessions) >= maxSessions {
		tm.mu.Unlock()
		return nil, fmt.Errorf("too many terminal sessions (max %d)", maxSessions)
	}
	tm.mu.Unlock()

	// Find available shell.
	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}

	session := &TerminalSession{
		ID:      uuid.New().String(),
		PTY:     ptmx,
		Cmd:     cmd,
		Created: time.Now(),
	}

	tm.mu.Lock()
	tm.sessions[session.ID] = session
	tm.mu.Unlock()

	tm.logger.Info("terminal session created", "id", session.ID)
	return session, nil
}

// Resize changes the terminal size.
func (tm *TerminalManager) Resize(sessionID string, cols, rows uint16) error {
	tm.mu.RLock()
	session, ok := tm.sessions[sessionID]
	tm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session not found")
	}
	return pty.Setsize(session.PTY, &pty.Winsize{Cols: cols, Rows: rows})
}

// Close terminates and cleans up a session.
func (tm *TerminalManager) Close(sessionID string) {
	tm.mu.Lock()
	session, ok := tm.sessions[sessionID]
	if ok {
		delete(tm.sessions, sessionID)
	}
	tm.mu.Unlock()

	if !ok {
		return
	}

	session.PTY.Close()
	if session.Cmd.Process != nil {
		session.Cmd.Process.Kill()
		session.Cmd.Wait()
	}
	tm.logger.Info("terminal session closed", "id", sessionID)
}

// CleanupStale removes sessions older than maxAge.
func (tm *TerminalManager) CleanupStale(maxAge time.Duration) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	now := time.Now()
	for id, s := range tm.sessions {
		if now.Sub(s.Created) > maxAge {
			s.PTY.Close()
			if s.Cmd.Process != nil {
				s.Cmd.Process.Kill()
				s.Cmd.Wait()
			}
			delete(tm.sessions, id)
			tm.logger.Info("stale terminal session cleaned up", "id", id)
		}
	}
}

// CloseAll terminates all sessions (called on plugin Stop).
func (tm *TerminalManager) CloseAll() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for id, s := range tm.sessions {
		s.PTY.Close()
		if s.Cmd.Process != nil {
			s.Cmd.Process.Kill()
			s.Cmd.Wait()
		}
		delete(tm.sessions, id)
	}
}
