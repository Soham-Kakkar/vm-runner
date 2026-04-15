package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	handler "vm-runner/internal/handlers"
	"vm-runner/internal/service"
	"vm-runner/internal/storage"
)

func main() {
	dataDir := os.Getenv("VMRUNNER_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}
	_ = os.MkdirAll(filepath.Join(dataDir, "runtime"), 0o755)
	challengeStore := storage.NewFileStore(dataDir)

	flagFoundChan := make(chan service.FlagFoundEvent, 10)
	sessionManager := service.NewSessionManager(challengeStore, flagFoundChan)
	httpHandler := handler.NewHTTPHandler(challengeStore, sessionManager)
	wsHandler := handler.NewWebSocketHandler(sessionManager)

	go func() {
		for event := range flagFoundChan {
			log.Printf("submission event received: session=%s challenge=%s", event.SessionID, event.QuestionID)
		}
	}()

	mux := http.NewServeMux()
	httpHandler.RegisterRoutes(mux)
	mux.HandleFunc("/ws/", wsHandler.ServeWS)
	mux.HandleFunc("/ws/session/", wsHandler.ServeWS)
	mux.HandleFunc("/vnc/", wsHandler.ServeVNC)
	mux.HandleFunc("/vnc/session/", wsHandler.ServeVNC)

	fs := http.FileServer(http.Dir("./web"))
	mux.Handle("/", fs)
	// Wrap the mux with a simple CORS middleware to allow browser clients
	// (useful during development when frontend may be served from a different origin).
	handler := corsMiddleware(mux)
	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal(err)
	}
}

// corsMiddleware adds permissive CORS headers for development and handles
// OPTIONS preflight requests. For production, restrict the allowed origin.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: tighten this in production to a specific origin
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
