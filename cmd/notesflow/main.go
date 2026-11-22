package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gofrs/flock"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/providers"
	"github.com/AndreaUnali/notesflow/internal/tui"
	"github.com/AndreaUnali/notesflow/internal/watcher"
	"github.com/AndreaUnali/notesflow/internal/web"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

const usage = `Uso: notesflow [comando] [opzioni]

  tui        TUI e server del grafo incorporato (predefinito)
  serve      Server del grafo e watcher in primo piano
  index      Scansione locale e snapshot JSON
  todos      Elenca o riassume i TODO
  compile    Compila un documento LaTeX, opzionalmente apre il PDF
  diagram    Rende un sorgente Mermaid o Pikchr in SVG
  agent      Apre OpenCode nel workspace

Opzioni comuni: --workspace DIR, --config FILE
Per la guida a un comando: notesflow <comando> --help
`

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "notesflow:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Print(usage)
		return nil
	}
	command := "tui"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	workspace := flags.String("workspace", ".", "directory degli appunti")
	cfgPath := flags.String("config", "", "file JSON di configurazione")
	var port int
	var browse, summary, viewPDF bool
	var kind, section, course, model string
	switch command {
	case "tui", "serve":
		flags.IntVar(&port, "port", 0, "porta loopback (0 = scelta automatica)")
		flags.BoolVar(&browse, "open", false, "apri il grafo nel browser all’avvio")
	case "index", "diagram":
	case "todos":
		flags.BoolVar(&summary, "summary", false, "riepilogo e suggerimenti dal provider configurato")
		flags.StringVar(&kind, "kind", "", "tipo: TODO o VISUAL")
		flags.StringVar(&section, "section", "", "filtra per titolo o ID della sezione")
		flags.StringVar(&course, "course", "", "filtra per corso")
	case "compile":
		flags.BoolVar(&viewPDF, "view", false, "apri il PDF compilato")
	case "agent":
		flags.StringVar(&model, "model", "", "modello OpenCode (provider/model)")
	default:
		return fmt.Errorf("comando %q sconosciuto. Usa notesflow --help", command)
	}
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Uso: notesflow %s [opzioni]", command)
		if command == "compile" || command == "diagram" {
			fmt.Fprint(flags.Output(), " FILE")
		}
		fmt.Fprintln(flags.Output())
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	needsFile := command == "compile" || command == "diagram"
	if (needsFile && flags.NArg() != 1) || (!needsFile && flags.NArg() != 0) {
		flags.Usage()
		return errors.New("argomenti del comando non validi")
	}
	kind = strings.ToUpper(kind)
	if kind != "" && kind != "TODO" && kind != "VISUAL" {
		return errors.New("--kind deve essere TODO o VISUAL")
	}
	if port < 0 || port > 65535 {
		return errors.New("--port deve essere compresa tra 0 e 65535")
	}
	root, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("--workspace deve indicare una directory")
	}
	cfg, err := config.Load(root, *cfgPath)
	if err != nil {
		return err
	}
	switch command {
	case "compile":
		file, err := workflow.Compile(ctx, root, flags.Arg(0), cfg.Tools.LatexEngine)
		if err != nil {
			return err
		}
		fmt.Println(file)
		if viewPDF {
			return workflow.Open(ctx, file, cfg.Tools.PDFViewer)
		}
		return nil
	case "diagram":
		file, err := workflow.Diagram(ctx, root, flags.Arg(0))
		if err == nil {
			fmt.Println(file)
		}
		return err
	case "agent":
		var args []string
		if model == "" && cfg.Providers.Mode == "byok" {
			for _, profile := range cfg.Providers.Profiles {
				if profile.ID == cfg.Providers.Active && profile.Driver == "opencode" {
					model = profile.Model
				}
			}
		}
		if model != "" {
			args = []string{"--model", model}
		}
		cmd := exec.CommandContext(ctx, "opencode", args...)
		cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = root, os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	stateDir := filepath.Join(root, ".notesflow")
	if err = os.MkdirAll(stateDir, 0700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(stateDir, "process.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("un’altra istanza usa già questo workspace; usa la sua TUI o il suo grafo")
	}
	defer lock.Close()
	store, err := graph.Open(filepath.Join(stateDir, "index.json"))
	if err != nil {
		return err
	}
	watch := watcher.New(root, store)
	if err = watch.Scan(ctx); err != nil {
		if command == "index" || command == "todos" {
			return err
		}
		store.Log("Scansione iniziale: " + err.Error())
	}
	if command == "index" {
		snapshot, _ := store.Snapshot()
		return outputJSON(snapshot)
	}
	if command == "todos" && !summary {
		return outputJSON(filterTasks(store, kind, section, course))
	}
	manager, err := providers.Open(filepath.Join(stateDir, "providers.json"), cfg.Providers)
	if err != nil {
		return err
	}
	if command == "todos" {
		text, err := manager.Summarize(ctx, filterTasks(store, kind, section, course))
		if text != "" {
			fmt.Println(text)
		}
		return err
	}
	return live(ctx, command, root, port, browse, cfg, store, watch, manager)
}

func outputJSON(value any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func filterTasks(store *graph.Store, kind, section, course string) []parser.Task {
	snapshot, _ := store.Snapshot()
	tasks := []parser.Task{}
	section = strings.ToLower(section)
	for _, task := range snapshot.Tasks {
		if (kind == "" || kind == task.Kind) &&
			(course == "" || strings.EqualFold(course, parser.Course(task.File))) &&
			(section == "" || strings.Contains(strings.ToLower(task.Section+" "+task.NodeID), section)) {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func live(parent context.Context, command, root string, port int, browse bool, cfg config.Config, store *graph.Store, watch *watcher.Watcher, manager *providers.Manager) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	server, err := web.Listen(ctx, port, store, func(ctx context.Context, node parser.Node) error {
		return workflow.RemoteEdit(ctx, root, cfg.Tools.NvimServer, node.File, node.Line)
	})
	if err != nil {
		return err
	}
	failures := make(chan error, 1)
	go func() {
		if err := server.Serve(); err != nil {
			failures <- err
			cancel()
		}
	}()
	var workers sync.WaitGroup
	workers.Go(func() { watch.Run(ctx) })
	if cfg.Semantic.Enabled {
		workers.Go(func() { watcher.Analyze(ctx, store, manager, cfg.Semantic.CandidateLimit, cfg.Semantic.DeltaRunes) })
	}
	if browse {
		workers.Go(func() {
			if err := workflow.Open(ctx, server.URL, ""); err != nil {
				store.Log("Browser: " + err.Error())
				if command == "serve" {
					fmt.Fprintln(os.Stderr, "Browser:", err)
				}
			}
		})
	}
	if command == "serve" {
		fmt.Println(server.URL)
		<-ctx.Done()
	} else {
		_, err = tea.NewProgram(tui.New(ctx, root, server.URL, cfg, store, manager), tea.WithContext(ctx), tea.WithAltScreen()).Run()
		if ctx.Err() != nil && errors.Is(err, tea.ErrProgramKilled) {
			err = nil
		}
	}
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	err = errors.Join(err, server.Shutdown(shutdown))
	workers.Wait()
	select {
	case failure := <-failures:
		err = errors.Join(err, failure)
	default:
	}
	return err
}
