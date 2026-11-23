// Package parser indexes source positions and common LaTeX and Markdown constructs.
package parser

import (
	"crypto/sha256"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
)

type Node struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Course  string `json:"course"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Summary string `json:"summary"`
}

type Section struct {
	Node
	Parent        string
	Hash          string
	Text          string
	Level         int
	ContentLine   int
	ContentWords  int
	OpenChecklist bool
}

type Link struct {
	Source string
	Kind   string
	Value  string
}

type Task struct {
	NodeID  string `json:"node_id"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Section string `json:"section"`
	Kind    string `json:"kind"`
	Text    string `json:"text"`
}

const MaintenanceTask = "MANUTENZIONE"

type Completion struct {
	Text string
	Line int
}

type RoadmapEvidence struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Kind   string `json:"kind"`
	Review string `json:"review,omitempty"`
}

type Milestone struct {
	Heading     bool             `json:"heading,omitempty"`
	Title       string           `json:"title"`
	Line        int              `json:"line"`
	Depth       int              `json:"depth"`
	Done        *bool            `json:"done,omitempty"`
	Check       string           `json:"check,omitempty"`
	KeepPending bool             `json:"keep_pending,omitempty"`
	Evidence    *RoadmapEvidence `json:"evidence,omitempty"`
}

type Roadmap struct {
	File   string      `json:"file"`
	Course string      `json:"course"`
	Items  []Milestone `json:"items"`
	Note   string      `json:"note,omitempty"`
}

func (r Roadmap) Progress() (done, total int) {
	for _, item := range r.Items {
		if item.Done != nil {
			total++
			if *item.Done {
				done++
			}
		}
	}
	return
}

type Document struct {
	File        string
	Hash        string
	Sections    []Section
	Labels      []Link
	Links       []Link
	Tasks       []Task
	Completions []Completion
	Roadmap     *Roadmap
}

var (
	taskRE     = regexp.MustCompile(`(?i)^\s*TODO\s*(VISUAL|MANUTENZIONE)?\s*:\s*(.+?)\s*$`)
	headingRE  = regexp.MustCompile(`^\s*(#{1,6})\s+(.+?)\s*#*\s*$`)
	checkboxRE = regexp.MustCompile(`^(\s*)(?:[-+*]|\d+[.)])\s+\[([ xX])\]\s+(.+?)\s*$`)
	bulletRE   = regexp.MustCompile(`^(\s*)(?:[-+*]|\d+[.)])\s+(.+?)\s*$`)
	commandRE  = regexp.MustCompile(`\\([A-Za-z]+)\*?`)
	verbatimRE = regexp.MustCompile(`\\(begin|end)\{(verbatim\*?|lstlisting|minted|comment)\}`)
)

var levels = map[string]int{"part": 1, "chapter": 2, "section": 3, "subsection": 4, "subsubsection": 5, "paragraph": 6, "subparagraph": 7}

func Digest(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func Course(file string) string {
	course, _, nested := strings.Cut(file, "/")
	if nested {
		return course
	}
	return "Workspace"
}

func IsRoadmap(file string) bool {
	base := strings.ToLower(path.Base(file))
	return base == "roadmap.md" || base == "roadmap.pdf"
}

func Parse(file string, data []byte) Document {
	doc := Document{File: file, Hash: SourceDigest(data)}
	title := strings.TrimSuffix(path.Base(file), path.Ext(file))
	doc.Sections = []Section{{Node: Node{ID: file, Title: title, Kind: "file", File: file, Course: Course(file), Line: 1}}}
	if IsRoadmap(file) {
		doc.Roadmap = &Roadmap{File: file, Course: Course(file)}
	}
	isTeX := strings.EqualFold(path.Ext(file), ".tex")
	current := 0
	stack := []int{0}
	depth := []int{0}
	occurrences := map[string]int{}
	var body strings.Builder
	var fence, verbatim string

	finish := func() {
		section := &doc.Sections[current]
		section.Text = strings.TrimSpace(body.String())
		section.Hash = Digest([]byte(section.Title + "\n" + section.Text))
		summary := clip(strings.Join(strings.Fields(section.Text), " "), 240)
		section.Summary = summary
		body.Reset()
	}
	start := func(title string, line, level int) {
		finish()
		for depth[len(depth)-1] >= level {
			depth = depth[:len(depth)-1]
			stack = stack[:len(stack)-1]
		}
		parent := doc.Sections[stack[len(stack)-1]].ID
		key := parent + "/" + slug(title)
		occurrences[key]++
		id := key + ":" + fmt.Sprint(occurrences[key])
		if parent == file {
			id = file + "#" + strings.TrimPrefix(id, file+"/")
		}
		doc.Sections = append(doc.Sections, Section{Node: Node{
			ID: id, Title: title, Kind: "section", File: file, Course: Course(file), Line: line,
		}, Parent: parent, Level: level})
		current = len(doc.Sections) - 1
		stack = append(stack, current)
		depth = append(depth, level)
	}
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	starts := map[int][]texCommand{}
	if isTeX {
		starts = texCommands(normalized)
	}
	for index, line := range strings.Split(normalized, "\n") {
		number := index + 1
		if isTeX {
			code, comment := splitTeXComment(line)
			if verbatim != "" {
				if strings.Contains(code, "\\end{"+verbatim+"}") {
					verbatim = ""
				}
				body.WriteString(line + "\n")
				continue
			}
			if marker := verbatimRE.FindStringSubmatch(code); marker != nil && marker[1] == "begin" {
				if !strings.Contains(code, "\\end{"+marker[2]+"}") {
					verbatim = marker[2]
				}
				body.WriteString(line + "\n")
				continue
			}
			for _, command := range starts[number] {
				if level := levels[command.name]; level != 0 {
					start(strings.Join(strings.Fields(command.arg), " "), number, level)
					continue
				}
				link := Link{Source: doc.Sections[current].ID, Kind: command.name, Value: strings.TrimSpace(command.arg)}
				if command.name == "label" {
					doc.Labels = append(doc.Labels, link)
				} else {
					doc.Links = append(doc.Links, link)
				}
			}
			if doc.Roadmap == nil {
				if done := completedTeX.FindStringSubmatch(comment); done != nil {
					doc.Completions = append(doc.Completions, Completion{Text: done[1], Line: number})
				}
				doc.Sections[current].addContent(code, number)
			}
			if task := taskRE.FindStringSubmatch(comment); task != nil {
				section := doc.Sections[current]
				kind := "TODO"
				if task[1] != "" {
					kind = strings.ToUpper(task[1])
				}
				doc.Tasks = append(doc.Tasks, Task{section.ID, file, number, section.Title, kind, task[2]})
			}
		} else {
			trimmed := strings.TrimSpace(line)
			marker := fenceMarker(trimmed)
			switch {
			case fence != "":
				if marker != "" && marker[0] == fence[0] && len(marker) >= len(fence) && strings.TrimSpace(trimmed[len(marker):]) == "" {
					fence = ""
				}
			case marker != "":
				fence = marker
			default:
				if heading := headingRE.FindStringSubmatch(line); heading != nil {
					start(heading[2], number, len(heading[1]))
				}
				if doc.Roadmap != nil {
					addMilestone(doc.Roadmap, line, number)
				} else if task := checkboxRE.FindStringSubmatch(line); task != nil {
					if task[2] != " " {
						doc.Completions = append(doc.Completions, Completion{Text: task[3], Line: number})
					} else {
						doc.Sections[current].OpenChecklist = true
					}
				} else if !headingRE.MatchString(line) {
					doc.Sections[current].addContent(trimmed, number)
				}
			}
		}
		body.WriteString(line + "\n")
	}
	finish()
	return doc
}

func addMilestone(roadmap *Roadmap, line string, number int) {
	switch {
	case headingRE.MatchString(line):
		match := headingRE.FindStringSubmatch(line)
		roadmap.Items = append(roadmap.Items, Milestone{Title: match[2], Heading: true, Line: number, Depth: len(match[1]) - 1})
	case checkboxRE.MatchString(line):
		match := checkboxRE.FindStringSubmatch(line)
		done := match[2] != " "
		milestone := Milestone{Title: match[3], Line: number, Depth: 1 + len(match[1])/2, Done: &done}
		milestone.parseCheck()
		roadmap.Items = append(roadmap.Items, milestone)
	case bulletRE.MatchString(line):
		match := bulletRE.FindStringSubmatch(line)
		roadmap.Items = append(roadmap.Items, Milestone{Title: match[2], Line: number, Depth: 1 + len(match[1])/2})
	case strings.TrimSpace(line) != "":
		roadmap.Items = append(roadmap.Items, Milestone{Title: strings.TrimSpace(line), Line: number})
	}
}

func fenceMarker(line string) string {
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return ""
	}
	end := 0
	for end < len(line) && line[end] == line[0] {
		end++
	}
	if end < 3 {
		return ""
	}
	return line[:end]
}

func splitTeXComment(line string) (string, string) {
	for index := 0; index < len(line); index++ {
		switch line[index] {
		case '\\':
			index++
		case '%':
			return line[:index], line[index+1:]
		}
	}
	return line, ""
}

type texCommand struct{ name, arg string }

func texCommands(text string) map[int][]texCommand {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i], _ = splitTeXComment(line)
	}
	code := strings.Join(lines, "\n")
	out := make(map[int][]texCommand)
	line, cursor := 1, 0
	for _, match := range commandRE.FindAllStringSubmatchIndex(code, -1) {
		if match[0] < cursor || escaped(code, match[0]) {
			continue
		}
		name := code[match[2]:match[3]]
		if levels[name] == 0 && name != "label" && name != "ref" && name != "eqref" && name != "input" && name != "include" {
			continue
		}
		line += strings.Count(code[cursor:match[0]], "\n")
		cursor = match[0]
		argStart := skipSpace(code, match[1])
		if levels[name] != 0 && argStart < len(code) && code[argStart] == '[' {
			_, end, ok := group(code, argStart, '[', ']')
			if ok {
				argStart = skipSpace(code, end)
			}
		}
		arg, end, ok := group(code, argStart, '{', '}')
		if !ok && name == "input" {
			end = argStart
			for end < len(code) && !strings.ContainsRune(" \t\r\n{}\\", rune(code[end])) {
				end++
			}
			arg, ok = code[argStart:end], end > argStart
		}
		if ok {
			out[line] = append(out[line], texCommand{name, arg})
			line += strings.Count(code[cursor:end], "\n")
			cursor = end
		}
	}
	return out
}

func skipSpace(code string, index int) int {
	for index < len(code) && strings.ContainsRune(" \t\r\n", rune(code[index])) {
		index++
	}
	return index
}

func group(code string, start int, open, close byte) (string, int, bool) {
	if start >= len(code) || code[start] != open {
		return "", start, false
	}
	depth := 1
	for index := start + 1; index < len(code); index++ {
		if escaped(code, index) {
			continue
		}
		switch code[index] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return code[start+1 : index], index + 1, true
			}
		}
	}
	return "", start, false
}

func escaped(code string, index int) bool {
	backslashes := 0
	for index > 0 && code[index-1] == '\\' {
		backslashes++
		index--
	}
	return backslashes%2 == 1
}

func slug(title string) string {
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 {
		return "sezione"
	}
	return strings.Join(words, "-")
}

func clip(text string, limit int) string {
	for index := range text {
		if limit == 0 {
			return text[:index]
		}
		limit--
	}
	return text
}
