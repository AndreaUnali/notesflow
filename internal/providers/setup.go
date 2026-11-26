package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/AndreaUnali/notesflow/internal/config"
)

// Models lists the catalog presented by the selected connection. A catalog
// entry is only confirmed usable after TestModel succeeds.
func Models(ctx context.Context, profile config.Profile) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	if profile.Driver == "opencode" {
		return openCodeModels(ctx)
	}
	profile.Model = "catalogo"
	if err := config.ValidateProfile(profile); err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(profile.BaseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("L’indirizzo del provider non è valido.")
	}
	if key := profile.APIKey(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := httpClient.Do(req)
	if err != nil {
		return nil, errors.New("Il provider non risponde. Controlla l’indirizzo e la connessione.")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if cause, help := statusDiagnosis(response.StatusCode, false); cause != "" {
			return nil, fmt.Errorf("%s %s", cause, help)
		}
		return nil, fmt.Errorf("Il catalogo non è disponibile (HTTP %d). Puoi indicare il modello manualmente.", response.StatusCode)
	}
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&catalog); err != nil {
		return nil, errors.New("Il provider ha restituito un catalogo in un formato sconosciuto.")
	}
	var models []string
	for _, item := range catalog.Data {
		if model := strings.TrimSpace(item.ID); model != "" {
			models = append(models, model)
		}
	}
	return sortedModels(models)
}

func openCodeModels(ctx context.Context) ([]string, error) {
	path, err := exec.LookPath("opencode")
	if err != nil {
		return nil, errors.New("OpenCode non è disponibile nel PATH.")
	}
	dir, err := os.MkdirTemp("", "notesflow-opencode-models-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	cmd := openCodeCommand(ctx, path, dir, []string{"models"})
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	output, err := cmd.Output()
	if err != nil {
		return nil, errors.New("OpenCode non ha restituito il catalogo. Verifica l’accesso in OpenCode e riprova.")
	}
	var models []string
	for _, line := range strings.Split(string(output), "\n") {
		for _, field := range strings.Fields(line) {
			field = strings.Trim(field, "|")
			provider, model, valid := strings.Cut(field, "/")
			if valid && provider != "" && model != "" && !strings.HasPrefix(provider, ".") && !strings.ContainsAny(field, ":\\[]") {
				models = append(models, field)
				break
			}
		}
	}
	return sortedModels(models)
}

// OpenCode 2 can start a private server for a command. Check the command's
// help so the same integration also works with releases predating that flag.
func openCodeCommand(ctx context.Context, executable, dir string, subcommand []string, args ...string) *exec.Cmd {
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	help := exec.CommandContext(probe, executable, append(slices.Clone(subcommand), "--help")...)
	help.Dir = dir
	options := slices.Clone(subcommand)
	if output, err := help.Output(); err == nil && strings.Contains(string(output), "--standalone") {
		options = append(options, "--standalone")
	}
	cmd := exec.CommandContext(ctx, executable, append(options, args...)...)
	cmd.Dir = dir
	return cmd
}

func OpenCodeLogin(ctx context.Context) (*exec.Cmd, error) {
	path, err := exec.LookPath("opencode")
	if err != nil {
		return nil, err
	}
	return openCodeCommand(ctx, path, "", []string{"auth", "login"}), nil
}

func sortedModels(models []string) ([]string, error) {
	slices.Sort(models)
	models = slices.Compact(models)
	if len(models) == 0 {
		return nil, errors.New("Nessun modello disponibile nel catalogo. Puoi indicarne uno manualmente.")
	}
	return models, nil
}

func TestModel(ctx context.Context, profile config.Profile) error {
	if err := config.ValidateProfile(profile); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	_, err := complete(ctx, profile, "Rispondi solo con OK.", "Test di connessione di Notesflow.")
	if err == nil {
		return nil
	}
	cause, help := Explain(err)
	return fmt.Errorf("%s %s", cause, help)
}
