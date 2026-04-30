package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vm-runner/internal/storage"
)

func TestSwitchSessionChallengeReusesVMForSameCTF(t *testing.T) {
	store := storage.NewFileStore(t.TempDir())
	q1 := storage.Challenge{ID: "q1-text", CTFID: "ctf-a", Title: "Text", Validator: storage.ChallengeValidatorHMAC, Template: "flag{<hmac>}", QuestionNo: 1}
	q2 := storage.Challenge{ID: "q2-image", CTFID: "ctf-a", Title: "Image", Validator: storage.ChallengeValidatorHMAC, Template: "flag{<hmac>}", QuestionNo: 2}
	other := storage.Challenge{ID: "q1-other", CTFID: "ctf-b", Title: "Other", Validator: storage.ChallengeValidatorStatic, Flag: "flag"}
	if err := store.CreateCTF(storage.CTF{ID: "ctf-a", Title: "A", Challenges: []storage.Challenge{q1, q2}}); err != nil {
		t.Fatalf("CreateCTF(ctf-a) error = %v", err)
	}
	if err := store.CreateCTF(storage.CTF{ID: "ctf-b", Title: "B", Challenges: []storage.Challenge{other}}); err != nil {
		t.Fatalf("CreateCTF(ctf-b) error = %v", err)
	}

	sm := NewSessionManager(store, nil)
	sm.sessions = make(map[string]*sessionRecord)
	sm.runtimeBase = filepath.Join(t.TempDir(), "runtime")
	if err := os.MkdirAll(filepath.Join(sm.runtimeBase, "session-1"), 0o755); err != nil {
		t.Fatalf("failed to create runtime path: %v", err)
	}

	now := time.Now().UTC()
	sm.sessions["session-1"] = &sessionRecord{
		session: &storage.Session{
			ID:          "session-1",
			ChallengeID: q1.ID,
			Status:      storage.SessionStatusActive,
			RuntimePath: filepath.Join(sm.runtimeBase, "session-1"),
			Seed:        "00",
			CreatedAt:   now,
			ExpiresAt:   now.Add(30 * time.Minute),
			Challenge:   q1,
		},
	}

	switched, err := sm.SwitchSessionChallenge("session-1", q2.ID)
	if err != nil {
		t.Fatalf("SwitchSessionChallenge() error = %v", err)
	}
	if switched.ID != "session-1" || switched.ChallengeID != q2.ID || switched.Challenge.ID != q2.ID {
		t.Fatalf("switch returned wrong session/challenge: session=%q challenge_id=%q nested=%q", switched.ID, switched.ChallengeID, switched.Challenge.ID)
	}
	if switched.Seed != "00" || switched.Status != storage.SessionStatusActive {
		t.Fatalf("switch did not preserve active session state: seed=%q status=%q", switched.Seed, switched.Status)
	}

	_, err = sm.SwitchSessionChallenge("session-1", other.ID)
	if !errors.Is(err, ErrChallengeMismatch) {
		t.Fatalf("SwitchSessionChallenge() cross-ctf error = %v, want %v", err, ErrChallengeMismatch)
	}
	after, err := sm.GetSession("session-1")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if after.ChallengeID != q2.ID {
		t.Fatalf("cross-ctf switch changed active challenge to %q, want %q", after.ChallengeID, q2.ID)
	}
}
