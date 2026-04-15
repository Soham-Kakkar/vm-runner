package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"vm-runner/internal/service"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type WebSocketHandler struct {
	sessionManager *service.SessionManager
}

func NewWebSocketHandler(sm *service.SessionManager) *WebSocketHandler {
	return &WebSocketHandler{sessionManager: sm}
}

func (h *WebSocketHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/ws/session/")
	if sessionID == r.URL.Path {
		sessionID = strings.TrimPrefix(r.URL.Path, "/ws/")
	}
	if sessionID == "" {
		http.Error(w, "session id is required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Log headers to help diagnose non-websocket requests hitting this endpoint.
		log.Printf("failed to upgrade websocket for session %s: %v; remote=%s; Upgrade=%q; Connection=%q", sessionID, err, r.RemoteAddr, r.Header.Get("Upgrade"), r.Header.Get("Connection"))
		for name, vals := range r.Header {
			log.Printf("WS header %s: %v", name, vals)
		}
		http.Error(w, "websocket upgrade failed", http.StatusBadRequest)
		return
	}

	log.Printf("websocket connection established for session %s from %s", sessionID, r.RemoteAddr)
	send := make(chan service.WebSocketMessage, 256)
	if err := h.sessionManager.RegisterWebSocket(sessionID, send); err != nil {
		_ = conn.Close()
		return
	}

	go func() {
		defer conn.Close()
		for msg := range send {
			payload, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		}
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var input struct {
			Type    string `json:"type"`
			Payload string `json:"payload"`
		}
		if err := json.Unmarshal(message, &input); err != nil {
			log.Printf("invalid websocket payload for session %s: %v", sessionID, err)
			continue
		}
		// log.Printf("ws message for session %s: type=%s len=%d", sessionID, input.Type, len(input.Payload))
		if input.Type == "input" {
			if err := h.sessionManager.HandleVMInput(sessionID, input.Payload); err != nil {
				log.Printf("error handling vm input for session %s: %v", sessionID, err)
			}
		}
	}
}

func (h *WebSocketHandler) ServeVNC(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/vnc/session/")
	if sessionID == r.URL.Path {
		sessionID = strings.TrimPrefix(r.URL.Path, "/vnc/")
	}
	if sessionID == "" {
		http.Error(w, "session id is required", http.StatusBadRequest)
		return
	}

	session, err := h.sessionManager.GetSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if session.Challenge.VMConfig.DisplayType != "vnc" {
		http.Error(w, "session does not expose vnc", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("failed to upgrade vnc websocket for session %s: %v", sessionID, err)
		return
	}
	defer conn.Close()

	var qemuConn *websocket.Conn
	dialer := websocket.Dialer{}
	// Prefer the port configured on the session's qemu manager; fall back to 5701.
	port := 5701
	if p, err := h.sessionManager.GetVNCPort(sessionID); err == nil && p > 0 {
		port = p
	}
	for i := 0; i < 10; i++ {
		qemuConn, _, err = dialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d", port), nil)
		if err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		log.Printf("failed to connect to qemu vnc for session %s: %v", sessionID, err)
		return
	}
	defer qemuConn.Close()

	errChan := make(chan error, 2)
	go func() {
		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			if err := qemuConn.WriteMessage(mt, message); err != nil {
				errChan <- err
				return
			}
		}
	}()
	go func() {
		for {
			mt, message, err := qemuConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			if err := conn.WriteMessage(mt, message); err != nil {
				errChan <- err
				return
			}
		}
	}()

	<-errChan
}
