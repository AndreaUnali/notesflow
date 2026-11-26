package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AndreaUnali/notesflow/internal/config"
)

func TestCreateProjectUsesManagedBuildAndPreservesExistingFiles(t *testing.T) {
	for _, structure := range []string{SingleFile, Chapters} {
		t.Run(structure, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "Documents", "Notesflow")
			title := "Analisi e probabilità"
			root, err := createProjectIn(parent, title, structure)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(root, "")
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(root) != title || cfg.Project != title || cfg.Structure != structure ||
				cfg.Tools.MainTeX != "main.tex" || cfg.Tools.PDF != "main.pdf" || cfg.Tools.LatexEngine != "latexmk" {
				t.Fatalf("unexpected project settings: %+v", cfg)
			}
			main := filepath.Join(root, cfg.Tools.MainTeX)
			if err := os.WriteFile(main, []byte("testo da conservare"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := createProjectIn(parent, title, structure); err == nil {
				t.Fatal("creation accepted an existing project")
			}
			text, err := os.ReadFile(main)
			if err != nil || string(text) != "testo da conservare" {
				t.Fatal("creation changed the existing project")
			}
			dir, release, err := prepareBuildDir(root, cfg.Tools.BuildDir)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			expected := filepath.Join(root, ".notesflow", "build")
			if structure == Chapters {
				expected = filepath.Join(root, "ausiliari")
				if _, err := os.Stat(filepath.Join(root, "capitoli", "ch1_introduzione.tex")); err != nil {
					t.Fatal(err)
				}
			}
			if dir != expected {
				t.Fatalf("build folder = %s, want %s", dir, expected)
			}
		})
	}
}

func TestProjectTitleCannotSpecifyAnotherLocation(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "Notesflow")
	for _, title := range []string{"", ".", "..", "../Altro", "/Altro", "Corso/Schemi", "Corso\\Schemi"} {
		if _, err := createProjectIn(parent, title, SingleFile); err == nil {
			t.Errorf("accepted invalid project title %q", title)
		}
	}
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("invalid titles created the project folder: %v", err)
	}
}

func TestPublishPDFKeepsLastValidFile(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "main.pdf")
	if err := os.WriteFile(destination, []byte("%PDF-previous"), 0600); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(dir, "partial.pdf")
	if err := os.WriteFile(candidate, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := publishPDF(candidate, destination); err == nil {
		t.Fatal("accepted invalid PDF output")
	}
	pdf, err := os.ReadFile(destination)
	if err != nil || string(pdf) != "%PDF-previous" {
		t.Fatal("invalid output replaced the last successful PDF")
	}
}
