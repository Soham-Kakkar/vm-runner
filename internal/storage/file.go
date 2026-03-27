package storage

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

// --- Data Models ---

type Challenge struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	VMConfig    VMConfig   `json:"vm_config"`
	Questions   []Question `json:"questions"`
}

type VMConfig struct {
	ImagePath      string `json:"image_path"`
	ImageFormat    string `json:"image_format"`
	MemoryMB       int    `json:"memory_mb"`
	CPUs           int    `json:"cpus"`
	Architecture   string `json:"architecture"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	DisplayType    string `json:"display_type"` // "terminal" or "vnc"
}

type Question struct {
	ID     string `json:"id"`
	Order  int    `json:"order"`
	Text   string `json:"text"`
	Answer string `json:"answer"`
	Hint   string `json:"hint,omitempty"`
}

// --- File-based Storage ---

type FileStore struct {
	challengesPath string
}

func NewFileStore(challengesPath string) *FileStore {
	return &FileStore{challengesPath: challengesPath}
}

// ListChallenges retrieves all challenges from the filesystem.
func (s *FileStore) ListChallenges() ([]Challenge, error) {
	challenges := []Challenge{}
	err := filepath.Walk(s.challengesPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == "config.json" {
			challenge, err := s.readChallengeConfig(path)
			if err != nil {
				// Log the error but continue with other challenges
				fmt.Printf("Warning: could not load challenge from %s: %v\n", path, err)
				return nil
			}
			challenges = append(challenges, challenge)
		}
		return nil
	})
	return challenges, err
}

// GetChallenge retrieves a single challenge by its ID.
func (s *FileStore) GetChallenge(id string) (Challenge, error) {
	configPath := filepath.Join(s.challengesPath, id, "config.json")
	return s.readChallengeConfig(configPath)
}

func (s *FileStore) readChallengeConfig(configPath string) (Challenge, error) {
	var challenge Challenge
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return challenge, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}
	if err := json.Unmarshal(data, &challenge); err != nil {
		return challenge, fmt.Errorf("failed to unmarshal challenge config %s: %w", configPath, err)
	}
	// Make image path relative to the project root
	challenge.VMConfig.ImagePath = filepath.Join(filepath.Dir(configPath), challenge.VMConfig.ImagePath)
	return challenge, nil
}
