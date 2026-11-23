package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateChapterAddsOneIncludeAndKeepsExistingNotes(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "main.tex")
	original := "\\documentclass{report}\n\\begin{document}\nAppunti.\n\\end{document}\n"
	if err := os.WriteFile(main, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	chapter, err := PlanChapter(root, "main.tex", "Code & memoria")
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateChapter(root, chapter); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "Appunti.\n\\input{capitoli/code-memoria}\n\\end{document}") {
		t.Fatalf("wrong TeX include: %s", got)
	}
	content, err := os.ReadFile(filepath.Join(root, chapter.File))
	if err != nil || string(content) != "\\chapter{Code \\& memoria}\n" {
		t.Fatalf("wrong new chapter: %q, %v", content, err)
	}
	if err := CreateChapter(root, chapter); err == nil {
		t.Fatal("existing chapter overwritten")
	}
}
