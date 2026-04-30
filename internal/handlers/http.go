package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
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

	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	if req.Username == "" || req.Password == "" {
		http.Error(w, "username and password are required", http.StatusBadRequest)
		return
	}

	if strings.Contains(r.URL.Path, "register") {
		req.Name = strings.TrimSpace(req.Name)
		req.ExternalID = strings.TrimSpace(req.ExternalID)
		if req.Name == "" || req.ExternalID == "" {
			http.Error(w, "name and id are required", http.StatusBadRequest)
			return
		}
		req.Role = normalizeRole(req.Role)
		if req.Role == "" {
			req.Role = "user"
		}
		if req.Role != "user" && req.Role != "admin" {
			http.Error(w, "role must be user or admin", http.StatusBadRequest)
			return
		}
		if err := h.challengeStore.CreateUser(req); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "registered"})
		return
	}

	user, err := h.challengeStore.GetUser(req.Username)
	if err != nil || user.Password != req.Password {
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "logged-in",
		"user":   user,
		"token":  "dummy-session-token",
	})
}

func (h *HTTPHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *HTTPHandler) handleQCOW2Upload(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	if !isAdmin(user) {
		http.Error(w, "admin access required", http.StatusForbidden)
		return
	}
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
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		ctfs, err := h.challengeStore.ListCTFs()
		if err != nil {
			http.Error(w, "failed to list ctfs", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, filterCTFsForUser(ctfs, user))
	case http.MethodPost:
		if !isAdmin(user) {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}
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
		ctf.Status = normalizeCTFStatus(ctf.Status, storage.CTFStatusDraft)
		ctf.CreatedAt = time.Now().UTC()
		ctf.UpdatedAt = ctf.CreatedAt
		ctf.Visibility = normalizeCTFVisibility(ctf.Visibility, storage.CTFVisibilityPrivate)
		ctf.Maker = user.Username
		ctf.OwnerID = user.Username
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
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/ctfs/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	ctfID := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			ctf, err := h.challengeStore.GetCTF(ctfID)
			if err != nil {
				http.Error(w, "ctf not found", http.StatusNotFound)
				return
			}
			if !canViewCTF(user, ctf) {
				http.Error(w, "ctf not accessible", http.StatusForbidden)
				return
			}
			writeJSON(w, http.StatusOK, ctf)
			return
		case http.MethodPut, http.MethodPatch:
			h.handleUpdateCTF(w, r, ctfID, user)
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
		if !canEditCTFUser(user, ctf) {
			http.Error(w, "only the ctf creator can publish", http.StatusForbidden)
			return
		}
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
		if !canEditCTFUser(user, ctf) {
			http.Error(w, "only the ctf creator can disable", http.StatusForbidden)
			return
		}
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
	case "telemetry":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !canEditCTFUser(user, ctf) {
			http.Error(w, "only the ctf creator can view telemetry", http.StatusForbidden)
			return
		}
		events, err := h.challengeStore.ListTelemetry(ctfID)
		if err != nil {
			http.Error(w, "failed to load telemetry", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, events)
	case "scores":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !canEditCTFUser(user, ctf) {
			http.Error(w, "only the ctf creator can view scores", http.StatusForbidden)
			return
		}
		scores, err := h.challengeStore.GetScores(ctfID)
		if err != nil {
			http.Error(w, "failed to load scores", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, scores)
	default:
		http.NotFound(w, r)
	}
}

func (h *HTTPHandler) handleUpdateCTF(w http.ResponseWriter, r *http.Request, ctfID string, user *storage.User) {
	existing, err := h.challengeStore.GetCTF(ctfID)
	if err != nil {
		http.Error(w, "ctf not found", http.StatusNotFound)
		return
	}
	if !canEditCTFUser(user, existing) {
		http.Error(w, "only the ctf creator can edit this ctf", http.StatusForbidden)
		return
	}

	var req storage.CTF
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	updated, err := normalizeCTFUpdate(existing, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	updated.Maker = existing.Maker
	updated.OwnerID = existing.OwnerID
	if err := h.challengeStore.CreateCTF(updated); err != nil {
		http.Error(w, "failed to update ctf", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *HTTPHandler) handleChallenges(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		challenges, err := h.challengeStore.ListChallenges()
		if err != nil {
			http.Error(w, "failed to list challenges", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, challenges)
	case http.MethodPost:
		if !isAdmin(user) {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}
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
		if !canEditCTFUser(user, ctf) {
			http.Error(w, "only the ctf creator can edit questions", http.StatusForbidden)
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
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPost:
		h.handleCreateSession(w, r, user)
	case http.MethodGet:
		active := h.sessionManager.GetActiveSession()
		if active == nil || (active.UserID != "" && active.UserID != user.Username && !isAdmin(user)) {
			writeJSON(w, http.StatusOK, nil)
			return
		}
		writeJSON(w, http.StatusOK, active)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HTTPHandler) handleSessionActions(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	sessionID := parts[0]
	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			h.handleGetSession(w, r, sessionID, user)
			return
		}
		http.NotFound(w, r)
		return
	}
	action := parts[1]
	switch action {
	case "stop", "end":
		h.handleStopSession(w, r, sessionID, user)
	case "submit-answer", "submit":
		h.handleSubmitAnswer(w, r, sessionID, user)
	default:
		http.NotFound(w, r)
	}
}

func (h *HTTPHandler) handleCreateSession(w http.ResponseWriter, r *http.Request, user *storage.User) {
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

	challenge, err := h.challengeStore.GetChallenge(req.ChallengeID)
	if err != nil {
		http.Error(w, "challenge not found", http.StatusNotFound)
		return
	}
	ctf, err := h.challengeStore.GetCTF(challenge.CTFID)
	if err != nil {
		http.Error(w, "ctf not found", http.StatusNotFound)
		return
	}
	if !canViewCTF(user, ctf) {
		http.Error(w, "ctf not accessible", http.StatusForbidden)
		return
	}

	if req.SessionID != "" {
		if s, err := h.sessionManager.GetSession(req.SessionID); err == nil && s.Status == storage.SessionStatusActive {
			if s.UserID != "" && s.UserID != user.Username && !isAdmin(user) {
				http.Error(w, "session belongs to another user", http.StatusForbidden)
				return
			}
			if sameActiveChallengeSession(s, req.ChallengeID) {
				writeJSON(w, http.StatusOK, s)
				return
			}
			switched, err := h.sessionManager.SwitchSessionChallenge(req.SessionID, req.ChallengeID)
			if err == nil {
				writeJSON(w, http.StatusOK, switched)
				return
			}
			if errors.Is(err, service.ErrChallengeMismatch) {
				if stopErr := h.sessionManager.EndSession(req.SessionID); stopErr != nil && !errors.Is(stopErr, service.ErrSessionClosed) {
					log.Printf("warning: failed to stop mismatched session %s before challenge switch: %v", req.SessionID, stopErr)
				}
				req.SessionID = ""
			} else if errors.Is(err, service.ErrSessionClosed) || errors.Is(err, service.ErrSessionNotFound) {
				req.SessionID = ""
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	session, err := h.sessionManager.StartSessionWithID(req.ChallengeID, req.SessionID, user.Username)
	if err != nil {
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

func sameActiveChallengeSession(session *storage.Session, challengeID string) bool {
	return session != nil &&
		session.Status == storage.SessionStatusActive &&
		session.ChallengeID == challengeID
}

func (h *HTTPHandler) handleGetSession(w http.ResponseWriter, r *http.Request, sessionID string, user *storage.User) {
	session, err := h.sessionManager.GetSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if session.UserID != "" && session.UserID != user.Username && !isAdmin(user) {
		http.Error(w, "session belongs to another user", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (h *HTTPHandler) handleGetActiveSession(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	active := h.sessionManager.GetActiveSession()
	if active == nil || (active.UserID != "" && active.UserID != user.Username && !isAdmin(user)) {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, active)
}

func (h *HTTPHandler) handleStopSession(w http.ResponseWriter, r *http.Request, sessionID string, user *storage.User) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session, err := h.sessionManager.GetSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if session.UserID != "" && session.UserID != user.Username && !isAdmin(user) {
		http.Error(w, "session belongs to another user", http.StatusForbidden)
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

func (h *HTTPHandler) handleSubmitAnswer(w http.ResponseWriter, r *http.Request, sessionID string, user *storage.User) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session, err := h.sessionManager.GetSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if session.UserID != "" && session.UserID != user.Username && !isAdmin(user) {
		http.Error(w, "session belongs to another user", http.StatusForbidden)
		return
	}

	var req struct {
		Answer      string `json:"answer"`
		LastCommand string `json:"last_command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	correct, submission, err := h.sessionManager.SubmitAnswer(sessionID, req.Answer, req.LastCommand)
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

func normalizeCTFUpdate(existing storage.CTF, req storage.CTF) (storage.CTF, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return storage.CTF{}, fmt.Errorf("ctf title is required")
	}

	vmInput := req.VMConfig
	if strings.TrimSpace(vmInput.ImagePath) == "" {
		vmInput.ImagePath = existing.VMConfig.ImagePath
	}
	if strings.TrimSpace(vmInput.DisplayType) == "" {
		vmInput.DisplayType = existing.VMConfig.DisplayType
	}
	vmConfig, err := storage.NormalizeMakerVMConfig(vmInput)
	if err != nil {
		return storage.CTF{}, err
	}

	now := time.Now().UTC()
	if existing.CreatedAt.IsZero() {
		existing.CreatedAt = now
	}
	updated := existing
	updated.Title = title
	updated.Visibility = normalizeCTFVisibility(req.Visibility, existing.Visibility)
	updated.Status = normalizeCTFStatus(req.Status, existing.Status)
	updated.VMConfig = vmConfig
	updated.UpdatedAt = now

	existingChallenges := make(map[string]storage.Challenge, len(existing.Challenges))
	for _, challenge := range existing.Challenges {
		if challenge.ID != "" {
			existingChallenges[challenge.ID] = challenge
		}
	}

	updated.Challenges = make([]storage.Challenge, 0, len(req.Challenges))
	for i := range req.Challenges {
		if strings.TrimSpace(req.Challenges[i].Title) == "" {
			return storage.CTF{}, fmt.Errorf("each question requires a name")
		}
		challenge := storage.NormalizeChallengeForMaker(req.Challenges[i], vmConfig)
		challenge.CTFID = existing.ID
		challenge.UpdatedAt = now
		if old, ok := existingChallenges[challenge.ID]; ok && !old.CreatedAt.IsZero() {
			challenge.CreatedAt = old.CreatedAt
		} else {
			challenge.CreatedAt = now
		}
		updated.Challenges = append(updated.Challenges, challenge)
	}
	ensureUniqueChallengeIDs(updated.Challenges)
	return updated, nil
}

func normalizeCTFVisibility(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case storage.CTFVisibilityPublic, storage.CTFVisibilityPrivate:
		return value
	case "":
		if fallback != "" {
			return fallback
		}
	}
	return storage.CTFVisibilityPrivate
}

func normalizeCTFStatus(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case storage.CTFStatusDraft, storage.CTFStatusPublished, storage.CTFStatusDisabled:
		return value
	case "":
		if fallback != "" {
			return fallback
		}
	}
	return storage.CTFStatusDraft
}

func normalizeRole(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "admin", "user":
		return value
	default:
		return "user"
	}
}

func (h *HTTPHandler) requireUser(w http.ResponseWriter, r *http.Request) (*storage.User, bool) {
	username := strings.TrimSpace(r.Header.Get("X-VMRunner-User"))
	role := normalizeRole(r.Header.Get("X-VMRunner-Role"))
	if username == "" || role == "" {
		http.Error(w, "login required", http.StatusUnauthorized)
		return nil, false
	}
	user, err := h.challengeStore.GetUser(username)
	if err != nil {
		http.Error(w, "invalid user", http.StatusUnauthorized)
		return nil, false
	}
	if normalizeRole(user.Role) != role {
		http.Error(w, "invalid role", http.StatusForbidden)
		return nil, false
	}
	return user, true
}

func isAdmin(user *storage.User) bool {
	return user != nil && normalizeRole(user.Role) == "admin"
}

func canEditCTFUser(user *storage.User, ctf storage.CTF) bool {
	if user == nil {
		return false
	}
	creator := strings.TrimSpace(ctf.Maker)
	if creator == "" {
		creator = strings.TrimSpace(ctf.OwnerID)
	}
	if creator == "" || creator == "unknown" || creator == "system" {
		return isAdmin(user)
	}
	return user.Username != "" && user.Username == creator
}

func canViewCTF(user *storage.User, ctf storage.CTF) bool {
	if user == nil {
		return false
	}
	if isAdmin(user) || canEditCTFUser(user, ctf) {
		return true
	}
	return ctf.Visibility == storage.CTFVisibilityPublic
}

func filterCTFsForUser(ctfs []storage.CTF, user *storage.User) []storage.CTF {
	if user == nil {
		return []storage.CTF{}
	}
	filtered := make([]storage.CTF, 0, len(ctfs))
	for _, ctf := range ctfs {
		if canViewCTF(user, ctf) {
			filtered = append(filtered, ctf)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].CreatedAt.After(filtered[j].CreatedAt) })
	return filtered
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
