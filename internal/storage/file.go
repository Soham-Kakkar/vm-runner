package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	CTFStatusDraft     = "draft"
	CTFStatusPublished = "published"
	CTFStatusDisabled  = "disabled"

	CTFVisibilityPublic  = "public"
	CTFVisibilityPrivate = "private"

	ChallengeValidatorStatic = "static"
	ChallengeValidatorHMAC   = "hmac"

	SessionStatusActive  = "active"
	SessionStatusStopped = "stopped"
	SessionStatusExpired = "expired"
)

type VMConfig struct {
	ImagePath      string `json:"image_path"`
	ImageFormat    string `json:"image_format"`
	MemoryMB       int    `json:"memory_mb"`
	CPUs           int    `json:"cpus"`
	Architecture   string `json:"architecture"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	DisplayType    string `json:"display_type"`
}

type Challenge struct {
	ID          string    `json:"id"`
	CTFID       string    `json:"ctf_id,omitempty"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	ImageID     string    `json:"image_id"`
	Validator   string    `json:"validator"`
	Flag        string    `json:"flag,omitempty"`
	Template    string    `json:"template,omitempty"`
	QuestionNo  int       `json:"question_no,omitempty"`
	VMConfig    VMConfig  `json:"vm_config"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CTF struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	OwnerID    string      `json:"owner_id"`
	Maker      string      `json:"maker"` // Username of the creator
	Visibility string      `json:"visibility"`
	VMConfig   VMConfig    `json:"vm_config,omitempty"`
	StartTime  *time.Time  `json:"start_time,omitempty"`
	EndTime    *time.Time  `json:"end_time,omitempty"`
	Status     string      `json:"status"`
	Challenges []Challenge `json:"challenges"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type Submission struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"session_id"`
	ChallengeID string    `json:"challenge_id"`
	Answer      string    `json:"answer"`
	IsCorrect   bool      `json:"is_correct"`
	CreatedAt   time.Time `json:"created_at"`
}

type Session struct {
	ID           string       `json:"id"`
	ChallengeID  string       `json:"challenge_id"`
	UserID       string       `json:"user_id,omitempty"`
	Status       string       `json:"status"`
	RuntimePath  string       `json:"runtime_path"`
	Seed         string       `json:"-"`
	LastActivity *time.Time   `json:"last_activity,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	ExpiresAt    time.Time    `json:"expires_at"`
	SolvedAt     *time.Time   `json:"solved_at,omitempty"`
	StoppedAt    *time.Time   `json:"stopped_at,omitempty"`
	Challenge    Challenge    `json:"challenge"`
	Submissions  []Submission `json:"submissions,omitempty"`
}

type User struct {
	Username string    `json:"username"`
	Password string    `json:"password"` // In a real app, this should be hashed
	Role     string    `json:"role"`     // "admin" or "user"
	CreatedAt time.Time `json:"created_at"`
}

type FileStore struct {
	basePath   string
	ctfPath    string
	userPath   string
	legacyPath string
}

func NewFileStore(basePath string) *FileStore {
	return &FileStore{
		basePath:   basePath,
		ctfPath:    filepath.Join(basePath, "ctfs"),
		userPath:   filepath.Join(basePath, "users.json"),
		legacyPath: filepath.Join(basePath, "challenges"),
	}
}

func (s *FileStore) GetUser(username string) (*User, error) {
	users, err := s.loadUsers()
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.Username == username {
			return &u, nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

func (s *FileStore) CreateUser(user User) error {
	users, err := s.loadUsers()
	if err != nil {
		users = []User{}
	}
	for _, u := range users {
		if u.Username == user.Username {
			return fmt.Errorf("user already exists")
		}
	}
	user.CreatedAt = time.Now().UTC()
	users = append(users, user)
	return s.saveUsers(users)
}

func (s *FileStore) loadUsers() ([]User, error) {
	data, err := os.ReadFile(s.userPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []User{}, nil
		}
		return nil, err
	}
	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func (s *FileStore) saveUsers(users []User) error {
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.userPath, data, 0o644)
}

func (s *FileStore) ListCTFs() ([]CTF, error) {
	ctfs, err := s.loadCTFDirectory()
	if err != nil {
		return nil, err
	}
	if len(ctfs) > 0 {
		return ctfs, nil
	}
	return s.loadLegacyCTFs()
}

func (s *FileStore) GetCTF(id string) (CTF, error) {
	ctfs, err := s.ListCTFs()
	if err != nil {
		return CTF{}, err
	}
	for _, ctf := range ctfs {
		if ctf.ID == id {
			return ctf, nil
		}
	}
	return CTF{}, fmt.Errorf("ctf %q not found", id)
}

func (s *FileStore) ListChallenges() ([]Challenge, error) {
	ctfs, err := s.ListCTFs()
	if err != nil {
		return nil, err
	}
	challenges := make([]Challenge, 0)
	for _, ctf := range ctfs {
		challenges = append(challenges, ctf.Challenges...)
	}
	sort.Slice(challenges, func(i, j int) bool {
		if challenges[i].CTFID == challenges[j].CTFID {
			return challenges[i].ID < challenges[j].ID
		}
		return challenges[i].CTFID < challenges[j].CTFID
	})
	return challenges, nil
}

func (s *FileStore) GetChallenge(id string) (Challenge, error) {
	ctfs, err := s.ListCTFs()
	if err != nil {
		return Challenge{}, err
	}
	for _, ctf := range ctfs {
		for _, challenge := range ctf.Challenges {
			if challenge.ID == id {
				return challenge, nil
			}
		}
	}
	return Challenge{}, fmt.Errorf("challenge %q not found", id)
}

func (s *FileStore) CreateCTF(ctf CTF) error {
	if err := os.MkdirAll(s.ctfPath, 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.ctfPath, ctf.ID+".json")
	data, err := json.MarshalIndent(ctf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *FileStore) CreateChallenge(ctfID string, challenge Challenge) error {
	ctf, err := s.GetCTF(ctfID)
	if err != nil {
		return err
	}
	challenge.CTFID = ctfID
	challenge.CreatedAt = time.Now().UTC()
	challenge.UpdatedAt = challenge.CreatedAt
	ctf.Challenges = append(ctf.Challenges, challenge)
	ctf.UpdatedAt = time.Now().UTC()
	return s.CreateCTF(ctf)
}

func (s *FileStore) loadCTFDirectory() ([]CTF, error) {
	entries, err := os.ReadDir(s.ctfPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	ctfs := make([]CTF, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		ctf, err := s.readCTF(filepath.Join(s.ctfPath, entry.Name()))
		if err != nil {
			return nil, err
		}
		ctfs = append(ctfs, ctf)
	}
	return ctfs, nil
}

func (s *FileStore) loadLegacyCTFs() ([]CTF, error) {
	entries, err := os.ReadDir(s.legacyPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	ctfs := make([]CTF, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		configPath := filepath.Join(s.legacyPath, entry.Name(), "config.json")
		if _, err := os.Stat(configPath); err != nil {
			continue
		}
		legacy, err := s.readLegacyChallenge(configPath)
		if err != nil {
			return nil, err
		}
		ctfs = append(ctfs, CTF{
			ID:         legacy.CTFID,
			Title:      legacy.Title,
			OwnerID:    "system",
			Visibility: CTFVisibilityPublic,
			Status:     CTFStatusPublished,
			Challenges: []Challenge{legacy},
			CreatedAt:  legacy.CreatedAt,
			UpdatedAt:  legacy.UpdatedAt,
		})
	}
	return ctfs, nil
}

func (s *FileStore) readCTF(path string) (CTF, error) {
	var ctf CTF
	data, err := os.ReadFile(path)
	if err != nil {
		return ctf, fmt.Errorf("failed to read ctf file %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &ctf); err != nil {
		return ctf, fmt.Errorf("failed to unmarshal ctf file %s: %w", path, err)
	}
	for i := range ctf.Challenges {
		ctf.Challenges[i].CTFID = ctf.ID
		// If the challenge does not specify an image path, fall back to the CTF-level vm_config
		if ctf.Challenges[i].VMConfig.ImagePath == "" && ctf.VMConfig.ImagePath != "" {
			ctf.Challenges[i].VMConfig = ctf.VMConfig
		} else {
			ctf.Challenges[i].VMConfig.ImagePath = s.resolveImagePath(path, ctf.Challenges[i].VMConfig.ImagePath)
		}
	}
	return ctf, nil
}

func (s *FileStore) readLegacyChallenge(configPath string) (Challenge, error) {
	type legacyQuestion struct {
		ID     string `json:"id"`
		Order  int    `json:"order"`
		Text   string `json:"text"`
		Answer string `json:"answer"`
		Hint   string `json:"hint,omitempty"`
	}
	type legacyChallenge struct {
		ID          string           `json:"id"`
		Name        string           `json:"name"`
		Description string           `json:"description"`
		VMConfig    VMConfig         `json:"vm_config"`
		Questions   []legacyQuestion `json:"questions"`
	}

	var legacy legacyChallenge
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Challenge{}, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return Challenge{}, fmt.Errorf("failed to unmarshal legacy challenge config %s: %w", configPath, err)
	}

	challenge := Challenge{
		ID:          legacy.ID,
		CTFID:       filepath.Base(filepath.Dir(configPath)),
		Title:       legacy.Name,
		Description: legacy.Description,
		ImageID:     legacy.ID,
		Validator:   ChallengeValidatorStatic,
		VMConfig:    legacy.VMConfig,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if len(legacy.Questions) > 0 {
		challenge.Flag = legacy.Questions[0].Answer
		challenge.QuestionNo = legacy.Questions[0].Order
	}
	challenge.VMConfig.ImagePath = s.resolveImagePath(configPath, challenge.VMConfig.ImagePath)
	return challenge, nil
}

func (s *FileStore) resolveImagePath(configPath, imagePath string) string {
	if imagePath == "" {
		return ""
	}
	if filepath.IsAbs(imagePath) {
		return imagePath
	}
	if _, err := os.Stat(imagePath); err == nil {
		return imagePath
	}
	base := filepath.Dir(configPath)
	candidates := []string{
		filepath.Join(base, imagePath),
		filepath.Join(s.basePath, imagePath),
		filepath.Join(s.legacyPath, filepath.Base(base), imagePath),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return imagePath
}
