package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGenerateUniqueCTFID(t *testing.T) {
	store := NewFileStore(t.TempDir())

	id, err := store.GenerateUniqueCTFID("SQL Injection Lab!")
	if err != nil {
		t.Fatalf("GenerateUniqueCTFID() error = %v", err)
	}
	if ok, _ := regexp.MatchString(`^sql-injection-lab-[0-9A-Za-z_-]{6}$`, id); !ok {
		t.Fatalf("generated id %q does not match expected slug+nanoid format", id)
	}
}

func TestSaveUploadedQCOW2(t *testing.T) {
	store := NewFileStore(t.TempDir())
	content := append([]byte{'Q', 'F', 'I', 0xfb}, []byte("qcow2 payload")...)

	path, err := store.SaveUploadedQCOW2(bytes.NewReader(content), "Base Image.qcow2")
	if err != nil {
		t.Fatalf("SaveUploadedQCOW2() error = %v", err)
	}
	if !strings.HasSuffix(path, ".qcow2") {
		t.Fatalf("saved path %q does not end with .qcow2", path)
	}
	if filepath.Base(filepath.Dir(path)) != "qcow2" {
		t.Fatalf("saved path %q is not under qcow2 upload directory", path)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved upload: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("saved upload content mismatch")
	}
}

func TestSaveUploadedQCOW2RejectsInvalidImage(t *testing.T) {
	store := NewFileStore(t.TempDir())

	if _, err := store.SaveUploadedQCOW2(strings.NewReader("not qcow2"), "image.qcow2"); err == nil {
		t.Fatalf("SaveUploadedQCOW2() accepted invalid qcow2 content")
	}
	if _, err := store.SaveUploadedQCOW2(bytes.NewReader([]byte{'Q', 'F', 'I', 0xfb}), "image.img"); err == nil {
		t.Fatalf("SaveUploadedQCOW2() accepted non-qcow2 extension")
	}
}

func TestNormalizeChallengeForMakerGeneratesQuestionID(t *testing.T) {
	challenge := NormalizeChallengeForMaker(Challenge{
		Title:      "Find Root Flag",
		Validator:  ChallengeValidatorHMAC,
		Template:   "flag{<hmac>}",
		QuestionNo: 3,
	}, VMConfig{ImagePath: "base.qcow2"})

	if challenge.ID != "q3-find-root-flag" {
		t.Fatalf("generated challenge id = %q, want q3-find-root-flag", challenge.ID)
	}
}

func TestCreateCTFPreservesFlagTemplateAngles(t *testing.T) {
	store := NewFileStore(t.TempDir())
	ctf := CTF{
		ID:     "angle-test",
		Title:  "Angle Test",
		Status: CTFStatusDraft,
		Challenges: []Challenge{
			{
				ID:         "q1-angle",
				Title:      "Angle",
				Validator:  ChallengeValidatorHMAC,
				Template:   "flag{<hmac>}",
				QuestionNo: 1,
			},
		},
	}

	if err := store.CreateCTF(ctf); err != nil {
		t.Fatalf("CreateCTF() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(store.ctfPath, "angle-test.json"))
	if err != nil {
		t.Fatalf("failed to read ctf file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "flag{<hmac>}") {
		t.Fatalf("ctf json did not preserve literal angle template: %s", text)
	}
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) {
		t.Fatalf("ctf json contains escaped angle brackets: %s", text)
	}
}
