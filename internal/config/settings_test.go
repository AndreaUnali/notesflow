package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserSettingsDoNotOverrideWorkspaceProvider(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	settings := Settings{
		Theme: "contrasto", APIKey: "private-test-key",
		Profile: &Profile{ID: "api", Driver: "openai-compatible", BaseURL: "https://example.com/v1", Model: "test"},
	}
	if err := settings.Save(); err != nil {
		t.Fatal(err)
	}
	got, exists, err := LoadSettings()
	if err != nil || !exists || got.Theme != settings.Theme || got.APIKey != settings.APIKey {
		t.Fatal("the private user settings did not round trip")
	}
	root := t.TempDir()
	file := filepath.Join(root, "notesflow.json")
	if err := os.WriteFile(file, []byte(`{"tools":{"main_tex":"main.tex"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithSettings(root, "", got)
	if err != nil || cfg.Providers.Active != "api" || cfg.Providers.Profiles[0].APIKey() != settings.APIKey || cfg.Tools.MainTeX != "main.tex" {
		t.Fatal("the workspace did not inherit the configured API connection")
	}
	if err := os.WriteFile(file, []byte(`{"providers":{"active":"locale"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadWithSettings(root, "", got)
	if err != nil || cfg.Providers.Active != "locale" || cfg.Providers.Profiles[0].APIKey() != "" {
		t.Fatal("the local profile was overridden by the user settings")
	}
}
