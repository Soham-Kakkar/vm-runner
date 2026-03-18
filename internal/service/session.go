package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"vm-runner/internal/storage"
	"vm-runner/internal/vm"
)

var (
	ErrSessionExists   = errors.New("a session is already active")
	ErrSessionNotFound = errors.New("session not found")
)

// Session represents an active challenge session.
type Session struct {
	ID                 string
	Challenge          storage.Challenge
	VMManager          *vm.QEMUManager
	CompletedQuestions map[string]bool
	StartTime          time.Time
	WebSocket          chan<- WebSocketMessage
}

// SessionManager handles the lifecycle of active sessions.
// For the MVP, it manages only one session at a time.
type SessionManager struct {
	mu            sync.RWMutex
	activeSession *Session
	challengeStore *storage.FileStore
	flagFoundChan chan<- FlagFoundEvent
}

// NewSessionManager creates a new session manager.
func NewSessionManager(challengeStore *storage.FileStore, flagFoundChan chan FlagFoundEvent) *SessionManager {
	return &SessionManager{
		challengeStore: challengeStore,
		flagFoundChan: flagFoundChan,
	}
}

// StartSession creates and starts a new challenge session.
func (sm *SessionManager) StartSession(challengeID string) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.activeSession != nil {
		return nil, ErrSessionExists
	}

	challenge, err := sm.challengeStore.GetChallenge(challengeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get challenge: %w", err)
	}

	vmManager := vm.NewQEMUManager(challenge.VMConfig)
	if err := vmManager.Start(); err != nil {
		return nil, fmt.Errorf("failed to start vm: %w", err)
	}

	sessionID := uuid.New().String()
	session := &Session{
		ID:                 sessionID,
		Challenge:          challenge,
		VMManager:          vmManager,
		CompletedQuestions: make(map[string]bool),
		StartTime:          time.Now(),
	}

	sm.activeSession = session
	go sm.monitorVM(session)

	return session, nil
}

// EndSession stops and cleans up the active session.
func (sm *SessionManager) EndSession(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.activeSession == nil || sm.activeSession.ID != sessionID {
		return ErrSessionNotFound
	}

	if err := sm.activeSession.VMManager.Stop(); err != nil {
		fmt.Printf("Warning: failed to stop VM for session %s: %v\n", sessionID, err)
	}

	if sm.activeSession.WebSocket != nil {
		close(sm.activeSession.WebSocket)
	}
	
	sm.activeSession = nil
	return nil
}

// GetSession retrieves the currently active session.
func (sm *SessionManager) GetSession(sessionID string) (*Session, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if sm.activeSession != nil && sm.activeSession.ID == sessionID {
		return sm.activeSession, nil
	}
	return nil, ErrSessionNotFound
}

// GetActiveSession returns the currently active session if any.
func (sm *SessionManager) GetActiveSession() *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.activeSession
}

// RegisterWebSocket associates a WebSocket connection with the session.
func (sm *SessionManager) RegisterWebSocket(sessionID string, ws chan<- WebSocketMessage) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.activeSession == nil || sm.activeSession.ID != sessionID {
		return ErrSessionNotFound
	}
	sm.activeSession.WebSocket = ws
	return nil
}


// HandleVMInput sends user input to the VM.
func (sm *SessionManager) HandleVMInput(sessionID, input string) error {
	session, err := sm.GetSession(sessionID)
	if err != nil {
		return err
	}
	_, err = session.VMManager.SendInput(input)
	return err
}

// SubmitAnswer checks a user-submitted answer.
func (sm *SessionManager) SubmitAnswer(sessionID, answer string) (bool, string) {
	session, err := sm.GetSession(sessionID)
	if err != nil {
		return false, ""
	}

	for _, q := range session.Challenge.Questions {
		if !session.CompletedQuestions[q.ID] && strings.TrimSpace(answer) == q.Answer {
			session.CompletedQuestions[q.ID] = true
			return true, q.ID
		}
	}
	return false, ""
}


// monitorVM reads output from the VM and forwards it.
func (sm *SessionManager) monitorVM(session *Session) {
	for {
		select {
		case output, ok := <-session.VMManager.OutputChan:
			if !ok {
				// VM stopped
				if session.WebSocket != nil {
					session.WebSocket <- WebSocketMessage{
						Type:    "error",
						Payload: "VM stopped unexpectedly",
					}
				}
				sm.EndSession(session.ID)
				return
			}

			//IMPORTANT: send raw output (no forced newline)
			if session.WebSocket != nil && output != "" {
				session.WebSocket <- WebSocketMessage{
					Type:    "vm_output",
					Payload: output,
				}
			}

			// Check for flags using raw output
			sm.checkForFlag(session, output)

		case err, ok := <-session.VMManager.ErrorChan:
			if ok && err != nil {
				if session.WebSocket != nil {
					session.WebSocket <- WebSocketMessage{
						Type:    "error",
						Payload: err.Error(),
					}
				}
			}

			sm.EndSession(session.ID)
			return
		}
	}
}

// checkForFlag scans VM output for answers.
func (sm *SessionManager) checkForFlag(session *Session, output string) {
	for _, q := range session.Challenge.Questions {
		if !session.CompletedQuestions[q.ID] && strings.Contains(output, q.Answer) {
			
			session.CompletedQuestions[q.ID] = true

			// Notify the hub/handler via channel
			if sm.flagFoundChan != nil {
				sm.flagFoundChan <- FlagFoundEvent{
					SessionID:  session.ID,
					QuestionID: q.ID,
				}
			}
		}
	}
}
