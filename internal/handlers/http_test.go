package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vm-runner/internal/storage"
)

func TestUpdateCTFAllowsCreatorToEditChallenges(t *testing.T) {
	baseDir := t.TempDir()
	store := storage.NewFileStore(baseDir)
	createdAt := time.Date(2026, 4, 30, 1, 2, 3, 0, time.UTC)
	existing := storage.CTF{
		ID:         "edit-me",
		Title:      "Original",
		Maker:      "alice",
		Visibility: storage.CTFVisibilityPrivate,
		Status:     storage.CTFStatusDraft,
		VMConfig: storage.VMConfig{
			ImagePath:      "data/uploads/qcow2/base.qcow2",
			ImageFormat:    storage.DefaultMakerImageFormat,
			MemoryMB:       storage.DefaultMakerMemoryMB,
			CPUs:           storage.DefaultMakerCPUs,
			Architecture:   storage.DefaultMakerArchitecture,
			TimeoutSeconds: storage.DefaultMakerTimeoutSeconds,
			DisplayType:    "terminal",
		},
		Challenges: []storage.Challenge{
			{
				ID:          "q1-original",
				CTFID:       "edit-me",
				Title:       "Original Question",
				Description: "before",
				Validator:   storage.ChallengeValidatorHMAC,
				Template:    "flag{old+<hmac>}",
				QuestionNo:  1,
				CreatedAt:   createdAt,
				UpdatedAt:   createdAt,
			},
		},
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	if err := store.CreateCTF(existing); err != nil {
		t.Fatalf("CreateCTF() error = %v", err)
	}

	payload := storage.CTF{
		Title:      "Edited",
		Visibility: storage.CTFVisibilityPublic,
		Status:     storage.CTFStatusPublished,
		VMConfig: storage.VMConfig{
			DisplayType: "vnc",
			MemoryMB:    32768,
			CPUs:        99,
		},
		Challenges: []storage.Challenge{
			{
				ID:          "q1-original",
				Title:       "Edited Question",
				Description: "after",
				Validator:   storage.ChallengeValidatorHMAC,
				Template:    "flag{new+<hmac>}",
				QuestionNo:  1,
			},
			{
				Title:      "Second",
				Validator:  storage.ChallengeValidatorStatic,
				Flag:       "flag{static}",
				QuestionNo: 2,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/ctfs/edit-me", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-VMRunner-User", "alice")
	req.Header.Set("X-VMRunner-Role", "admin")
	rr := httptest.NewRecorder()
	NewHTTPHandler(store, nil).handleCTFActions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got storage.CTF
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.ID != existing.ID || got.Maker != existing.Maker {
		t.Fatalf("update changed immutable fields: id=%q maker=%q", got.ID, got.Maker)
	}
	if got.VMConfig.ImagePath != existing.VMConfig.ImagePath {
		t.Fatalf("image path = %q, want existing %q", got.VMConfig.ImagePath, existing.VMConfig.ImagePath)
	}
	if got.VMConfig.MemoryMB != storage.DefaultMakerMemoryMB || got.VMConfig.CPUs != storage.DefaultMakerCPUs {
		t.Fatalf("sensitive vm resources were not normalized: memory=%d cpus=%d", got.VMConfig.MemoryMB, got.VMConfig.CPUs)
	}
	if got.VMConfig.DisplayType != "vnc" {
		t.Fatalf("display type = %q, want vnc", got.VMConfig.DisplayType)
	}
	if len(got.Challenges) != 2 {
		t.Fatalf("challenge count = %d, want 2", len(got.Challenges))
	}
	if got.Challenges[0].ID != "q1-original" || !got.Challenges[0].CreatedAt.Equal(createdAt) {
		t.Fatalf("existing challenge metadata not preserved: id=%q created=%s", got.Challenges[0].ID, got.Challenges[0].CreatedAt)
	}
	if got.Challenges[1].ID != "q2-second" {
		t.Fatalf("new challenge id = %q, want q2-second", got.Challenges[1].ID)
	}

	data, err := os.ReadFile(filepath.Join(baseDir, "ctfs", "edit-me.json"))
	if err != nil {
		t.Fatalf("failed to read updated ctf file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "flag{new+<hmac>}") || strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) {
		t.Fatalf("updated ctf json did not preserve hmac template angles: %s", text)
	}
}

func TestUpdateCTFRejectsNonCreator(t *testing.T) {
	store := storage.NewFileStore(t.TempDir())
	if err := store.CreateCTF(storage.CTF{
		ID:     "creator-only",
		Title:  "Creator Only",
		Maker:  "alice",
		Status: storage.CTFStatusDraft,
		VMConfig: storage.VMConfig{
			ImagePath:   "data/uploads/qcow2/base.qcow2",
			DisplayType: "terminal",
		},
	}); err != nil {
		t.Fatalf("CreateCTF() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/ctfs/creator-only", strings.NewReader(`{"title":"Nope"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-VMRunner-User", "bob")
	req.Header.Set("X-VMRunner-Role", "admin")
	rr := httptest.NewRecorder()
	NewHTTPHandler(store, nil).handleCTFActions(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("update status = %d, want %d; body = %s", rr.Code, http.StatusForbidden, rr.Body.String())
	}
}

func TestSameActiveChallengeSessionRejectsDifferentChallenge(t *testing.T) {
	session := &storage.Session{
		ID:          "session-1",
		ChallengeID: "q3-static",
		Status:      storage.SessionStatusActive,
	}

	if !sameActiveChallengeSession(session, "q3-static") {
		t.Fatalf("sameActiveChallengeSession rejected matching active challenge")
	}
	if sameActiveChallengeSession(session, "q1-text") {
		t.Fatalf("sameActiveChallengeSession accepted mismatched challenge")
	}
	session.Status = storage.SessionStatusStopped
	if sameActiveChallengeSession(session, "q3-static") {
		t.Fatalf("sameActiveChallengeSession accepted stopped session")
	}
}
