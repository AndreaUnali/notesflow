package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/AndreaUnali/notesflow/internal/storage"
)

var importReference = regexp.MustCompile(`\\(includegraphics\*?|includesvg|input|include|includeonly|bibliography|addbibresource|lstinputlisting|verbatiminput)\s*(?:\[[^\]\r\n]*\]\s*)?\{([^{}\r\n]+)\}`)
var graphicPath = regexp.MustCompile(`\\graphicspath\s*\{((?:[^{}]|\{[^{}]*\})*)\}`)
var graphicDir = regexp.MustCompile(`\{([^{}]+)\}`)
var chapterPrefix = regexp.MustCompile(`(?i)^(?:capitolo|chapter|ch)(?:[0-9]+|[ivxlcdm]+)[-_ ]+`)
var importClassLine = regexp.MustCompile(`(?m)^[ \t]*\\documentclass[ \t]*(?:\[[^\]\r\n]*\][ \t]*)?\{[^{}\r\n]+\}[ \t]*(?:%[^\r\n]*)?(?:\r?\n|$)`)

var graphicExtensions = []string{".pdf", ".png", ".jpg", ".jpeg", ".svg", ".eps", ".webp"}

type importLayout struct {
	main, scope, config, structure, buildDir string
	files                                    []string
	tex, destinations, index                 map[string]string
	graphics                                 []string
}

func planImport(files []string, tex map[string]string, main string) importLayout {
	p := importLayout{main: main, scope: path.Dir(main), structure: SingleFile, buildDir: ".notesflow/build",
		files: files, tex: tex, destinations: map[string]string{main: "main.tex"}, index: make(map[string]string)}
	if len(tex) > 1 {
		p.structure, p.buildDir = Chapters, "ausiliari"
	}
	for _, name := range files {
		p.index[name] = name
		if folded := strings.ToLower(name); p.index[folded] == "" {
			p.index[folded] = name
		}
	}
	for _, name := range files {
		for _, match := range graphicPath.FindAllStringSubmatch(activeTeX(tex[name]), -1) {
			declared := graphicDir.FindAllStringSubmatch(match[1], -1)
			if len(declared) == 0 {
				p.graphics = append(p.graphics, strings.TrimSpace(match[1]))
			}
			for _, dir := range declared {
				p.graphics = append(p.graphics, strings.TrimSpace(dir[1]))
			}
		}
	}

	body := activeTeX(tex[main])
	if begin := importBegin.FindStringIndex(body); begin != nil {
		for _, ref := range importReference.FindAllStringSubmatch(body[:begin[0]], -1) {
			if ref[1] == "input" {
				file := p.resolve(main, "input", ref[2])
				if file != "" && file != main && strings.EqualFold(path.Ext(file), ".tex") {
					p.config, p.destinations[file] = file, "config.tex"
					break
				}
			}
		}
	}

	claimed := map[string]bool{"main.tex": true, "main.pdf": true, "notesflow.json": true}
	if p.structure == Chapters {
		claimed["config.tex"] = true
	}
	chapter := 0
	addChapter := func(name string) {
		if _, ok := p.destinations[name]; ok {
			return
		}
		if _, ok := tex[name]; !ok {
			return
		}
		chapter++
		stem := strings.TrimSuffix(path.Base(name), path.Ext(name))
		stem = chapterPrefix.ReplaceAllString(stem, "")
		slug := strings.Join(strings.FieldsFunc(strings.ToLower(stem), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}), "-")
		if slug == "" {
			slug = "capitolo"
		}
		p.destinations[name] = fmt.Sprintf("capitoli/ch%d_%s.tex", chapter, slug)
	}
	for _, ref := range importReference.FindAllStringSubmatch(body, -1) {
		if ref[1] == "input" || ref[1] == "include" {
			addChapter(p.resolve(main, ref[1], ref[2]))
		}
	}

	images := make(map[string]bool)
	for _, name := range files {
		if _, ok := tex[name]; ok {
			addChapter(name)
			for _, ref := range importReference.FindAllStringSubmatch(activeTeX(tex[name]), -1) {
				if isGraphicCommand(ref[1]) {
					if asset := p.resolve(name, ref[1], ref[2]); asset != "" {
						images[asset] = true
					}
				}
			}
		}
	}
	for _, name := range files {
		if _, ok := p.destinations[name]; ok {
			continue
		}
		relative := p.relative(name)
		if relative == "notesflow.json" || strings.EqualFold(name, strings.TrimSuffix(main, path.Ext(main))+".pdf") {
			continue
		}
		if images[name] || isImageFile(name, relative) {
			relative = path.Base(name)
			if p.structure == Chapters {
				relative = path.Join("immagini", relative)
			}
		}
		p.destinations[name] = uniqueImportName(claimed, relative)
	}
	return p
}

func (p importLayout) relative(name string) string {
	if p.scope != "." {
		if relative, ok := strings.CutPrefix(name, p.scope+"/"); ok {
			return relative
		}
	}
	return name
}

func isImageFile(name, relative string) bool {
	extension := strings.ToLower(path.Ext(name))
	if !slices.Contains(graphicExtensions, extension) {
		return false
	}
	if path.Dir(relative) == "." && extension != ".pdf" {
		return true
	}
	for _, dir := range strings.Split(path.Dir(relative), "/") {
		if slices.Contains([]string{"immagini", "images", "img", "figure", "figures", "figs", "graphics", "media"}, strings.ToLower(dir)) {
			return true
		}
	}
	return false
}

func uniqueImportName(claimed map[string]bool, name string) string {
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	unique := name
	for n := 2; claimed[strings.ToLower(unique)]; n++ {
		unique = fmt.Sprintf("%s-%d%s", stem, n, ext)
	}
	claimed[strings.ToLower(unique)] = true
	return unique
}

func isGraphicCommand(kind string) bool {
	return strings.HasPrefix(kind, "includegraphics") || kind == "includesvg"
}

func (p importLayout) resolve(from, kind, argument string) string {
	name := strings.Trim(strings.TrimSpace(argument), "\"")
	if name == "" || path.IsAbs(name) || strings.ContainsAny(name, "\\{}#") {
		return ""
	}
	bases := []string{p.scope, path.Dir(from)}
	extensions := []string{""}
	switch {
	case isGraphicCommand(kind):
		extensions = append(extensions, graphicExtensions...)
		for _, dir := range p.graphics {
			if dir != "" && !path.IsAbs(dir) && !strings.ContainsAny(dir, "\\{}") {
				bases = append(bases, path.Join(p.scope, dir))
			}
		}
	case kind == "include" || kind == "input" || kind == "includeonly":
		extensions = append(extensions, ".tex")
	case kind == "bibliography":
		extensions = append(extensions, ".bib")
	}
	for _, base := range bases {
		for _, ext := range extensions {
			candidate := path.Join(base, name+ext)
			if file := p.index[candidate]; file != "" {
				return file
			}
			if file := p.index[strings.ToLower(candidate)]; file != "" {
				return file
			}
		}
	}
	return ""
}

func (p importLayout) rewriteTeX(file, text string) string {
	text = importReference.ReplaceAllStringFunc(text, func(full string) string {
		match := importReference.FindStringSubmatchIndex(full)
		kind, argument := full[match[2]:match[3]], full[match[4]:match[5]]
		pieces := []string{argument}
		if kind == "includeonly" || kind == "bibliography" {
			pieces = strings.Split(argument, ",")
		}
		for i, piece := range pieces {
			target := p.destinations[p.resolve(file, kind, piece)]
			if target == "" {
				continue
			}
			if kind == "include" || kind == "includeonly" || kind == "bibliography" || kind == "includesvg" ||
				kind == "input" && path.Ext(strings.Trim(strings.TrimSpace(piece), "\"")) == "" {
				target = strings.TrimSuffix(target, path.Ext(target))
			}
			pieces[i] = target
		}
		return full[:match[4]] + strings.Join(pieces, ",") + full[match[5]:]
	})
	if len(p.graphics) > 0 {
		directory := "./"
		if p.structure == Chapters {
			directory = "immagini/"
		}
		text = graphicPath.ReplaceAllString(text, "\\graphicspath{{"+directory+"}}")
	}
	if file == p.main {
		if !importDocument.MatchString(activeTeX(text)) {
			text = "\\documentclass{article}\n\\usepackage{graphicx}\n\\begin{document}\n" + text + "\n\\end{document}\n"
		}
	}
	return text
}

func extractImportConfig(main string) (string, string) {
	config := "% Impostazioni condivise del progetto.\n"
	if begin := importBegin.FindStringIndex(main); begin != nil {
		preamble := begin[0]
		if class := importClassLine.FindStringIndex(main[:preamble]); class != nil {
			preamble = class[1]
			config += main[preamble:begin[0]]
		}
		main = main[:preamble] + "\\input{config}\n\n" + main[begin[0]:]
	}
	return main, config
}

func (p importLayout) markdownReferences() *strings.Replacer {
	froms := make(map[string]string)
	for _, name := range p.files {
		target, ok := p.destinations[name]
		if !ok || target == name {
			continue
		}
		froms[name] = target
		if relative := p.relative(name); relative != name && relative != target {
			froms[relative] = target
		}
	}
	keys := make([]string, 0, len(froms))
	for key := range froms {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int { return len(b) - len(a) })
	var pairs []string
	for _, key := range keys {
		pairs = append(pairs, key, froms[key])
	}
	return strings.NewReplacer(pairs...)
}

func (p importLayout) write(ctx context.Context, source, stage, title, configFile string) error {
	main := p.rewriteTeX(p.main, p.tex[p.main])
	dirs := []string{".notesflow"}
	if p.structure == Chapters {
		dirs = append(dirs, "capitoli", "immagini", "ausiliari")
		if p.config == "" {
			var documentConfig string
			main, documentConfig = extractImportConfig(main)
			if err := os.WriteFile(filepath.Join(stage, "config.tex"), []byte(documentConfig), 0644); err != nil {
				return err
			}
		}
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(stage, dir), 0755); err != nil {
			return err
		}
	}
	markdown := p.markdownReferences()
	for _, file := range p.files {
		if err := ctx.Err(); err != nil {
			return err
		}
		relative := p.destinations[file]
		if relative == "" {
			continue
		}
		target := filepath.Join(stage, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		var err error
		original := filepath.Join(source, filepath.FromSlash(file))
		if file == p.main {
			err = os.WriteFile(target, []byte(main), 0644)
		} else if text, ok := p.tex[file]; ok {
			err = os.WriteFile(target, []byte(p.rewriteTeX(file, text)), 0644)
		} else if strings.EqualFold(path.Ext(file), ".md") {
			var data []byte
			data, err = os.ReadFile(original)
			if err == nil {
				err = os.WriteFile(target, []byte(markdown.Replace(string(data))), 0644)
			}
		} else {
			err = copyImportFile(original, target)
		}
		if err != nil {
			return err
		}
	}
	if err := writeImportConfig(source, stage, title, p.structure, p.buildDir, configFile); err != nil {
		return err
	}
	return storage.Save(filepath.Join(stage, ".notesflow", "import.json"), importOrigin{
		Source: source, Document: filepath.Join(source, filepath.FromSlash(p.main)),
	})
}

func copyImportFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}
