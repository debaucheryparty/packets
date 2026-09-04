package android

import (
	"time"

	"github.com/google/uuid"
)

type SessionState string

const (
	SessionStateCreating     SessionState = "creating"
	SessionStateActive       SessionState = "active"
	SessionStateDisconnected SessionState = "disconnected"
	SessionStateStopped      SessionState = "stopped"
)

type Session struct {
	ID           string
	NodeID       string
	AVD          string
	Serial       string
	EmulatorPID  int
	State        SessionState
	CreatedAt    time.Time
	LastActivity time.Time
}

func NewSession(nodeID, avd string) *Session {
	return &Session{
		ID:           uuid.New().String(),
		NodeID:       nodeID,
		AVD:          avd,
		State:        SessionStateCreating,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}
}

func (s *Session) Touch() {
	s.LastActivity = time.Now()
}

func (s *Session) IsIdle(idleTimeout time.Duration) bool {
	return time.Since(s.LastActivity) >= idleTimeout
}

type SessionManager struct {
	sessions map[string]*Session
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

func (sm *SessionManager) CreateSession(nodeID, avd string) *Session {
	s := NewSession(nodeID, avd)
	sm.sessions[s.ID] = s
	return s
}

func (sm *SessionManager) GetSession(id string) (*Session, bool) {
	s, ok := sm.sessions[id]
	return s, ok
}

func (sm *SessionManager) ListSessions() []*Session {
	list := make([]*Session, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		list = append(list, s)
	}
	return list
}

func (sm *SessionManager) TouchSession(id string) {
	if s, ok := sm.sessions[id]; ok {
		s.Touch()
	}
}

func (sm *SessionManager) CloseSession(id string) {
	if s, ok := sm.sessions[id]; ok {
		s.State = SessionStateStopped
	}
}

func (sm *SessionManager) CleanupIdleSessions(idleTimeout time.Duration) []string {
	var cleaned []string
	for id, s := range sm.sessions {
		if s.State != SessionStateStopped && s.IsIdle(idleTimeout) {
			s.State = SessionStateStopped
			cleaned = append(cleaned, id)
		}
	}
	return cleaned
}
