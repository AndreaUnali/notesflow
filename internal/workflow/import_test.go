package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndreaUnali/notesflow/internal/config"
)

func TestImportRenamesChaptersAndFiguresAndReopensEditedCopy(t *testing.T) {
	source := t.TempDir()
	parent := filepath.Join(t.TempDir(), "Documents", "Notesflow")
	original := map[string]string{
		"Latex/main-ase.tex":                   "\\documentclass{report}\n\\input{config-ase.tex}\n\\begin{document}\n\\include{capitoli/capitolo3-secondo}\n\\input{capitoli/capitolo2-primo}\n\\end{document}\n",
		"Latex/config-ase.tex":                 "\\usepackage{graphicx}\n\\graphicspath{capitoli/Immagini/}\n",
		"Latex/capitoli/capitolo3-secondo.tex": "\\chapter{Secondo}\n\\includegraphics{capitoli/Immagini/Schema.png}\n",
		"Latex/capitoli/capitolo2-primo.tex":   "\\chapter{Primo}\n\\includegraphics{Schema}\n",
		"Latex/capitoli/immagini/Schema.png":   "figura",
		"Latex/capitoli/capitolo2-primo.aux":   "build precedente",
		"Latex/fonti/dispensa.pdf":             "fonte",
		"Latex/main-ase.pdf":                   "%PDF-previous",
		"Latex/AGENTS.md":                      "Documento in `main-ase.tex`, impostazioni in `config-ase.tex`.",
	}
	for file, text := range original {
		dest := filepath.Join(source, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := importProjectIn(context.Background(), source, parent, "Appunti originali", "")
	if err != nil || !result.Created {
		t.Fatalf("import failed: %+v %v", result, err)
	}
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(result.Root, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	cfg, err := config.Load(result.Root, "")
	if err != nil || cfg.Structure != Chapters || cfg.Tools.BuildDir != "ausiliari" || cfg.Tools.MainTeX != "main.tex" {
		t.Fatalf("unexpected imported config: %+v %v", cfg, err)
	}
	main := read("main.tex")
	if !strings.Contains(main, "\\input{config.tex}") ||
		!strings.Contains(main, "\\include{capitoli/ch1_secondo}") ||
		!strings.Contains(main, "\\input{capitoli/ch2_primo}") {
		t.Fatal(main)
	}
	for _, chapter := range []string{"capitoli/ch1_secondo.tex", "capitoli/ch2_primo.tex"} {
		if !strings.Contains(read(chapter), "\\includegraphics{immagini/Schema.png}") {
			t.Fatal("an image reference was not updated")
		}
	}
	if !strings.Contains(read("config.tex"), "\\graphicspath{{immagini/}}") ||
		read("immagini/Schema.png") != "figura" || read("fonti/dispensa.pdf") != "fonte" ||
		read("main.pdf") != "%PDF-previous" || strings.Contains(read("AGENTS.md"), "main-ase.tex") {
		t.Fatal("the figures, reference files or project documentation were not preserved")
	}
	if err := os.WriteFile(filepath.Join(result.Root, "main.tex"), []byte("modifica locale"), 0644); err != nil {
		t.Fatal(err)
	}
	again, err := importProjectIn(context.Background(), source, parent, "Titolo differente", "")
	if err != nil || again.Created || again.Root != result.Root || read("main.tex") != "modifica locale" {
		t.Fatalf("reopen did not preserve the working copy: %+v %v", again, err)
	}
	viaDocumentFolder, err := importProjectIn(context.Background(), filepath.Join(source, "Latex"), parent, "Titolo differente", "")
	if err != nil || viaDocumentFolder.Created || viaDocumentFolder.Root != result.Root {
		t.Fatalf("opening the document folder created a duplicate: %+v %v", viaDocumentFolder, err)
	}
	for file, text := range original {
		data, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(file)))
		if err != nil || string(data) != text {
			t.Fatalf("changed original %s", file)
		}
	}
}

func TestImportSingleFileKeepsFiguresAtRoot(t *testing.T) {
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "img"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "img", "foto.png"), []byte("foto"), 0644); err != nil {
		t.Fatal(err)
	}
	text := "\\documentclass{article}\n\\usepackage{graphicx}\n\\begin{document}\n\\includegraphics{img/foto.png}\n\\end{document}\n"
	if err := os.WriteFile(filepath.Join(source, "appunti.tex"), []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := importProjectIn(context.Background(), source, t.TempDir(), "Appunti brevi", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(result.Root, "")
	if err != nil || cfg.Structure != SingleFile || cfg.Tools.BuildDir != ".notesflow/build" {
		t.Fatalf("unexpected configuration: %+v %v", cfg, err)
	}
	data, err := os.ReadFile(filepath.Join(result.Root, "main.tex"))
	if err != nil || !strings.Contains(string(data), "\\includegraphics{foto.png}") {
		t.Fatalf("unexpected main file: %v %s", err, data)
	}
	if _, err := os.Stat(filepath.Join(result.Root, "foto.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(result.Root, "capitoli")); !os.IsNotExist(err) {
		t.Fatalf("single file import created a chapters folder: %v", err)
	}
}
