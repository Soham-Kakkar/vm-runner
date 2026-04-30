package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"vm-runner/internal/service"
	"vm-runner/internal/storage"
)

type HTTPHandler struct {
	challengeStore *storage.FileStore
	sessionManager *service.SessionManager
}

const maxQCOW2UploadBytes = 40 << 30

func NewHTTPHandler(cs *storage.FileStore, sm *service.SessionManager) *HTTPHandler {
	return &HTTPHandler{challengeStore: cs, sessionManager: sm}
}

func (h *HTTPHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", h.handleHealth)
	mux.HandleFunc("/api/auth/register", h.handleAuth)
	mux.HandleFunc("/api/auth/login", h.handleAuth)
	mux.HandleFunc("/api/uploads/qcow2", h.handleQCOW2Upload)
	mux.HandleFunc("/api/ctfs", h.handleCTFs)
	mux.HandleFunc("/api/ctfs/", h.handleCTFActions)
	mux.HandleFunc("/api/challenges", h.handleChallenges)
	mux.HandleFunc("/api/sessions/current", h.handleGetActiveSession)
	mux.HandleFunc("/api/sessions/", h.handleSessionActions)
	mux.HandleFunc("/api/sessions", h.handleSessions)
}

func (h *HTTPHandler) handleAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req storage.User
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Password == "" {
		http.Error(w, "username and password are required", http.StatusBadRequest)
		return
	}

	if strings.Contains(r.URL.Path, "register") {
		if req.Role == "" {
			req.Role = "user"
		}
		if err := h.challengeStore.CreateUser(req); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "registered"})
	} else {
		user, err := h.challengeStore.GetUser(req.Username)
		if err != nil || user.Password != req.Password {
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		// Simplified token response for MVP
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "logged-in",
			"user":   user,
			"token":  "dummy-session-token", // In production, generate a real JWT
		})
	}
}

func (h *HTTPHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *HTTPHandler) handleQCOW2Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxQCOW2UploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, fmt.Sprintf("invalid upload: %v", err), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		file, header, err = r.FormFile("qcow2")
	}
	if err != nil {
		http.Error(w, "qcow2 file field is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	path, err := h.challengeStore.SaveUploadedQCOW2(file, header.Filename)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"image_path": path,
		"filename":   header.Filename,
	})
}

func (h *HTTPHandler) handleCTFs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ctfs, err := h.challengeStore.ListCTFs()
		if err != nil {
			http.Error(w, "failed to list ctfs", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, ctfs)
	case http.MethodPost:
		var ctf storage.CTF
		if err := json.NewDecoder(r.Body).Decode(&ctf); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(ctf.Title) == "" {
			http.Error(w, "ctf title is required", http.StatusBadRequest)
			return
		}
		ctf.Title = strings.TrimSpace(ctf.Title)
		ctfID, err := h.challengeStore.GenerateUniqueCTFID(ctf.Title)
		if err != nil {
			http.Error(w, "failed to generate ctf id", http.StatusInternalServerError)
			return
		}
		ctf.ID = ctfID
		vmConfig, err := storage.NormalizeMakerVMConfig(ctf.VMConfig)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctf.VMConfig = vmConfig
		if ctf.Status == "" {
			ctf.Status = storage.CTFStatusDraft
		}
		ctf.CreatedAt = time.Now().UTC()
		ctf.UpdatedAt = ctf.CreatedAt
		if ctf.Visibility == "" {
			ctf.Visibility = storage.CTFVisibilityPrivate
		}
		for i := range ctf.Challenges {
			if strings.TrimSpace(ctf.Challenges[i].Title) == "" {
				http.Error(w, "each question requires a name", http.StatusBadRequest)
				return
			}
			ctf.Challenges[i] = storage.NormalizeChallengeForMaker(ctf.Challenges[i], ctf.VMConfig)
			ctf.Challenges[i].CTFID = ctf.ID
			ctf.Challenges[i].CreatedAt = ctf.CreatedAt
			ctf.Challenges[i].UpdatedAt = ctf.CreatedAt
		}
		ensureUniqueChallengeIDs(ctf.Challenges)
		if err := h.challengeStore.CreateCTF(ctf); err != nil {
			http.Error(w, "failed to create ctf", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, ctf)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HTTPHandler) handleCTFActions(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/ctfs/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	ctfID := parts[0]
	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			ctf, err := h.challengeStore.GetCTF(ctfID)
			if err != nil {
				http.Error(w, "ctf not found", http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, ctf)
			return
		}
		http.NotFound(w, r)
		return
	}
	action := parts[1]
	ctf, err := h.challengeStore.GetCTF(ctfID)
	if err != nil {
		http.Error(w, "ctf not found", http.StatusNotFound)
		return
	}
	switch action {
	case "publish":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctf.Status = storage.CTFStatusPublished
		ctf.UpdatedAt = time.Now().UTC()
		if err := h.challengeStore.CreateCTF(ctf); err != nil {
			http.Error(w, "failed to publish ctf", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, ctf)
	case "disable":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctf.Status = storage.CTFStatusDisabled
		ctf.UpdatedAt = time.Now().UTC()
		if err := h.challengeStore.CreateCTF(ctf); err != nil {
			http.Error(w, "failed to disable ctf", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, ctf)
	default:
		http.NotFound(w, r)
	}
}

func (h *HTTPHandler) handleChallenges(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		challenges, err := h.challengeStore.ListChallenges()
		if err != nil {
			http.Error(w, "failed to list challenges", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, challenges)
	case http.MethodPost:
		var challenge storage.Challenge
		if err := json.NewDecoder(r.Body).Decode(&challenge); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if challenge.CTFID == "" || strings.TrimSpace(challenge.Title) == "" {
			http.Error(w, "ctf_id and question name are required", http.StatusBadRequest)
			return
		}
		ctf, err := h.challengeStore.GetCTF(challenge.CTFID)
		if err != nil {
			http.Error(w, "ctf not found", http.StatusNotFound)
			return
		}
		challenge = storage.NormalizeChallengeForMaker(challenge, ctf.VMConfig)
		challenge.CTFID = ctf.ID
		existingIDs := make(map[string]bool, len(ctf.Challenges))
		for _, existing := range ctf.Challenges {
			existingIDs[existing.ID] = true
		}
		challenge.ID = uniqueChallengeID(challenge.ID, existingIDs)
		challenge.CreatedAt = time.Now().UTC()
		challenge.UpdatedAt = challenge.CreatedAt
		if err := h.challengeStore.CreateChallenge(challenge.CTFID, challenge); err != nil {
			http.Error(w, "failed to create challenge", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, challenge)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HTTPHandler) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleCreateSession(w, r)
	case http.MethodGet:
		active := h.sessionManager.GetActiveSession()
		if active == nil {
			writeJSON(w, http.StatusOK, nil)
			return
		}
		writeJSON(w, http.StatusOK, active)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HTTPHandler) handleSessionActions(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	sessionID := parts[0]
	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			h.handleGetSession(w, r, sessionID)
			return
		}
		http.NotFound(w, r)
		return
	}
	action := parts[1]
	switch action {
	case "stop", "end":
		h.handleStopSession(w, r, sessionID)
	case "submit-answer", "submit":
		h.handleSubmitAnswer(w, r, sessionID)
	default:
		http.NotFound(w, r)
	}
}

func (h *HTTPHandler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChallengeID string `json:"challenge_id"`
		SessionID   string `json:"session_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ChallengeID == "" {
		http.Error(w, "challenge_id is required", http.StatusBadRequest)
		return
	}

	// If client provided a session id, and that session exists and is active,
	// return it immediately so the client can reattach. Otherwise, attempt to
	// start a new session using the provided id (if any).
	if req.SessionID != "" {
		if s, err := h.sessionManager.GetSession(req.SessionID); err == nil && s.Status == storage.SessionStatusActive {
			// Return existing active session
			writeJSON(w, http.StatusOK, s)
			return
		}
	}

	session, err := h.sessionManager.StartSessionWithID(req.ChallengeID, req.SessionID)
	if err != nil {
		// If a session already exists (race), try to return it.
		if errors.Is(err, service.ErrSessionExists) {
			if s, ge := h.sessionManager.GetSession(req.SessionID); ge == nil {
				writeJSON(w, http.StatusOK, s)
				return
			}
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Log session creation for debugging and observability.
	// Note: session.Seed is omitted from JSON by design.
	log.Printf("session created id=%s challenge=%s runtime=%s", session.ID, session.ChallengeID, session.RuntimePath)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           session.ID,
		"challenge_id": session.ChallengeID,
		"status":       session.Status,
		"ws_url":       "/ws/session/" + session.ID,
		"vnc_url":      "/vnc/session/" + session.ID,
		"session":      session,
	})
}

func (h *HTTPHandler) handleGetSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	session, err := h.sessionManager.GetSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (h *HTTPHandler) handleGetActiveSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	active := h.sessionManager.GetActiveSession()
	if active == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, active)
}

func (h *HTTPHandler) handleStopSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := h.sessionManager.EndSession(sessionID); err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, service.ErrSessionClosed) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (h *HTTPHandler) handleSubmitAnswer(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	correct, submission, err := h.sessionManager.SubmitAnswer(sessionID, req.Answer)
	if err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, service.ErrSessionClosed) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"is_correct": correct,
		"submission": submission,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

func ensureUniqueChallengeIDs(challenges []storage.Challenge) {
	seen := make(map[string]bool, len(challenges))
	for i := range challenges {
		challenges[i].ID = uniqueChallengeID(challenges[i].ID, seen)
	}
}

func uniqueChallengeID(id string, seen map[string]bool) string {
	if id == "" {
		id = "q"
	}
	base := id
	for i := 2; seen[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	seen[id] = true
	return id
}
