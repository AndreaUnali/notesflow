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
	"github.com/charmbracelet/x/term"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/network"
	"github.com/AndreaUnali/notesflow/internal/palette"
	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/providers"
	"github.com/AndreaUnali/notesflow/internal/terminal"
	"github.com/AndreaUnali/notesflow/internal/tui"
	"github.com/AndreaUnali/notesflow/internal/watcher"
	"github.com/AndreaUnali/notesflow/internal/web"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

const usage = `Uso: notesflow [comando] [opzioni]

  tui        Workspace interattivo nel terminale (predefinito)
  setup      Configura aspetto, OpenCode e provider API
  new        Crea un progetto a file unico o a capitoli
  import     Copia e compila un progetto LaTeX in Documenti/Notesflow
  serve      Server del grafo e watcher in primo piano
  index      Scansione locale e snapshot JSON
  todos      Elenca o riassume i TODO
  compile    Compila un documento LaTeX, opzionalmente apre il PDF
  diagram    Rende un sorgente Mermaid o Pikchr in SVG
  agent      Apre OpenCode nel workspace

Opzioni globali: --help, --version

Opzioni comuni: --workspace DIR, --config FILE
Per la guida a un comando: notesflow <comando> --help
`

// Set by the release build.
var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "notesflow:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("notesflow", version)
		return nil
	}
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
	var importTitle string
	switch command {
	case "tui", "new", "serve":
		flags.IntVar(&port, "port", 0, "porta loopback (0 = scelta automatica)")
		flags.BoolVar(&browse, "open", false, "apri il grafo nel browser all’avvio")
	case "index", "diagram", "setup":
	case "import":
		flags.StringVar(&importTitle, "title", "", "nome della copia importata")
	case "todos":
		flags.BoolVar(&summary, "summary", false, "riepilogo e suggerimenti dal provider configurato")
		flags.StringVar(&kind, "kind", "", "tipo: TODO, TODO-AI, VISUAL o MANUTENZIONE")
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
	if kind != "" && kind != "TODO" && kind != "VISUAL" && kind != parser.MaintenanceTask && kind != parser.AITask {
		return errors.New("--kind deve essere TODO, TODO-AI, VISUAL o MANUTENZIONE")
	}
	if port < 0 || port > 65535 {
		return errors.New("--port deve essere compresa tra 0 e 65535")
	}
	explicitWorkspace := false
	flags.Visit(func(f *flag.Flag) { explicitWorkspace = explicitWorkspace || f.Name == "workspace" })
	var settings config.Settings
	var exists bool
	var err error
	switch command {
	case "setup", "tui", "new", "serve", "todos", "agent":
		settings, exists, err = config.LoadSettings()
		if err != nil {
			return err
		}
	}
	interactive := term.IsTerminal(os.Stdin.Fd())
	if command == "setup" || ((command == "tui" || command == "new") && interactive && !exists) {
		if !interactive {
			return errors.New("apri un terminale interattivo per configurare Notesflow")
		}
		configured, saved, err := tui.RunSetup(ctx, settings)
		if err != nil || command == "setup" {
			return err
		}
		if saved {
			settings = configured
		}
	}
	if command == "new" || command == "tui" {
		start := workspaceStart{configFile: *cfgPath, create: command == "new"}
		if explicitWorkspace {
			start.path = *workspace
		}
		return runInteractive(ctx, start, settings, port, browse)
	}
	root, err := workspaceRoot(*workspace)
	if err != nil {
		return err
	}
	if command == "import" {
		imported, err := importWorkspace(ctx, root, importTitle, *cfgPath)
		if err != nil {
			return err
		}
		if imported.Root != root {
			root, *cfgPath = imported.Root, ""
		}
	}
	cfg, err := config.LoadWithSettings(root, *cfgPath, settings)
	if err != nil {
		return err
	}
	switch command {
	case "import":
		fmt.Println(root)
		return compileImported(ctx, root, cfg)
	case "compile":
		file, err := workflow.CompileIn(ctx, root, flags.Arg(0), cfg.Tools.LatexEngine, cfg.Tools.BuildDir)
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
	project, err := openProjectState(root)
	if err != nil {
		return err
	}
	defer project.lock.Close()
	if err = project.watch.Scan(ctx); err != nil {
		if command == "index" || command == "todos" {
			return err
		}
		project.store.Log("Scansione iniziale: " + err.Error())
	}
	if command == "index" {
		snapshot, _ := project.store.Snapshot()
		return outputJSON(snapshot)
	}
	if command == "todos" && !summary {
		return outputJSON(filterTasks(project.store, kind, section, course))
	}
	manager, err := providers.Open(filepath.Join(project.dir, "providers.json"), cfg.Providers)
	if err != nil {
		return err
	}
	if command == "todos" {
		text, err := manager.Summarize(ctx, filterTasks(project.store, kind, section, course))
		if text != "" {
			fmt.Println(text)
		}
		return err
	}
	_, err = live(ctx, command, root, port, browse, cfg, project.store, project.watch, manager, "", palette.Preset(settings.Theme))
	return err
}

func compileImported(ctx context.Context, root string, cfg config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	_, err := workflow.CompileIn(ctx, root, cfg.Tools.MainTeX, cfg.Tools.LatexEngine, cfg.Tools.BuildDir)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func importCompileNotice(err error) string {
	for _, line := range strings.Split(err.Error(), "\n") {
		if reason, ok := strings.CutPrefix(line, "! "); ok {
			return reason
		}
	}
	return "Prima compilazione: " + strings.SplitN(err.Error(), "\n", 2)[0]
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

func live(parent context.Context, command, root string, port int, browse bool, cfg config.Config, store *graph.Store, watch *watcher.Watcher, manager *providers.Manager, initialNotice string, colors palette.Colors) (bool, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var server *web.Server
	var url string
	var err error
	var returnToLauncher bool
	var editorDone <-chan struct{}
	failures := make(chan error, 1)
	if command == "serve" || browse || port != 0 {
		server, err = web.Listen(ctx, port, store, colors, func(ctx context.Context, node parser.Node) error {
			return workflow.RemoteEdit(ctx, root, cfg.Tools.NvimServer, node.File, node.Line)
		})
		if err != nil {
			return false, err
		}
		url = server.URL
		go func() {
			if err := server.Serve(); err != nil {
				failures <- err
				cancel()
			}
		}()
	}
	var workers sync.WaitGroup
	workers.Go(func() { watch.Run(ctx) })
	startAnalysis := func(report func(watcher.AnalysisActivity)) {
		if cfg.Semantic.Enabled {
			workers.Go(func() {
				watcher.Analyze(ctx, store, manager, cfg.Semantic.CandidateLimit, cfg.Semantic.DeltaRunes, report)
			})
		}
	}
	if browse {
		workers.Go(func() {
			if err := workflow.Open(ctx, url, ""); err != nil {
				store.Log("Browser: " + err.Error())
				if command == "serve" {
					fmt.Fprintln(os.Stderr, "Browser:", err)
				}
			}
		})
	}
	if command == "serve" {
		startAnalysis(nil)
		fmt.Println(url)
		<-ctx.Done()
	} else {
		surface := terminal.New(os.Stdout, terminal.Enabled())
		snapshot, _ := store.Snapshot()
		if err := network.Remember(root, cfg.Project, snapshot); err != nil {
			store.Log("Note Network recenti: " + err.Error())
		}
		model := tui.New(ctx, root, url, cfg, store, manager).WithSurface(surface).WithSession().WithLaunchNotice(initialNotice)
		program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen(), tea.WithOutput(surface), tea.WithMouseAllMotion(), tea.WithFPS(60))
		startAnalysis(func(progress watcher.AnalysisActivity) { program.Send(progress) })
		var result tea.Model
		result, err = program.Run()
		if closed, ok := result.(tui.Model); ok {
			returnToLauncher, editorDone = closed.ReturnToLauncher(), closed.EditorDone()
		}
		snapshot, _ = store.Snapshot()
		if rememberErr := network.Remember(root, cfg.Project, snapshot); rememberErr != nil {
			fmt.Fprintln(os.Stderr, "Note Network recenti:", rememberErr)
		}
		if ctx.Err() != nil && errors.Is(err, tea.ErrProgramKilled) {
			err = nil
		}
	}
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if server != nil {
		err = errors.Join(err, server.Shutdown(shutdown))
	}
	if editorDone != nil {
		select {
		case <-editorDone:
		case <-shutdown.Done():
		}
	}
	workers.Wait()
	select {
	case failure := <-failures:
		err = errors.Join(err, failure)
	default:
	}
	return returnToLauncher && parent.Err() == nil, err
}
