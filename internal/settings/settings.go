package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aslanbrooke/jirahere/internal/auth"
)

type Defaults struct {
	Project         string `json:"project,omitempty"`
	IssueType       string `json:"issue_type,omitempty"`
	CurrentQuarter  string `json:"current_quarter,omitempty"`
	PreviousQuarter string `json:"previous_quarter,omitempty"`
	NextQuarter     string `json:"next_quarter,omitempty"`
}

type Settings struct {
	Defaults Defaults `json:"defaults,omitempty"`
}

func settingsPath(prof string) (string, error) {
	dir, err := auth.ConfigDir(prof)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

func Load(prof string) (*Settings, error) {
	path, err := settingsPath(prof)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Settings{}, nil
		}
		return nil, err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func Save(s *Settings, prof string) error {
	dir, err := auth.ConfigDir(prof)
	if err != nil {
		return err
	}
	if prof != "" {
		parent := filepath.Dir(dir)
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return fmt.Errorf("could not create profiles directory %s: %w", parent, err)
		}
		if err := os.Chmod(parent, 0o700); err != nil {
			return fmt.Errorf("could not set profiles directory permissions %s: %w", parent, err)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("could not create config directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("could not set config directory permissions %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("could not encode settings: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "settings-*.json.tmp")
	if err != nil {
		return fmt.Errorf("could not create temp settings file: %w", err)
	}
	tmpPath := tmp.Name()

	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("could not write temp settings file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("could not set settings file permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("could not write temp settings file: %w", err)
	}

	path := filepath.Join(dir, "settings.json")
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("could not save settings file: %w", err)
	}
	return nil
}
