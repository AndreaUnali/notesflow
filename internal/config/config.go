// Package config loads workspace settings. Provider secrets are read from the environment.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Profile struct {
	ID           string `json:"id"`
	Driver       string `json:"driver"`
	Model        string `json:"model"`
	BaseURL      string `json:"base_url,omitempty"`
	APIKeyEnv    string `json:"api_key_env,omitempty"`
	LocalKey     string `json:"-"`
	RequestLimit int    `json:"request_limit,omitempty"`
	Window       string `json:"window,omitempty"`
}

func (p Profile) APIKey() string {
	if p.LocalKey != "" {
		return p.LocalKey
	}
	return os.Getenv(p.APIKeyEnv)
}

func (p Profile) Duration() time.Duration {
	duration, _ := time.ParseDuration(p.Window)
	return duration
}

type Providers struct {
	Mode     string    `json:"mode"`
	Active   string    `json:"active"`
	Profiles []Profile `json:"profiles"`
}

type Semantic struct {
	Enabled        bool `json:"enabled"`
	CandidateLimit int  `json:"candidate_limit"`
	DeltaRunes     int  `json:"delta_runes"`
}

type Tools struct {
	NvimServer  string `json:"nvim_server"`
	LatexEngine string `json:"latex_engine"`
	PDFViewer   string `json:"pdf_viewer"`
	MainTeX     string `json:"main_tex,omitempty"`
	PDF         string `json:"pdf,omitempty"`
	BuildDir    string `json:"build_dir,omitempty"`
}

type Agent struct {
	QuestionSeconds int `json:"question_seconds"`
}

type Config struct {
	Project   string    `json:"project"`
	Structure string    `json:"structure,omitempty"`
	Providers Providers `json:"providers"`
	Semantic  Semantic  `json:"semantic"`
	Tools     Tools     `json:"tools"`
	Agent     Agent     `json:"agent"`
}

func Load(root, filename string) (Config, error) {
	return LoadWithSettings(root, filename, Settings{})
}

func defaults(root string) Config {
	return Config{
		Project:   filepath.Base(root),
		Providers: Providers{Mode: "byok", Active: "locale", Profiles: []Profile{{ID: "locale", Driver: "mock", Model: "affinità lessicale"}}},
		Semantic:  Semantic{Enabled: true, CandidateLimit: 20, DeltaRunes: 4000},
		Agent:     Agent{QuestionSeconds: 90},
		Tools:     Tools{NvimServer: os.Getenv("NVIM"), LatexEngine: "latexmk", PDFViewer: "Skim"},
	}
}

func LoadWithSettings(root, filename string, settings Settings) (Config, error) {
	cfg := defaults(root)
	explicit := filename != ""
	if !explicit {
		filename = filepath.Join(root, "notesflow.json")
	}
	file, err := os.Open(filename)
	if errors.Is(err, os.ErrNotExist) && !explicit {
		settings.apply(&cfg)
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	defer file.Close()
	d := json.NewDecoder(file)
	d.DisallowUnknownFields()
	workspace := struct {
		*Config
		Providers json.RawMessage `json:"providers"`
	}{Config: &cfg}
	if err = d.Decode(&workspace); err != nil {
		return cfg, fmt.Errorf("%s: %w", filename, err)
	}
	if d.Decode(new(any)) != io.EOF {
		return cfg, fmt.Errorf("%s: atteso un solo oggetto JSON", filename)
	}
	if len(workspace.Providers) == 0 {
		settings.apply(&cfg)
	} else if err = decodeProviders(workspace.Providers, &cfg.Providers); err != nil {
		return cfg, fmt.Errorf("%s: %w", filename, err)
	}
	return cfg, cfg.validate()
}

func decodeProviders(raw json.RawMessage, providers *Providers) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	return d.Decode(providers)
}

func (cfg Config) validate() error {
	if cfg.Agent.QuestionSeconds < 1 || cfg.Agent.QuestionSeconds > 3600 {
		return errors.New("agent.question_seconds deve essere 1–3600")
	}
	if cfg.Semantic.CandidateLimit < 1 || cfg.Semantic.CandidateLimit > 100 || cfg.Semantic.DeltaRunes < 1 || cfg.Semantic.DeltaRunes > 16000 {
		return errors.New("semantic: candidate_limit deve essere 1–100 e delta_runes 1–16000")
	}
	if cfg.Providers.Mode != "pool" && cfg.Providers.Mode != "byok" {
		return errors.New("providers.mode deve essere pool o byok")
	}
	ids := map[string]bool{}
	for _, p := range cfg.Providers.Profiles {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Model) == "" || ids[p.ID] {
			return errors.New("profili provider: specificare id univoci e model")
		}
		ids[p.ID] = true
		if p.RequestLimit < 0 || (p.RequestLimit > 0 && p.Duration() <= 0) {
			return fmt.Errorf("%s: request_limit richiede una finestra positiva, ad esempio 1h", p.ID)
		}
		switch p.Driver {
		case "mock", "opencode":
		case "openai-compatible":
			endpoint, err := url.Parse(p.BaseURL)
			if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
				return fmt.Errorf("%s: base_url non valida", p.ID)
			}
			ip := net.ParseIP(endpoint.Hostname())
			local := endpoint.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
			if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && local) {
				return fmt.Errorf("%s: base_url deve usare HTTPS o HTTP su loopback", p.ID)
			}
		default:
			return fmt.Errorf("%s: driver sconosciuto %q", p.ID, p.Driver)
		}
	}
	if len(ids) == 0 || (cfg.Providers.Mode == "byok" && !ids[cfg.Providers.Active]) {
		return errors.New("configurare almeno un profilo e indicare active per la modalità byok")
	}
	switch cfg.Tools.LatexEngine {
	case "latexmk", "pdflatex", "xelatex", "lualatex", "tectonic":
		return nil
	default:
		return errors.New("tools.latex_engine: motore non supportato")
	}
}
