package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

var ErrSourceChanged = errors.New("il sorgente è cambiato")

type Chapter struct {
	Main, File, Title string
	Line              int
}

var documentEnd = regexp.MustCompile(`(?m)^[\t ]*\\end\s*\{document\}`)
var documentClass = regexp.MustCompile(`(?m)^[\t ]*\\documentclass(?:\[[^\r\n]*\])?\s*\{(book|report|memoir|scrbook|scrreprt)\}`)

func PlanChapter(root, main, title string) (Chapter, error) {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" || len([]rune(title)) > 120 {
		return Chapter{}, errors.New("inserisci un titolo da 1 a 120 caratteri")
	}
	absolute, err := WorkspaceFile(root, main)
	if err != nil {
		return Chapter{}, errors.New("scegli un documento principale prima di creare il capitolo")
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return Chapter{}, err
	}
	closing := documentEnd.FindAllIndex(data, -1)
	if len(closing) != 1 {
		return Chapter{}, errors.New("serve una chiusura univoca \\end{document}")
	}
	slug := strings.ToLower(title)
	slug = strings.Join(strings.FieldsFunc(slug, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), "-")
	runes := []rune(slug)
	if len(runes) > 60 {
		slug = string(runes[:60])
	}
	if slug == "" {
		return Chapter{}, errors.New("il titolo deve contenere almeno una lettera o un numero")
	}
	folder := filepath.Join(filepath.Dir(main), "capitoli")
	if alt := filepath.Join(filepath.Dir(main), "chapters"); !directory(filepath.Join(root, folder)) && directory(filepath.Join(root, alt)) {
		folder = alt
	}
	file := filepath.ToSlash(filepath.Join(folder, slug+".tex"))
	if _, err := os.Lstat(filepath.Join(root, file)); !errors.Is(err, os.ErrNotExist) {
		return Chapter{}, fmt.Errorf("il file %s esiste già o non è disponibile", file)
	}
	return Chapter{Main: main, File: file, Title: title, Line: 1 + bytes.Count(data[:closing[0][0]], []byte("\n"))}, nil
}

func directory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func CreateChapter(root string, plan Chapter) error {
	current, err := PlanChapter(root, plan.Main, plan.Title)
	if err != nil {
		return err
	}
	if current != plan {
		return ErrSourceChanged
	}
	main, err := WorkspaceFile(root, plan.Main)
	if err != nil {
		return err
	}
	original, err := os.ReadFile(main)
	if err != nil {
		return err
	}
	closing := documentEnd.FindAllIndex(original, -1)
	if len(closing) != 1 || bytes.Count(original[:closing[0][0]], []byte("\n"))+1 != plan.Line {
		return ErrSourceChanged
	}
	path := filepath.Join(root, filepath.FromSlash(plan.File))
	folder := filepath.Dir(path)
	if err := os.MkdirAll(folder, 0755); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(folder)
	if err != nil || resolved != folder {
		return errors.New("la cartella del capitolo deve essere interna al progetto e senza alias")
	}
	relative, err := filepath.Rel(filepath.Dir(main), path)
	if err != nil {
		return err
	}
	relative = strings.TrimSuffix(filepath.ToSlash(relative), ".tex")
	if strings.ContainsAny(relative, "{}%#\\\r\n") {
		return errors.New("il percorso del capitolo contiene caratteri speciali LaTeX")
	}
	eol := "\n"
	if bytes.Contains(original, []byte("\r\n")) {
		eol = "\r\n"
	}
	heading := "section"
	if documentClass.Match(original) {
		heading = "chapter"
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	esc := strings.NewReplacer(`\`, `\textbackslash{}`, "#", `\#`, "$", `\$`, "%", `\%`, "&", `\&`,
		"_", `\_`, "{", `\{`, "}", `\}`, "^", `\textasciicircum{}`, "~", `\textasciitilde{}`)
	_, writeErr := fmt.Fprintf(file, "\\%s{%s}%s", heading, esc.Replace(plan.Title), eol)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	at := closing[0][0]
	revised := append(bytes.Clone(original[:at]), "\\input{"+relative+"}"+eol...)
	revised = append(revised, original[at:]...)
	if err := ReplaceSource(main, original, revised); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}

// ReplaceSource stages a complete file and rechecks its original bytes before replacing it.
func ReplaceSource(path string, original, updated []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	staged, err := os.CreateTemp(filepath.Dir(path), ".notesflow-edit-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	err = staged.Chmod(info.Mode().Perm())
	if err == nil {
		_, err = staged.Write(updated)
	}
	if err == nil {
		err = staged.Sync()
	}
	if err = errors.Join(err, staged.Close()); err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return ErrSourceChanged
	}
	return os.Rename(staged.Name(), path)
}

func ChapterName(file, summary string) string {
	name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	if index := strings.Index(summary, `\chapter{`); index >= 0 {
		if title, _, ok := strings.Cut(summary[index+len(`\chapter{`):], "}"); ok && parser.HeadingAnchor(title) != "" {
			return title
		}
	}
	return strings.NewReplacer("-", " ", "_", " ").Replace(name)
}
