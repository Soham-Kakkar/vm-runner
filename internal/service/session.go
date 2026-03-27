package service

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"vm-runner/internal/storage"
	"vm-runner/internal/vm"

	"github.com/google/uuid"
)

var (
	ErrSessionExists   = errors.New("a session is already active")
	ErrSessionNotFound = errors.New("session not found")
)

// Define the flag regex globally for efficiency.
// It matches "flag{" followed by any character that is not a bracket, ending with "}".
var flagRegex = regexp.MustCompile(`flag\{[^{}]*\}`)

// Session represents an active challenge session.
type Session struct {
	ID                 string
	Challenge          storage.Challenge
	VMManager          *vm.QEMUManager
	CompletedQuestions map[string]bool
	StartTime          time.Time
	WebSocket          chan<- WebSocketMessage
	outputBuffer       strings.Builder
	history            []byte
	historyMu          sync.Mutex
}

// SessionManager handles the lifecycle of active sessions.
// For the MVP, it manages only one session at a time.
type SessionManager struct {
	mu             sync.RWMutex
	activeSession  *Session
	challengeStore *storage.FileStore
	flagFoundChan  chan<- FlagFoundEvent
}

// NewSessionManager creates a new session manager.
func NewSessionManager(challengeStore *storage.FileStore, flagFoundChan chan FlagFoundEvent) *SessionManager {
	return &SessionManager{
		challengeStore: challengeStore,
		flagFoundChan:  flagFoundChan,
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

	// Clear WebSocket reference; the hub is responsible for closing client send channels.
	sm.activeSession.WebSocket = nil

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

	// Send current history if any
	sm.activeSession.historyMu.Lock()
	if len(sm.activeSession.history) > 0 {
		ws <- WebSocketMessage{
			Type:    "vm_output",
			Payload: string(sm.activeSession.history),
		}
	}
	sm.activeSession.historyMu.Unlock()

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
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	var batch strings.Builder

	for {
		select {
		case <-ticker.C:
			if batch.Len() > 0 && session.WebSocket != nil {
				session.WebSocket <- WebSocketMessage{
					Type:    "vm_output",
					Payload: batch.String(),
				}
				batch.Reset()
			}

		case outputBytes, ok := <-session.VMManager.OutputChan:
			if !ok {
				// Final flush if anything is left in the batch.
				if batch.Len() > 0 && session.WebSocket != nil {
					session.WebSocket <- WebSocketMessage{
						Type:    "vm_output",
						Payload: batch.String(),
					}
				}

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

			// Convert bytes to string and append to batch
			output := string(outputBytes)
			batch.WriteString(output)

			// Store in session history for reloads/new clients
			session.historyMu.Lock()
			session.history = append(session.history, outputBytes...)
			// Limit history to last 100KB
			const maxHistory = 100000
			if len(session.history) > maxHistory {
				session.history = session.history[len(session.history)-maxHistory:]
			}
			session.historyMu.Unlock()

			// Optional debug: print raw bytes as hex when VM_RUNNER_DEBUG=1
			if os.Getenv("VM_RUNNER_DEBUG") == "1" {
				fmt.Printf("[DEBUG] vm output bytes: %x\n", outputBytes)
			}

			// Check for flags using raw output (immediately, don't wait for ticker)
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

// checkForFlag scans VM output for anything matching flag{...}.
// If a match is found, it's checked against all incomplete questions.
func (sm *SessionManager) checkForFlag(session *Session, output string) {
	// Append the new output to the session-specific buffer.
	session.outputBuffer.WriteString(output)

	// Keep the buffer at a reasonable size for scanning (e.g., last 1000 characters)
	const maxScanBuffer = 1000
	currentBuf := session.outputBuffer.String()
	if len(currentBuf) > maxScanBuffer {
		newBuf := currentBuf[len(currentBuf)-maxScanBuffer:]
		session.outputBuffer.Reset()
		session.outputBuffer.WriteString(newBuf)
		currentBuf = newBuf
	}

	matches := flagRegex.FindAllString(currentBuf, -1)
	if len(matches) == 0 {
		return
	}

	for _, match := range matches {
		for _, q := range session.Challenge.Questions {
			if !session.CompletedQuestions[q.ID] && match == q.Answer {
				session.CompletedQuestions[q.ID] = true

				// Notify the handler via channel
				if sm.flagFoundChan != nil {
					sm.flagFoundChan <- FlagFoundEvent{
						SessionID:  session.ID,
						QuestionID: q.ID,
					}
				}
			}
		}
	}
}
