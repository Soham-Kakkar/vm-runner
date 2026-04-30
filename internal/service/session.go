package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"vm-runner/internal/storage"
	"vm-runner/internal/vm"
)

var (
	ErrSessionExists     = errors.New("a session is already active")
	ErrSessionNotFound   = errors.New("session not found")
	ErrSessionClosed     = errors.New("session is not active")
	ErrChallengeMismatch = errors.New("challenge belongs to a different ctf")
)

type sessionRecord struct {
	session      *storage.Session
	vmManager    *vm.QEMUManager
	webSocket    chan<- WebSocketMessage
	history      []byte
	historyMu    sync.Mutex
	outputBuffer strings.Builder
	commandMu    sync.Mutex
	commandBuf   []rune
	lastCommand  string
	tabPending   bool
	solvedMu     sync.Mutex
	solvedMap    map[string]bool
}

type SessionManager struct {
	mu             sync.RWMutex
	sessions       map[string]*sessionRecord
	challengeStore *storage.FileStore
	flagFoundChan  chan<- FlagFoundEvent
	secret         []byte
	runtimeBase    string
}

func NewSessionManager(challengeStore *storage.FileStore, flagFoundChan chan FlagFoundEvent) *SessionManager {
	secret := []byte(os.Getenv("VMRUNNER_SECRET"))
	if len(secret) == 0 {
		secret = []byte("dev-seed")
	}
	runtimeBase := filepath.Join("data", "runtime")
	_ = os.MkdirAll(runtimeBase, 0o755)
	sm := &SessionManager{
		sessions:       make(map[string]*sessionRecord),
		challengeStore: challengeStore,
		flagFoundChan:  flagFoundChan,
		secret:         secret,
		runtimeBase:    runtimeBase,
	}

	// Load any existing session metadata from the runtime directory so UI can
	// show persisted sessions after server restarts. These sessions do not have
	// an active vmManager attached (VM processes are not resumed automatically).
	_ = sm.loadPersistedSessions()

	// Start inactivity watcher to close idle sessions.
	go sm.inactivityWatcher()

	return sm
}

func (sm *SessionManager) persistedSessionPath(sessionID string) string {
	return filepath.Join(sm.runtimeBase, sessionID, "session.json")
}

func (sm *SessionManager) saveSessionToDisk(s *storage.Session) error {
	path := sm.persistedSessionPath(s.ID)
	data, err := storage.MarshalIndentNoEscape(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (sm *SessionManager) deleteSessionFromDisk(sessionID string) error {
	path := sm.persistedSessionPath(sessionID)
	if _, err := os.Stat(path); err == nil {
		return os.Remove(path)
	}
	return nil
}

func (sm *SessionManager) loadPersistedSessions() error {
	entries, err := os.ReadDir(sm.runtimeBase)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		path := filepath.Join(sm.runtimeBase, id, "session.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var s storage.Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		// Rehydrate into in-memory map without a vmManager.
		sm.sessions[s.ID] = &sessionRecord{session: &s}
		// Do not start monitorVM because there's no vmManager to read output from.
	}
	return nil
}

// inactivityWatcher closes sessions that have been idle past the inactivity threshold.
func (sm *SessionManager) inactivityWatcher() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		sm.mu.RLock()
		ids := make([]string, 0, len(sm.sessions))
		for id := range sm.sessions {
			ids = append(ids, id)
		}
		sm.mu.RUnlock()
		now := time.Now().UTC()
		for _, id := range ids {
			record, err := sm.getRecord(id)
			if err != nil {
				continue
			}
			if record.session.Status != storage.SessionStatusActive {
				continue
			}
			last := record.session.LastActivity
			if last == nil {
				// fall back to CreatedAt
				t := record.session.CreatedAt
				last = &t
			}
			if now.Sub(*last) > 10*time.Minute {
				log.Printf("ending session %s due to inactivity", id)
				_ = sm.EndSession(id)
			}
		}
	}
}

func (sm *SessionManager) StartSession(challengeID string) (*storage.Session, error) {
	return sm.StartSessionWithID(challengeID, "", "")
}

// StartSessionWithID starts a session using the provided sessionID.
// If sessionID is empty a new random UUID will be generated.
func (sm *SessionManager) StartSessionWithID(challengeID, sessionID, userID string) (*storage.Session, error) {
	challenge, err := sm.challengeStore.GetChallenge(challengeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get challenge: %w", err)
	}
	if sessionID == "" {
		sessionID = uuid.NewString()
	} else {
		// If a session with this ID exists and is active, return exists error
		sm.mu.RLock()
		if rec, ok := sm.sessions[sessionID]; ok && rec.session != nil && rec.session.Status == storage.SessionStatusActive {
			sm.mu.RUnlock()
			return nil, ErrSessionExists
		}
		sm.mu.RUnlock()
	}
	seed := sm.deriveSeed(sessionID)
	runtimePath := filepath.Join(sm.runtimeBase, sessionID)
	if err := os.MkdirAll(runtimePath, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create runtime directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runtimePath, "seed"), []byte(seed), 0o400); err != nil {
		return nil, fmt.Errorf("failed to write session seed: %w", err)
	}

	// Prefer the CTF-level VMConfig when the challenge does not specify its own image.
	cfg := challenge.VMConfig
	if cfg.ImagePath == "" {
		if ctf, err := sm.challengeStore.GetCTF(challenge.CTFID); err == nil {
			if ctf.VMConfig.ImagePath != "" {
				cfg = ctf.VMConfig
			}
		}
	}
	vmManager := vm.NewQEMUManager(cfg, runtimePath)
	if err := vmManager.Start(); err != nil {
		return nil, fmt.Errorf("failed to start vm: %w", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(cfg.TimeoutSeconds) * time.Second)
	if cfg.TimeoutSeconds <= 0 {
		expiresAt = now.Add(30 * time.Minute)
	}

	session := &storage.Session{
		ID:           sessionID,
		ChallengeID:  challenge.ID,
		UserID:       userID,
		RuntimePath:  runtimePath,
		Seed:         seed,
		CreatedAt:    now,
		LastActivity: &now,
		ExpiresAt:    expiresAt,
		Status:       storage.SessionStatusActive,
		Challenge:    challenge,
	}

	record := &sessionRecord{
		session:   session,
		vmManager: vmManager,
		solvedMap: make(map[string]bool),
	}

	sm.mu.Lock()
	sm.sessions[sessionID] = record
	sm.mu.Unlock()

	// Persist session metadata so it survives server restarts.
	if err := sm.saveSessionToDisk(session); err != nil {
		log.Printf("warning: failed to persist session %s: %v", session.ID, err)
	}

	go sm.monitorVM(sessionID)
	go sm.expireSession(sessionID, expiresAt)
	return sm.copySession(sessionID)
}

func (sm *SessionManager) EndSession(sessionID string) error {
	record, err := sm.getRecord(sessionID)
	if err != nil {
		return err
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()
	if record.session.Status != storage.SessionStatusActive {
		return ErrSessionClosed
	}
	if record.vmManager != nil {
		if err := record.vmManager.Stop(); err != nil {
			log.Printf("warning: failed to stop vm for session %s: %v", sessionID, err)
		}
	} else {
		log.Printf("no vmManager for session %s; marking stopped", sessionID)
	}
	now := time.Now().UTC()
	record.session.Status = storage.SessionStatusStopped
	record.session.StoppedAt = &now
	record.webSocket = nil

	// Remove persisted session metadata
	if err := sm.deleteSessionFromDisk(sessionID); err != nil {
		log.Printf("warning: failed to delete session file for %s: %v", sessionID, err)
	}
	return nil
}

func (sm *SessionManager) GetSession(sessionID string) (*storage.Session, error) {
	return sm.copySession(sessionID)
}

func (sm *SessionManager) SwitchSessionChallenge(sessionID, challengeID string) (*storage.Session, error) {
	challenge, err := sm.challengeStore.GetChallenge(challengeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get challenge: %w", err)
	}
	record, err := sm.getRecord(sessionID)
	if err != nil {
		return nil, err
	}
	if record.session.Status != storage.SessionStatusActive {
		return nil, ErrSessionClosed
	}

	currentCTFID := record.session.Challenge.CTFID
	if currentCTFID == "" {
		current, err := sm.challengeStore.GetChallenge(record.session.ChallengeID)
		if err != nil {
			return nil, fmt.Errorf("failed to get current challenge: %w", err)
		}
		currentCTFID = current.CTFID
	}
	if challenge.CTFID != currentCTFID {
		return nil, ErrChallengeMismatch
	}

	now := time.Now().UTC()
	sm.mu.Lock()
	record.session.ChallengeID = challenge.ID
	record.session.Challenge = challenge
	record.session.LastActivity = &now
	if record.session.ExpiresAt.Before(now) {
		record.session.ExpiresAt = now.Add(30 * time.Minute)
	}
	record.outputBuffer.Reset()
	sm.mu.Unlock()

	if err := sm.saveSessionToDisk(record.session); err != nil {
		log.Printf("warning: failed to persist challenge switch for session %s: %v", sessionID, err)
	}
	return sm.copySession(sessionID)
}

func (sm *SessionManager) GetActiveSession() *storage.Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	var latest *storage.Session
	for _, record := range sm.sessions {
		if record.session.Status == storage.SessionStatusActive {
			if latest == nil || record.session.CreatedAt.After(latest.CreatedAt) {
				copy := *record.session
				latest = &copy
			}
		}
	}
	return latest
}

func (sm *SessionManager) RegisterWebSocket(sessionID string, ws chan<- WebSocketMessage) error {
	record, err := sm.getRecord(sessionID)
	if err != nil {
		return err
	}

	sm.mu.Lock()
	record.webSocket = ws
	log.Printf("websocket registered for session %s", sessionID)
	now := time.Now().UTC()
	record.session.LastActivity = &now
	if err := sm.saveSessionToDisk(record.session); err != nil {
		log.Printf("warning: failed to update session last_activity for %s: %v", sessionID, err)
	}
	history := append([]byte(nil), record.history...)
	sm.mu.Unlock()

	if len(history) > 0 {
		ws <- WebSocketMessage{Type: "vm_output", Payload: string(history)}
	}
	return nil
}

func (sm *SessionManager) HandleVMInput(sessionID, input string) error {
	record, err := sm.getRecord(sessionID)
	if err != nil {
		return err
	}
	if record.session.Status != storage.SessionStatusActive {
		return ErrSessionClosed
	}
	sm.trackCommand(record, input)
	_, err = record.vmManager.SendInput(input)
	if err != nil {
		log.Printf("failed to send input to vm for session %s: %v", sessionID, err)
		return err
	}
	now := time.Now().UTC()
	record.session.LastActivity = &now
	if err := sm.saveSessionToDisk(record.session); err != nil {
		log.Printf("warning: failed to update session last_activity for %s: %v", sessionID, err)
	}
	// log.Printf("wrote %d bytes to vm for session %s", n, sessionID)
	return nil
}

func (sm *SessionManager) SubmitAnswer(sessionID, answer, lastCommand string) (bool, *storage.Submission, error) {
	record, err := sm.getRecord(sessionID)
	if err != nil {
		return false, nil, err
	}
	if record.session.Status != storage.SessionStatusActive {
		return false, nil, ErrSessionClosed
	}

	correct, err := sm.validateAnswer(record.session, strings.TrimSpace(answer))
	if err != nil {
		return false, nil, err
	}

	submission := &storage.Submission{
		ID:          uuid.NewString(),
		SessionID:   record.session.ID,
		ChallengeID: record.session.ChallengeID,
		Answer:      strings.TrimSpace(answer),
		IsCorrect:   correct,
		CreatedAt:   time.Now().UTC(),
	}

	sm.mu.Lock()
	record.session.Submissions = append(record.session.Submissions, *submission)
	sm.mu.Unlock()

	if correct {
		if lastCommand = stripTerminalControlCodes(strings.TrimSpace(lastCommand)); lastCommand != "" {
			record.commandMu.Lock()
			record.lastCommand = lastCommand
			record.commandMu.Unlock()
		}
		sm.onSolved(record, submission.CreatedAt, record.session.Challenge.ID)
	}

	return correct, submission, nil
}

func (sm *SessionManager) deriveSeed(sessionID string) string {
	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (sm *SessionManager) validateAnswer(session *storage.Session, answer string) (bool, error) {
	switch strings.ToLower(session.Challenge.Validator) {
	case "", storage.ChallengeValidatorStatic:
		return answer == session.Challenge.Flag, nil
	case storage.ChallengeValidatorHMAC:
		return answer == sm.expectedHMACFlag(session), nil
	default:
		return false, fmt.Errorf("unsupported validator %q", session.Challenge.Validator)
	}
}

func (sm *SessionManager) expectedHMACFlag(session *storage.Session) string {
	seedBytes, err := hex.DecodeString(session.Seed)
	if err != nil {
		return ""
	}
	questionNo := session.Challenge.QuestionNo
	if questionNo <= 0 {
		questionNo = 1
	}
	mac := hmac.New(sha256.New, seedBytes)
	mac.Write([]byte(fmt.Sprintf("%d", questionNo)))
	h := hex.EncodeToString(mac.Sum(nil))
	template := session.Challenge.Template
	if template == "" {
		template = "flag{<hmac>}"
	}
	return strings.ReplaceAll(template, "<hmac>", h[:5])
}

func (sm *SessionManager) monitorVM(sessionID string) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	var batch strings.Builder
	for {
		record, err := sm.getRecord(sessionID)
		if err != nil {
			return
		}

		select {
		case <-ticker.C:
			if batch.Len() > 0 {
				sm.pushOutput(record, batch.String())
				batch.Reset()
			}
		case outputBytes, ok := <-record.vmManager.OutputChan:
			if !ok {
				if batch.Len() > 0 {
					sm.pushOutput(record, batch.String())
				}
				sm.signalSessionError(record, "VM stopped unexpectedly")
				_ = sm.EndSession(sessionID)
				return
			}
			output := string(outputBytes)
			batch.WriteString(output)
			sm.applyTabCompletion(record, output)
			sm.appendHistory(record, outputBytes)
			sm.checkForFlag(record, output)
		case err, ok := <-record.vmManager.ErrorChan:
			if ok && err != nil {
				sm.signalSessionError(record, err.Error())
			}
			_ = sm.EndSession(sessionID)
			return
		}
	}
}

func (sm *SessionManager) expireSession(sessionID string, expiresAt time.Time) {
	time.Sleep(time.Until(expiresAt))
	record, err := sm.getRecord(sessionID)
	if err != nil {
		return
	}
	sm.mu.Lock()
	if record.session.Status == storage.SessionStatusActive {
		now := time.Now().UTC()
		record.session.Status = storage.SessionStatusExpired
		record.session.StoppedAt = &now
		_ = record.vmManager.Stop()
	}
	sm.mu.Unlock()
}

func (sm *SessionManager) appendHistory(record *sessionRecord, outputBytes []byte) {
	record.historyMu.Lock()
	defer record.historyMu.Unlock()
	record.history = append(record.history, outputBytes...)
	const maxHistory = 100000
	if len(record.history) > maxHistory {
		record.history = record.history[len(record.history)-maxHistory:]
	}
}

func (sm *SessionManager) pushOutput(record *sessionRecord, output string) {
	record.historyMu.Lock()
	ws := record.webSocket
	record.historyMu.Unlock()
	if ws == nil || output == "" {
		if ws == nil {
			log.Printf("no websocket for session %s; dropping %d bytes of output", record.session.ID, len(output))
		}
		return
	}
	// Send output to websocket channel and log size.
	ws <- WebSocketMessage{Type: "vm_output", Payload: output}
	// log.Printf("pushed %d bytes of vm_output to session %s websocket", len(output), record.session.ID)

	// Update last activity when we push output to a connected client.
	now := time.Now().UTC()
	record.session.LastActivity = &now
	if err := sm.saveSessionToDisk(record.session); err != nil {
		log.Printf("warning: failed to update session last_activity for %s: %v", record.session.ID, err)
	}
}

func (sm *SessionManager) signalSessionError(record *sessionRecord, message string) {
	record.historyMu.Lock()
	ws := record.webSocket
	record.historyMu.Unlock()
	if ws != nil {
		ws <- WebSocketMessage{Type: "error", Payload: message}
	}
}

func (sm *SessionManager) checkForFlag(record *sessionRecord, output string) {
	record.outputBuffer.WriteString(output)
	const maxScanBuffer = 2000 // slightly larger to accommodate longer flags
	currentBuf := record.outputBuffer.String()
	if len(currentBuf) > maxScanBuffer {
		trimmed := currentBuf[len(currentBuf)-maxScanBuffer:]
		record.outputBuffer.Reset()
		record.outputBuffer.WriteString(trimmed)
		currentBuf = trimmed
	}

	var expected string
	if record.session.Challenge.Validator == "" || record.session.Challenge.Validator == storage.ChallengeValidatorStatic {
		expected = record.session.Challenge.Flag
	} else if record.session.Challenge.Validator == storage.ChallengeValidatorHMAC {
		expected = sm.expectedHMACFlag(record.session)
	}

	if expected != "" && strings.Contains(currentBuf, expected) {
		if sm.onSolved(record, time.Now().UTC(), record.session.Challenge.ID) {
			log.Printf("detected expected flag in session %s output", record.session.ID)
		}
	}
}

func (sm *SessionManager) trackCommand(record *sessionRecord, input string) {
	if input == "" {
		return
	}
	input = sanitizeCommandInput(input)
	if input == "" {
		return
	}
	record.commandMu.Lock()
	defer record.commandMu.Unlock()
	for _, r := range input {
		switch r {
		case '\t':
			record.tabPending = true
		case '\r', '\n':
			record.tabPending = false
			cmd := strings.TrimSpace(string(record.commandBuf))
			if cmd != "" {
				record.lastCommand = cmd
			}
			record.commandBuf = record.commandBuf[:0]
		case 0x08, 0x7f:
			record.tabPending = false
			if len(record.commandBuf) > 0 {
				record.commandBuf = record.commandBuf[:len(record.commandBuf)-1]
			}
		default:
			record.tabPending = false
			if r >= 0x20 {
				record.commandBuf = append(record.commandBuf, r)
			}
		}
	}
}

func (sm *SessionManager) applyTabCompletion(record *sessionRecord, output string) {
	if output == "" {
		return
	}
	record.commandMu.Lock()
	defer record.commandMu.Unlock()
	if !record.tabPending {
		return
	}
	if strings.ContainsAny(output, "\r\n") {
		record.tabPending = false
		return
	}
	cleaned := stripTerminalControlCodes(output)
	if cleaned == "" {
		record.tabPending = false
		return
	}
	buf := string(record.commandBuf)
	if buf != "" {
		if idx := strings.Index(cleaned, buf); idx >= 0 {
			cleaned = cleaned[idx:]
		}
	}
	if strings.HasPrefix(cleaned, buf) {
		record.commandBuf = []rune(cleaned)
	} else {
		record.commandBuf = append(record.commandBuf, []rune(cleaned)...)
	}
	if cmd := strings.TrimSpace(string(record.commandBuf)); cmd != "" {
		record.lastCommand = cmd
	}
	record.tabPending = false
}

func sanitizeCommandInput(input string) string {
	if input == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(input))
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if ch == 0x1b {
			i++
			if i >= len(input) {
				break
			}
			next := input[i]
			switch next {
			case '[':
				for i+1 < len(input) {
					i++
					if input[i] >= 0x40 && input[i] <= 0x7E {
						break
					}
				}
			case ']':
				for i+1 < len(input) {
					i++
					if input[i] == 0x07 {
						break
					}
					if input[i] == 0x1b && i+1 < len(input) && input[i+1] == '\\' {
						i++
						break
					}
				}
			default:
				// swallow the escape and the following byte
			}
			continue
		}
		switch ch {
		case '\r', '\n', '\t', 0x08, 0x7f:
			b.WriteByte(ch)
		default:
			if ch < 0x20 {
				continue
			}
			b.WriteByte(ch)
		}
	}
	return b.String()
}

func stripTerminalControlCodes(input string) string {
	if input == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(input))
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if ch == 0x1b {
			i++
			if i >= len(input) {
				break
			}
			next := input[i]
			switch next {
			case '[':
				for i+1 < len(input) {
					i++
					if input[i] >= 0x40 && input[i] <= 0x7E {
						break
					}
				}
			case ']':
				for i+1 < len(input) {
					i++
					if input[i] == 0x07 {
						break
					}
					if input[i] == 0x1b && i+1 < len(input) && input[i+1] == '\\' {
						i++
						break
					}
				}
			default:
				// swallow the escape and the following byte
			}
			continue
		}
		if ch < 0x20 || ch == 0x7f {
			continue
		}
		b.WriteByte(ch)
	}
	return strings.TrimSpace(b.String())
}

func (sm *SessionManager) onSolved(record *sessionRecord, when time.Time, challengeID string) bool {
	if challengeID == "" {
		challengeID = record.session.Challenge.ID
	}
	record.solvedMu.Lock()
	if record.solvedMap == nil {
		record.solvedMap = make(map[string]bool)
	}
	if record.solvedMap[challengeID] {
		record.solvedMu.Unlock()
		return false
	}
	record.solvedMap[challengeID] = true
	record.solvedMu.Unlock()

	if sm.flagFoundChan != nil {
		sm.flagFoundChan <- FlagFoundEvent{SessionID: record.session.ID, QuestionID: challengeID}
	}

	sm.mu.Lock()
	if record.session.SolvedAt == nil {
		record.session.SolvedAt = &when
	}
	if !hasCorrectSubmission(record.session.Submissions, challengeID) {
		record.session.Submissions = append(record.session.Submissions, storage.Submission{
			ID:          uuid.NewString(),
			SessionID:   record.session.ID,
			ChallengeID: challengeID,
			IsCorrect:   true,
			CreatedAt:   when,
		})
	}
	sm.mu.Unlock()

	if err := sm.saveSessionToDisk(record.session); err != nil {
		log.Printf("warning: failed to persist solved session %s: %v", record.session.ID, err)
	}

	ctfID := record.session.Challenge.CTFID
	if ctfID != "" {
		record.commandMu.Lock()
		lastCommand := stripTerminalControlCodes(record.lastCommand)
		record.commandMu.Unlock()
		event := storage.TelemetryEvent{
			CTFID:       ctfID,
			SessionID:   record.session.ID,
			ChallengeID: challengeID,
			User:        record.session.UserID,
			LastCommand: lastCommand,
			IsCorrect:   true,
			CreatedAt:   when,
		}
		if err := sm.challengeStore.AppendTelemetry(ctfID, event); err != nil {
			log.Printf("warning: failed to append telemetry for ctf %s: %v", ctfID, err)
		}
		if record.session.UserID != "" {
			if _, err := sm.challengeStore.IncrementScore(ctfID, record.session.UserID); err != nil {
				log.Printf("warning: failed to update score for ctf %s: %v", ctfID, err)
			}
		}
	}

	record.historyMu.Lock()
	ws := record.webSocket
	record.historyMu.Unlock()
	if ws != nil {
		ws <- WebSocketMessage{Type: "flag_found", Payload: map[string]string{"challenge_id": challengeID}}
	}
	return true
}

func hasCorrectSubmission(submissions []storage.Submission, challengeID string) bool {
	for _, submission := range submissions {
		if submission.IsCorrect && submission.ChallengeID == challengeID {
			return true
		}
	}
	return false
}

func (sm *SessionManager) getRecord(sessionID string) (*sessionRecord, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	record, ok := sm.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return record, nil
}

func (sm *SessionManager) copySession(sessionID string) (*storage.Session, error) {
	record, err := sm.getRecord(sessionID)
	if err != nil {
		return nil, err
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	copy := *record.session
	copy.Submissions = append([]storage.Submission(nil), record.session.Submissions...)
	return &copy, nil
}

// GetVNCPort returns the websocket port the QEMU instance is listening on for VNC proxying.
// Returns 0 if the VM was not configured for VNC or port is unknown.
func (sm *SessionManager) GetVNCPort(sessionID string) (int, error) {
	record, err := sm.getRecord(sessionID)
	if err != nil {
		return 0, err
	}
	if record.vmManager == nil {
		return 0, nil
	}
	return record.vmManager.VNCPort, nil
}
