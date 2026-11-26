package workflow

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/gofrs/flock"

	"github.com/AndreaUnali/notesflow/internal/storage"
)

const (
	SingleFile       = "file-unico"
	Chapters         = "capitoli"
	projectsLockFile = ".projects.lock"
)

//go:embed templates
var templates embed.FS

// CreateProject reserves a folder with the project title under Documents/Notesflow.
func CreateProject(title, structure string) (string, error) {
	parent, err := projectsDirectory()
	if err != nil {
		return "", err
	}
	return createProjectIn(parent, title, structure)
}

func projectsDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Documents", "Notesflow"), nil
}

func projectTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("inserisci il titolo del progetto")
	}
	if !filepath.IsLocal(title) || title == "." || strings.ContainsAny(title, "/\\") || strings.IndexFunc(title, unicode.IsControl) >= 0 {
		return "", errors.New("usa un titolo senza barre o caratteri di controllo")
	}
	return title, nil
}

func createProjectIn(parent, title, structure string) (string, error) {
	title, err := projectTitle(title)
	if err != nil {
		return "", err
	}
	files := []string{"main.tex"}
	dirs := []string{}
	buildDir := ".notesflow/build"
	switch structure {
	case SingleFile:
	case Chapters:
		files = append(files, "config.tex", "capitoli/ch1_introduzione.tex")
		dirs = append(dirs, "capitoli", "ausiliari", "immagini")
		buildDir = "ausiliari"
	default:
		return "", errors.New("scegli File unico oppure A capitoli")
	}
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", fmt.Errorf("creazione della cartella dei progetti: %w", err)
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	lock := flock.New(filepath.Join(parent, projectsLockFile))
	defer lock.Close()
	if err := lock.Lock(); err != nil {
		return "", err
	}
	root := filepath.Join(parent, title)
	if err := os.Mkdir(root, 0755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", errors.New("esiste già un progetto con questo titolo. Scegli un altro titolo o aprilo dal selettore")
		}
		return "", err
	}
	var created []string
	complete := false
	defer func() {
		if !complete {
			for i := len(created) - 1; i >= 0; i-- {
				_ = os.Remove(created[i])
			}
			_ = os.Remove(root)
		}
	}()
	for _, relative := range dirs {
		dir := filepath.Join(root, relative)
		if err := os.Mkdir(dir, 0755); err != nil {
			return "", err
		}
		created = append(created, dir)
	}
	for _, relative := range files {
		data, err := templates.ReadFile("templates/" + structure + "/" + relative)
		if err != nil {
			return "", err
		}
		file := filepath.Join(root, filepath.FromSlash(relative))
		handle, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return "", err
		}
		created = append(created, file)
		_, writeErr := handle.Write(data)
		if err := errors.Join(writeErr, handle.Close()); err != nil {
			return "", err
		}
	}
	meta := map[string]any{
		"project":   title,
		"structure": structure,
		"tools":     map[string]string{"main_tex": "main.tex", "pdf": "main.pdf", "build_dir": buildDir},
	}
	if err := storage.Save(filepath.Join(root, "notesflow.json"), meta); err != nil {
		return "", fmt.Errorf("salvataggio della configurazione del progetto: %w", err)
	}
	complete = true
	return root, nil
}
