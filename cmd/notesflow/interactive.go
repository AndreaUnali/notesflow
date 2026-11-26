package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/network"
	"github.com/AndreaUnali/notesflow/internal/palette"
	"github.com/AndreaUnali/notesflow/internal/providers"
	"github.com/AndreaUnali/notesflow/internal/terminal"
	"github.com/AndreaUnali/notesflow/internal/tui"
	"github.com/AndreaUnali/notesflow/internal/watcher"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

type workspaceStart struct {
	path, configFile string
	create           bool
}

func runInteractive(ctx context.Context, start workspaceStart, settings config.Settings, port int, browse bool) error {
	for ctx.Err() == nil {
		tui.UseAppearance(settings)
		path, notice, create := start.path, "", start.create
		if path == "" && !create {
			selected, action, err := tui.ChooseNetwork(ctx, terminal.New(os.Stdout, terminal.Enabled()))
			if err != nil {
				return err
			}
			switch action {
			case tui.LaunchSettings:
				configured, saved, err := tui.RunSetup(ctx, settings)
				if err != nil {
					return err
				}
				if saved {
					settings = configured
				}
				continue
			case tui.LaunchNew:
				create = true
			default:
				if selected == "" {
					return nil
				}
				path = selected
			}
		}
		if create {
			var err error
			path, notice, err = tui.RunNewProject(ctx, settings)
			if err != nil || path == "" && start.create {
				return err
			}
			if path == "" {
				continue
			}
		}
		back, err := runProject(ctx, path, start.configFile, settings, port, browse, notice)
		if err != nil || !back || ctx.Err() != nil {
			return err
		}
		start = workspaceStart{}
		settings, _, err = config.LoadSettings()
		if err != nil {
			return err
		}
	}
	return nil
}

func runProject(ctx context.Context, path, configFile string, settings config.Settings, port int, browse bool, notice string) (bool, error) {
	root, err := workspaceRoot(path)
	if err != nil {
		return false, err
	}
	var imported workflow.ImportedProject
	if notice == "" {
		imported, err = importWorkspace(ctx, root, "", configFile)
		if err != nil && !errors.Is(err, workflow.ErrNoLaTeX) {
			return false, err
		}
		if imported.Root != "" && imported.Root != root {
			root, configFile = imported.Root, ""
		}
	}
	cfg, err := config.LoadWithSettings(root, configFile, settings)
	if err != nil {
		return false, err
	}
	cfg.Project = network.DisplayName(root, cfg.Project)
	if imported.Created {
		notice = "Progetto importato · PDF pronto"
		if err := compileImported(ctx, root, cfg); err != nil {
			notice = "Progetto importato · " + importCompileNotice(err)
		}
	}
	project, err := openProjectState(root)
	if err != nil {
		return false, err
	}
	defer project.lock.Close()
	if err := project.watch.Scan(ctx); err != nil {
		project.store.Log("Scansione iniziale: " + err.Error())
	}
	manager, err := providers.Open(filepath.Join(project.dir, "providers.json"), cfg.Providers)
	if err != nil {
		return false, err
	}
	return live(ctx, "tui", root, port, browse, cfg, project.store, project.watch, manager, notice, palette.Preset(settings.Theme))
}

func workspaceRoot(directory string) (string, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("--workspace deve indicare una directory")
	}
	return root, nil
}

func importWorkspace(ctx context.Context, root, title, configFile string) (workflow.ImportedProject, error) {
	if title == "" {
		title = network.DisplayName(root, filepath.Base(root))
		if strings.ContainsAny(title, "/\\") {
			title = filepath.Base(root)
		}
	}
	return workflow.ImportProject(ctx, root, title, configFile)
}

type projectState struct {
	dir   string
	lock  *flock.Flock
	store *graph.Store
	watch *watcher.Watcher
}

func openProjectState(root string) (projectState, error) {
	dir := filepath.Join(root, ".notesflow")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return projectState{}, err
	}
	lock := flock.New(filepath.Join(dir, "process.lock"))
	locked, err := lock.TryLock()
	if err != nil || !locked {
		_ = lock.Close()
		if err == nil {
			err = errors.New("un’altra istanza usa già questo workspace; usa la sua TUI o il suo grafo")
		}
		return projectState{}, err
	}
	store, err := graph.Open(filepath.Join(dir, "index.json"))
	if err != nil {
		_ = lock.Close()
		return projectState{}, err
	}
	return projectState{dir: dir, lock: lock, store: store, watch: watcher.New(root, store)}, nil
}
