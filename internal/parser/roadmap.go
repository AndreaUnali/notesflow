package parser

import (
	"regexp"
	"strings"
	"unicode"
)

// SourceDigest includes the parser revision so existing caches acquire new metadata.
func SourceDigest(data []byte) string { return "source-v4:" + Digest(data) }

var (
	completedTeX    = regexp.MustCompile(`(?i)^\s*DONE(?:\s+VISUAL)?\s*:\s*(.+?)\s*$`)
	pendingNote     = regexp.MustCompile(`(?i)\s*<!--\s*notesflow:\s*pending\s*-->\s*$`)
	checkLink       = regexp.MustCompile(`(?i)\s+\[verifica\]\(<?([^<>()]+)>?\)\s*$`)
	texListItem     = regexp.MustCompile(`^\\item(?:\[[^\]]*\])?\s+`)
	texInlineFormat = regexp.MustCompile(`^\\(?:textbf|textit|emph|texttt|textsc|underline)\s*\{`)
)

func (m *Milestone) parseCheck() {
	if marker := pendingNote.FindStringIndex(m.Title); marker != nil {
		m.Title = strings.TrimSpace(m.Title[:marker[0]])
		m.KeepPending = true
	}
	if link := checkLink.FindStringSubmatchIndex(m.Title); link != nil {
		m.Check = strings.TrimSpace(m.Title[link[2]:link[3]])
		m.Title = strings.TrimSpace(m.Title[:link[0]])
	}
}

func (s *Section) addContent(line string, number int) {
	trimmed := strings.TrimSpace(line)
	if list := texListItem.FindStringIndex(trimmed); list != nil {
		trimmed = strings.TrimSpace(trimmed[list[1]:])
	}
	if format := texInlineFormat.FindStringIndex(trimmed); format != nil {
		trimmed = strings.TrimSpace(trimmed[format[1]:])
	}
	if trimmed == "" || strings.ContainsRune("\\#<!>|", rune(trimmed[0])) {
		return
	}
	words := strings.FieldsFunc(strings.ToLower(trimmed), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	if len(words) < 4 {
		return
	}
	lower := strings.Join(words, " ")
	for _, prefix := range []string{"todo ", "tbd ", "da scrivere ", "da completare ", "da aggiungere "} {
		if strings.HasPrefix(lower, prefix) {
			return
		}
	}
	if s.ContentLine == 0 {
		s.ContentLine = number
	}
	s.ContentWords += len(words)
}

// HeadingAnchor is the normalized form used by local Markdown heading links.
func HeadingAnchor(title string) string { return slug(title) }
