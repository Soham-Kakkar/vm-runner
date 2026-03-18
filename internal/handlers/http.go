package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"vm-runner/internal/service"
	"vm-runner/internal/storage"
)

// HTTPHandler holds the dependencies for HTTP handlers.
type HTTPHandler struct {
	challengeStore *storage.FileStore
	sessionManager *service.SessionManager
}

// NewHTTPHandler creates a new HTTPHandler.
func NewHTTPHandler(cs *storage.FileStore, sm *service.SessionManager) *HTTPHandler {
	return &HTTPHandler{
		challengeStore: cs,
		sessionManager: sm,
	}
}

// RegisterRoutes sets up the HTTP routes.
func (h *HTTPHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/challenges", h.handleListChallenges)
	mux.HandleFunc("/api/sessions", h.handleCreateSession)
	mux.HandleFunc("/api/sessions/current", h.handleGetActiveSession)
	mux.HandleFunc("/api/sessions/", h.handleSessionRoutes) // Note the trailing slash
}

// handleListChallenges returns a list of all available challenges.
func (h *HTTPHandler) handleListChallenges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	challenges, err := h.challengeStore.ListChallenges()
	if err != nil {
		http.Error(w, "Failed to list challenges", http.StatusInternalServerError)
		return
	}

	// Create a simpler response model for the frontend
	type challengeResponse struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		QuestionCount int    `json:"question_count"`
	}
	response := make([]challengeResponse, len(challenges))
	for i, ch := range challenges {
		response[i] = challengeResponse{
			ID:           ch.ID,
			Name:         ch.Name,
			QuestionCount: len(ch.Questions),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCreateSession starts a new challenge session.
func (h *HTTPHandler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ChallengeID string `json:"challenge_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := h.sessionManager.StartSession(req.ChallengeID)
	if err == service.ErrSessionExists {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "Failed to start session", http.StatusInternalServerError)
		return
	}

	h.respondWithSession(w, session)
}

func (h *HTTPHandler) handleGetActiveSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session := h.sessionManager.GetActiveSession()
	if session == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nil)
		return
	}

	h.respondWithSession(w, session)
}

func (h *HTTPHandler) respondWithSession(w http.ResponseWriter, session *service.Session) {
	type questionResponse struct {
		ID          string `json:"id"`
		Order       int    `json:"order"`
		Text        string `json:"text"`
		IsCompleted bool   `json:"is_completed"`
	}
	
	questions := make([]questionResponse, len(session.Challenge.Questions))
	for i, q := range session.Challenge.Questions {
		questions[i] = questionResponse{
			ID: q.ID,
			Order: q.Order,
			Text: q.Text,
			IsCompleted: session.CompletedQuestions[q.ID],
		}
	}

	response := map[string]interface{}{
		"session_id":    session.ID,
		"challenge_id":  session.Challenge.ID,
		"websocket_url": "/ws/" + session.ID,
		"questions":     questions,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}


// handleSessionRoutes delegates requests for /api/sessions/:id/...
func (h *HTTPHandler) handleSessionRoutes(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/")
	
	if len(pathParts) < 2 {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	sessionID := pathParts[0]
	action := pathParts[1]
	
	switch action {
	case "submit":
		h.handleSubmitAnswer(w, r, sessionID)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}


// handleSubmitAnswer handles answer submission for a session.
func (h *HTTPHandler) handleSubmitAnswer(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	correct, questionID := h.sessionManager.SubmitAnswer(sessionID, req.Answer)

	response := map[string]interface{}{
		"correct":     correct,
		"question_id": questionID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
