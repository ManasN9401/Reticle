package memory

import "time"

// SessionState holds global, mostly immutable configuration for the current run.
type SessionState struct {
	SessionID string
	StartTime time.Time
}

func NewSessionState(sessionID string) *SessionState {
	return &SessionState{
		SessionID: sessionID,
		StartTime: time.Now(),
	}
}
