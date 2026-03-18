package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"vm-runner/internal/service"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow all connections for MVP
		return true
	},
}

// WebSocketHandler handles WebSocket connections.
type WebSocketHandler struct {
	sessionManager *service.SessionManager
	hub            *Hub
}

// NewWebSocketHandler creates a new WebSocketHandler.
func NewWebSocketHandler(sm *service.SessionManager) *WebSocketHandler {
	hub := NewHub(sm)
	go hub.Run()
	return &WebSocketHandler{
		sessionManager: sm,
		hub:            hub,
	}
}

// ServeWS handles WebSocket requests.
func (h *WebSocketHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/ws/")
	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection for session %s: %v", sessionID, err)
		return
	}

	client := &Client{
		hub:       h.hub,
		conn:      conn,
		send:      make(chan service.WebSocketMessage, 256),
		sessionID: sessionID,
	}
	client.hub.register <- client

	go client.writePump()
	go client.readPump()
}

// --- Hub and Client implementation ---

// Hub maintains the set of active clients and broadcasts messages.
type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	sessionMgr *service.SessionManager
}

func NewHub(sm *service.SessionManager) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		sessionMgr: sm,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			// Associate the client's send channel with the session
			h.sessionMgr.RegisterWebSocket(client.sessionID, client.send)
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		}
	}
}

// Client is a middleman between the WebSocket connection and the hub.
type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan service.WebSocketMessage
	sessionID string
}

type clientInput struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}


func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		
		var input clientInput
		if err := json.Unmarshal(message, &input); err != nil {
			log.Printf("Error unmarshalling client input: %v", err)
			continue
		}

		if input.Type == "input" {
			c.hub.sessionMgr.HandleVMInput(c.sessionID, input.Payload)
		}
	}
}

func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()
	for {
		message, ok := <-c.send
		if !ok {
			// The hub closed the channel.
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}

		w, err := c.conn.NextWriter(websocket.TextMessage)
		if err != nil {
			return
		}
		
		payload, _ := json.Marshal(message)
		w.Write(payload)


		if err := w.Close(); err != nil {
			return
		}
	}
}
