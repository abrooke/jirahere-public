package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aslanbrooke/jirahere/internal/layout"
	"github.com/aslanbrooke/jirahere/internal/profile"
	"github.com/aslanbrooke/jirahere/internal/safedelete"
)

const (
	ProviderOAuth    = "oauth"
	ProviderAPIToken = "api-token"
)

type OAuthConfig struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type APITokenConfig struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

type Config struct {
	Provider string          `json:"provider"`
	Site     string          `json:"site"`
	CloudID  string          `json:"cloud_id,omitempty"`
	OAuth    *OAuthConfig    `json:"oauth,omitempty"`
	APIToken *APITokenConfig `json:"api_token,omitempty"`
}

func ConfigDir(prof string) (string, error) {
	if prof != "" {
		if err := profile.Validate(prof); err != nil {
			return "", fmt.Errorf("invalid profile: %w", err)
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not determine config directory: %w", err)
	}
	if prof == "" {
		return filepath.Join(base, layout.Namespace), nil
	}
	return filepath.Join(base, layout.ProfilesDirName, prof), nil
}

func ConfigPath(prof string) (string, error) {
	dir, err := ConfigDir(prof)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func Load(prof string) (*Config, error) {
	path, err := ConfigPath(prof)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("could not parse %s: %w", path, err)
	}
	return &cfg, nil
}

func Save(prof string, cfg *Config) error {
	if err := ValidateSiteHostname(cfg.Site); err != nil {
		return err
	}

	dir, err := ConfigDir(prof)
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

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("could not encode config: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("could not create temp config file: %w", err)
	}
	tmpPath := tmp.Name()

	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("could not write temp config file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("could not set config file permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("could not write temp config file: %w", err)
	}

	path := filepath.Join(dir, "config.json")
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("could not save config file: %w", err)
	}
	return nil
}

func Delete(prof string) (existed bool, err error) {
	dir, err := ConfigDir(prof)
	if err != nil {
		return false, err
	}
	path := filepath.Join(dir, "config.json")

	if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
		return false, nil
	}
	if err := safedelete.Remove(dir, path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("could not remove config file: %w", err)
	}
	return true, nil
}

func ProviderLabel(provider string) string {
	switch provider {
	case ProviderOAuth:
		return "OAuth"
	case ProviderAPIToken:
		return "API token"
	default:
		return provider
	}
}
