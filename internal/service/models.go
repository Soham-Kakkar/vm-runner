package service

// WebSocketMessage defines the structure for messages sent over the WebSocket.
type WebSocketMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// FlagFoundEvent is used to notify when a flag is automatically detected.
type FlagFoundEvent struct {
	SessionID  string `json:"session_id"`
	QuestionID string `json:"question_id"`
}
