package config

import (
	"os"
	"path/filepath"

	"github.com/AndreaUnali/notesflow/internal/storage"
)

// Settings are private to the user. A workspace can override the default model.
type Settings struct {
	Theme   string   `json:"theme"`
	Profile *Profile `json:"profile,omitempty"`
	APIKey  string   `json:"api_key,omitempty"`
}

func settingsPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(dir) {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "notesflow", "settings.json"), nil
}

func LoadSettings() (settings Settings, exists bool, err error) {
	path, err := settingsPath()
	if err != nil {
		return settings, false, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return settings, false, nil
	} else if err != nil {
		return settings, false, err
	}
	err = storage.Load(path, &settings)
	return settings, true, err
}

func (s Settings) Save() error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return storage.Save(path, s)
}

func (s Settings) apply(cfg *Config) {
	if s.Profile == nil {
		return
	}
	profile := *s.Profile
	profile.LocalKey = s.APIKey
	cfg.Providers = Providers{Mode: "byok", Active: profile.ID, Profiles: []Profile{profile}}
}

func ValidateProfile(profile Profile) error {
	cfg := defaults(".")
	Settings{Profile: &profile}.apply(&cfg)
	return cfg.validate()
}
