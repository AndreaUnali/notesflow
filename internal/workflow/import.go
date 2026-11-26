package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/storage"
)

var ErrNoLaTeX = errors.New("la cartella non contiene sorgenti LaTeX")

type ImportedProject struct {
	Root    string
	Created bool
}

type importOrigin struct {
	Source   string `json:"source"`
	Document string `json:"document,omitempty"`
}

// ImportProject copies an external project once. Subsequent opens of the
// original return the same managed project, including edits made after import.
func ImportProject(ctx context.Context, source, title, configFile string) (ImportedProject, error) {
	parent, err := projectsDirectory()
	if err != nil {
		return ImportedProject{}, err
	}
	return importProjectIn(ctx, source, parent, title, configFile)
}

func importProjectIn(ctx context.Context, source, parent, title, configFile string) (ImportedProject, error) {
	source, err := filepath.EvalSymlinks(source)
	if err != nil {
		return ImportedProject{}, err
	}
	if real, err := filepath.EvalSymlinks(parent); err == nil {
		parent = real
		if relative, err := filepath.Rel(real, source); err == nil && filepath.IsLocal(relative) {
			if relative == "." {
				return ImportedProject{}, ErrNoLaTeX
			}
			return ImportedProject{Root: source}, nil
		}
	}
	title, err = projectTitle(title)
	if err != nil {
		return ImportedProject{}, err
	}
	if root, err := findImported(parent, source, ""); err != nil || root != "" {
		return ImportedProject{Root: root}, err
	}
	cfg, err := config.Load(source, configFile)
	if err != nil {
		return ImportedProject{}, err
	}
	files, tex, err := readImportFiles(ctx, source, parent)
	if err != nil {
		return ImportedProject{}, err
	}
	main, err := findImportMain(tex, cfg.Tools.MainTeX)
	if err != nil {
		return ImportedProject{}, err
	}
	document := filepath.Join(source, filepath.FromSlash(main))
	plan := planImport(files, tex, main)

	if err := os.MkdirAll(parent, 0755); err != nil {
		return ImportedProject{}, err
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return ImportedProject{}, err
	}
	lock := flock.New(filepath.Join(parent, projectsLockFile))
	defer lock.Close()
	if locked, err := lock.TryLockContext(ctx, 50*time.Millisecond); !locked || err != nil {
		return ImportedProject{}, errors.Join(errors.New("impossibile avviare l’importazione"), err)
	}
	if root, err := findImported(parent, source, document); err != nil || root != "" {
		return ImportedProject{Root: root}, err
	}
	display, err := availableProjectTitle(parent, title)
	if err != nil {
		return ImportedProject{}, err
	}
	stage, err := os.MkdirTemp(parent, ".import-*")
	if err != nil {
		return ImportedProject{}, err
	}
	defer os.RemoveAll(stage)
	if err := plan.write(ctx, source, stage, display, configFile); err != nil {
		return ImportedProject{}, err
	}
	if err := keepImportedPDF(source, stage, main, cfg.Tools.PDF); err != nil {
		return ImportedProject{}, err
	}
	root := filepath.Join(parent, display)
	if err := os.Rename(stage, root); err != nil {
		return ImportedProject{}, err
	}
	return ImportedProject{Root: root, Created: true}, nil
}

func keepImportedPDF(source, stage, main, configured string) error {
	pdf := strings.TrimSuffix(main, path.Ext(main)) + ".pdf"
	for _, name := range []string{configured, pdf, path.Join(path.Dir(main), "build", path.Base(pdf)), path.Join(path.Dir(main), "out", path.Base(pdf))} {
		if name == "" {
			continue
		}
		if original, err := WorkspaceFile(source, name); err == nil {
			if err := publishPDF(original, filepath.Join(stage, "main.pdf")); err == nil {
				return nil
			} else if !errors.Is(err, errInvalidPDF) {
				return err
			}
		}
	}
	return nil
}

func findImported(parent, source, document string) (string, error) {
	entries, err := os.ReadDir(parent)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		root := filepath.Join(parent, entry.Name())
		var origin importOrigin
		if err := storage.Load(filepath.Join(root, ".notesflow", "import.json"), &origin); err != nil {
			continue
		}
		if origin.Source == source || document != "" && (origin.Document == document ||
			origin.Document == "" && origin.Source == filepath.Dir(document)) {
			return root, nil
		}
	}
	return "", nil
}

func availableProjectTitle(parent, title string) (string, error) {
	display := title
	for n := 2; ; n++ {
		_, err := os.Lstat(filepath.Join(parent, display))
		if errors.Is(err, os.ErrNotExist) {
			return display, nil
		}
		if err != nil {
			return "", err
		}
		display = fmt.Sprintf("%s (%d)", title, n)
	}
}

func readImportFiles(ctx context.Context, root, managed string) ([]string, map[string]string, error) {
	var files []string
	tex := make(map[string]string)
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if name != root && (name == managed || slices.Contains([]string{".git", ".notesflow", "build", "out", "ausiliari"}, entry.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		extension := strings.ToLower(filepath.Ext(name))
		if !entry.Type().IsRegular() || slices.Contains([]string{".DS_Store", ".git", ".notesflow", ".latexmkrc", "latexmkrc"}, entry.Name()) ||
			strings.HasSuffix(name, ".synctex.gz") ||
			slices.Contains([]string{".aux", ".fdb_latexmk", ".fls", ".log", ".out", ".toc", ".lof", ".lot", ".blg", ".bcf"}, extension) {
			return nil
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		files = append(files, relative)
		if extension == ".tex" {
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			tex[relative] = string(data)
		}
		return nil
	})
	return files, tex, err
}

var importDocument = regexp.MustCompile(`(?m)^[\t ]*\\documentclass\b`)
var importBegin = regexp.MustCompile(`\\begin\s*\{document\}`)
var texComments = regexp.MustCompile(`(?m)(^|[^\\])%[^\r\n]*`)

func activeTeX(text string) string { return texComments.ReplaceAllString(text, "$1") }

func findImportMain(tex map[string]string, configured string) (string, error) {
	if len(tex) == 0 {
		return "", ErrNoLaTeX
	}
	if configured = filepath.ToSlash(filepath.Clean(configured)); tex[configured] != "" {
		return configured, nil
	}
	names := make([]string, 0, len(tex))
	for name := range tex {
		names = append(names, name)
	}
	slices.Sort(names)
	best, bestRank := "", -1
	for _, name := range names {
		if !importDocument.MatchString(activeTeX(tex[name])) {
			continue
		}
		rank := 100 - min(90, strings.Count(name, "/"))
		if slices.Contains([]string{"main.tex", "root.tex", "master.tex"}, strings.ToLower(path.Base(name))) {
			rank += 2
		}
		if rank > bestRank {
			best, bestRank = name, rank
		}
	}
	if best != "" {
		return best, nil
	}
	if len(names) == 1 {
		return names[0], nil
	}
	return "", errors.New("non trovo un documento principale. Indica main_tex nelle impostazioni del progetto esterno")
}

func writeImportConfig(source, stage, title, structure, buildDir, filename string) error {
	if filename == "" {
		filename = filepath.Join(source, "notesflow.json")
	}
	raw := make(map[string]json.RawMessage)
	if data, err := os.ReadFile(filename); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if raw == nil {
		raw = make(map[string]json.RawMessage)
	}
	var tools map[string]any
	if data := raw["tools"]; len(data) > 0 {
		if err := json.Unmarshal(data, &tools); err != nil {
			return err
		}
	}
	if tools == nil {
		tools = make(map[string]any)
	}
	tools["main_tex"], tools["pdf"], tools["build_dir"] = "main.tex", "main.pdf", buildDir
	for key, value := range map[string]any{"project": title, "structure": structure, "tools": tools} {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		raw[key] = data
	}
	return storage.Save(filepath.Join(stage, "notesflow.json"), raw)
}
