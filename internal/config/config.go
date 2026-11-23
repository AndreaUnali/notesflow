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
	RequestLimit int    `json:"request_limit,omitempty"`
	Window       string `json:"window,omitempty"`
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
}

type Config struct {
	Project   string    `json:"project"`
	Providers Providers `json:"providers"`
	Semantic  Semantic  `json:"semantic"`
	Tools     Tools     `json:"tools"`
}

func Load(root, filename string) (Config, error) {
	cfg := Config{
		Project:   filepath.Base(root),
		Providers: Providers{Mode: "byok", Active: "locale", Profiles: []Profile{{ID: "locale", Driver: "mock", Model: "affinità lessicale"}}},
		Semantic:  Semantic{Enabled: true, CandidateLimit: 20, DeltaRunes: 4000},
		Tools:     Tools{NvimServer: os.Getenv("NVIM"), LatexEngine: "latexmk", PDFViewer: "Skim"},
	}
	explicit := filename != ""
	if !explicit {
		filename = filepath.Join(root, "notesflow.json")
	}
	file, err := os.Open(filename)
	if errors.Is(err, os.ErrNotExist) && !explicit {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	defer file.Close()
	d := json.NewDecoder(file)
	d.DisallowUnknownFields()
	if err = d.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", filename, err)
	}
	if d.Decode(new(any)) != io.EOF {
		return cfg, fmt.Errorf("%s: atteso un solo oggetto JSON", filename)
	}
	return cfg, cfg.validate()
}

func (cfg Config) validate() error {
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
