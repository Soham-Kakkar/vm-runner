package main

import (
	"log"
	"net/http"

	"vm-runner/internal/handlers"
	"vm-runner/internal/service"
	"vm-runner/internal/storage"
)

func main() {
	// Configuration (for now, hardcoded path)
	challengeStore := storage.NewFileStore("./data/challenges")

	// Create a channel for flag found events
	flagFoundChan := make(chan service.FlagFoundEvent, 10)

	// Services
	sessionManager := service.NewSessionManager(challengeStore, flagFoundChan)

	// Handlers
	httpHandler := handler.NewHTTPHandler(challengeStore, sessionManager)
	wsHandler := handler.NewWebSocketHandler(sessionManager)

	// Listen for flag found events and notify the appropriate WebSocket client
	go func() {
		for event := range flagFoundChan {
			session, err := sessionManager.GetSession(event.SessionID)
			if err == nil && session.WebSocket != nil {
				session.WebSocket <- service.WebSocketMessage{
					Type: "flag_found",
					Payload: map[string]string{
						"question_id": event.QuestionID,
					},
				}
			}
		}
	}()

	// Register routes
	mux := http.NewServeMux()
	httpHandler.RegisterRoutes(mux)
	mux.HandleFunc("/ws/", wsHandler.ServeWS)

	// Serve static files
	fs := http.FileServer(http.Dir("./web"))
	mux.Handle("/", fs)

	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
