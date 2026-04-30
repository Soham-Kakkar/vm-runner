package storage

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
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

	DefaultMakerImageFormat    = "qcow2"
	DefaultMakerMemoryMB       = 1024
	DefaultMakerCPUs           = 1
	DefaultMakerArchitecture   = "x86_64"
	DefaultMakerTimeoutSeconds = 1800

	NanoIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_-"
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
	Username   string    `json:"username"`
	Password   string    `json:"password"` // In a real app, this should be hashed
	Role       string    `json:"role"`     // "admin" or "user"
	Name       string    `json:"name"`
	ExternalID string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
}

type TelemetryEvent struct {
	ID          string    `json:"id"`
	CTFID       string    `json:"ctf_id"`
	SessionID   string    `json:"session_id"`
	ChallengeID string    `json:"challenge_id"`
	User        string    `json:"user"`
	LastCommand string    `json:"last_command"`
	IsCorrect   bool      `json:"is_correct"`
	CreatedAt   time.Time `json:"created_at"`
}

type ScoreEntry struct {
	User        string    `json:"user"`
	Score       int       `json:"score"`
	LastUpdated time.Time `json:"last_updated"`
}

type Scoreboard struct {
	CTFID     string       `json:"ctf_id"`
	UpdatedAt time.Time    `json:"updated_at"`
	Scores    []ScoreEntry `json:"scores"`
}

type FileStore struct {
	basePath      string
	ctfPath       string
	userPath      string
	legacyPath    string
	uploadPath    string
	telemetryPath string
}

func NewFileStore(basePath string) *FileStore {
	return &FileStore{
		basePath:      basePath,
		ctfPath:       filepath.Join(basePath, "ctfs"),
		userPath:      filepath.Join(basePath, "users.json"),
		legacyPath:    filepath.Join(basePath, "challenges"),
		uploadPath:    filepath.Join(basePath, "uploads", "qcow2"),
		telemetryPath: filepath.Join(basePath, "telemetry"),
	}
}

func Slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "ctf"
	}
	return slug
}

func NanoID(length int) (string, error) {
	if length <= 0 {
		return "", nil
	}
	var b strings.Builder
	b.Grow(length)
	max := big.NewInt(int64(len(NanoIDAlphabet)))
	for b.Len() < length {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(NanoIDAlphabet[n.Int64()])
	}
	return b.String(), nil
}

func (s *FileStore) GenerateUniqueCTFID(title string) (string, error) {
	slug := Slugify(title)
	for i := 0; i < 10; i++ {
		suffix, err := NanoID(6)
		if err != nil {
			return "", err
		}
		id := slug + "-" + suffix
		if _, err := os.Stat(filepath.Join(s.ctfPath, id+".json")); errors.Is(err, fs.ErrNotExist) {
			return id, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique ctf id")
}

func MakeChallengeID(questionNo int, title string) string {
	prefix := "q"
	if questionNo > 0 {
		prefix = fmt.Sprintf("q%d", questionNo)
	}
	slug := Slugify(title)
	if slug == "" || slug == "ctf" {
		return prefix
	}
	return prefix + "-" + slug
}

func (s *FileStore) SaveUploadedQCOW2(src io.Reader, originalName string) (string, error) {
	if !strings.EqualFold(filepath.Ext(originalName), ".qcow2") {
		return "", fmt.Errorf("uploaded image must be a .qcow2 file")
	}
	var header [4]byte
	if _, err := io.ReadFull(src, header[:]); err != nil {
		return "", fmt.Errorf("uploaded image is too small")
	}
	if header != [4]byte{'Q', 'F', 'I', 0xfb} {
		return "", fmt.Errorf("uploaded image is not a qcow2 image")
	}
	if err := os.MkdirAll(s.uploadPath, 0o755); err != nil {
		return "", err
	}
	base := Slugify(strings.TrimSuffix(filepath.Base(originalName), filepath.Ext(originalName)))
	suffix, err := NanoID(6)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.uploadPath, base+"-"+suffix+".qcow2")
	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := dst.Write(header[:]); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func NormalizeMakerVMConfig(cfg VMConfig) (VMConfig, error) {
	imagePath := strings.TrimSpace(cfg.ImagePath)
	if imagePath == "" {
		return VMConfig{}, fmt.Errorf("qcow2 image path is required")
	}
	if !strings.EqualFold(filepath.Ext(imagePath), ".qcow2") {
		return VMConfig{}, fmt.Errorf("base image must be a .qcow2 file")
	}

	displayType := strings.ToLower(strings.TrimSpace(cfg.DisplayType))
	if displayType != "vnc" {
		displayType = "terminal"
	}

	return VMConfig{
		ImagePath:      imagePath,
		ImageFormat:    DefaultMakerImageFormat,
		MemoryMB:       DefaultMakerMemoryMB,
		CPUs:           DefaultMakerCPUs,
		Architecture:   DefaultMakerArchitecture,
		TimeoutSeconds: DefaultMakerTimeoutSeconds,
		DisplayType:    displayType,
	}, nil
}

func NormalizeChallengeForMaker(challenge Challenge, ctfVMConfig VMConfig) Challenge {
	challenge.ID = strings.TrimSpace(challenge.ID)
	challenge.Title = strings.TrimSpace(challenge.Title)
	challenge.Description = strings.TrimSpace(challenge.Description)
	challenge.ImageID = strings.TrimSpace(challenge.ImageID)
	challenge.Validator = strings.ToLower(strings.TrimSpace(challenge.Validator))
	if challenge.Validator == "" {
		challenge.Validator = ChallengeValidatorStatic
	}
	if challenge.Validator != ChallengeValidatorHMAC {
		challenge.Validator = ChallengeValidatorStatic
	}
	challenge.Flag = strings.TrimSpace(challenge.Flag)
	challenge.Template = strings.TrimSpace(challenge.Template)
	if challenge.Validator == ChallengeValidatorHMAC && challenge.Template == "" {
		challenge.Template = "flag{<hmac>}"
	}
	if challenge.QuestionNo <= 0 {
		challenge.QuestionNo = 1
	}
	if challenge.ID == "" {
		challenge.ID = MakeChallengeID(challenge.QuestionNo, challenge.Title)
	}
	challenge.VMConfig = ctfVMConfig
	return challenge
}

func MarshalIndentNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
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
		if u.Username == user.Username || u.ExternalID != "" && u.ExternalID == user.ExternalID {
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
	data, err := MarshalIndentNoEscape(users)
	if err != nil {
		return err
	}
	return os.WriteFile(s.userPath, data, 0o644)
}

func (s *FileStore) telemetryDir(ctfID string) string {
	return filepath.Join(s.telemetryPath, ctfID)
}

func (s *FileStore) telemetryEventsPath(ctfID string) string {
	return filepath.Join(s.telemetryDir(ctfID), "events.json")
}

func (s *FileStore) telemetryScoresPath(ctfID string) string {
	return filepath.Join(s.telemetryDir(ctfID), "scores.json")
}

func (s *FileStore) AppendTelemetry(ctfID string, event TelemetryEvent) error {
	if ctfID == "" {
		return fmt.Errorf("ctf id is required")
	}
	if err := os.MkdirAll(s.telemetryDir(ctfID), 0o755); err != nil {
		return err
	}
	events, err := s.ListTelemetry(ctfID)
	if err != nil {
		return err
	}
	if event.ID == "" {
		event.ID, err = NanoID(10)
		if err != nil {
			return err
		}
	}
	event.CTFID = ctfID
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	events = append(events, event)
	data, err := MarshalIndentNoEscape(events)
	if err != nil {
		return err
	}
	return os.WriteFile(s.telemetryEventsPath(ctfID), data, 0o644)
}

func (s *FileStore) ListTelemetry(ctfID string) ([]TelemetryEvent, error) {
	path := s.telemetryEventsPath(ctfID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []TelemetryEvent{}, nil
		}
		return nil, err
	}
	var events []TelemetryEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *FileStore) GetScores(ctfID string) (Scoreboard, error) {
	path := s.telemetryScoresPath(ctfID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Scoreboard{CTFID: ctfID, Scores: []ScoreEntry{}}, nil
		}
		return Scoreboard{}, err
	}
	var board Scoreboard
	if err := json.Unmarshal(data, &board); err != nil {
		return Scoreboard{}, err
	}
	if board.CTFID == "" {
		board.CTFID = ctfID
	}
	return board, nil
}

func (s *FileStore) IncrementScore(ctfID, username string) (Scoreboard, error) {
	if ctfID == "" || username == "" {
		return Scoreboard{}, fmt.Errorf("ctf id and username are required")
	}
	if err := os.MkdirAll(s.telemetryDir(ctfID), 0o755); err != nil {
		return Scoreboard{}, err
	}
	board, err := s.GetScores(ctfID)
	if err != nil {
		return Scoreboard{}, err
	}
	now := time.Now().UTC()
	updated := false
	for i := range board.Scores {
		if board.Scores[i].User == username {
			board.Scores[i].Score++
			board.Scores[i].LastUpdated = now
			updated = true
			break
		}
	}
	if !updated {
		board.Scores = append(board.Scores, ScoreEntry{User: username, Score: 1, LastUpdated: now})
	}
	board.CTFID = ctfID
	board.UpdatedAt = now
	data, err := MarshalIndentNoEscape(board)
	if err != nil {
		return Scoreboard{}, err
	}
	if err := os.WriteFile(s.telemetryScoresPath(ctfID), data, 0o644); err != nil {
		return Scoreboard{}, err
	}
	return board, nil
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
	data, err := MarshalIndentNoEscape(ctf)
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
