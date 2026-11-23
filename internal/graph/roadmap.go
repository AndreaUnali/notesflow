package graph

import (
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

type noteSection struct {
	parser.Section
	pending bool
	topic   []string
	profile conceptProfile
}

func projectRoadmaps(docs map[string]parser.Document, profiles map[string]conceptProfile) ([]parser.Roadmap, []parser.Task) {
	files := make([]string, 0, len(docs))
	var roadmapFiles []string
	for file, doc := range docs {
		if doc.Roadmap != nil {
			roadmapFiles = append(roadmapFiles, file)
		} else {
			files = append(files, file)
		}
	}
	if len(roadmapFiles) == 0 {
		return []parser.Roadmap{}, nil
	}
	sort.Strings(files)
	sort.Strings(roadmapFiles)

	var sections []noteSection
	var vocabulary []conceptProfile
	for _, file := range files {
		for _, section := range docs[file].Sections {
			profile := profiles[section.ID]
			if section.Kind == "section" && profile.line > 0 {
				sections = append(sections, noteSection{Section: section, pending: profile.pending, topic: topicWords(section.Title), profile: profile})
				vocabulary = append(vocabulary, profile)
			}
		}
	}
	frequency := profileFrequency(vocabulary)

	roads := make([]parser.Roadmap, 0)
	var reviews []parser.Task
	for _, file := range roadmapFiles {
		raw := docs[file].Roadmap
		road := *raw
		var outline bool
		road.Items, outline = roadmapMilestones(raw)
		completed := completedNotes(file, files, docs)
		for i := range road.Items {
			item := &road.Items[i]
			if item.Done == nil || *item.Done || item.KeepPending {
				continue
			}
			evidence, ok := milestoneEvidence(file, *item, docs, completed, sections, outline, frequency)
			if !ok {
				continue
			}
			done := true
			item.Done, item.Evidence = &done, &evidence
			if evidence.Review != "" {
				reviews = append(reviews, parser.Task{
					NodeID: file, File: file, Line: item.Line, Section: "Roadmap",
					Kind: parser.MaintenanceTask,
					Text: fmt.Sprintf("Verifica «%s». %s (%s:%d).", item.Title, evidence.Review, evidence.File, evidence.Line),
				})
			}
		}
		roads = append(roads, road)
	}
	return roads, reviews
}

func completedNotes(road string, files []string, docs map[string]parser.Document) map[string]parser.RoadmapEvidence {
	result := map[string]parser.RoadmapEvidence{}
	for _, file := range files {
		doc := docs[file]
		if !inRoadmapScope(road, file) {
			continue
		}
		for _, done := range doc.Completions {
			key := strings.Join(roadWords(done.Text), " ")
			if key == "" {
				continue
			}
			if _, exists := result[key]; !exists {
				result[key] = parser.RoadmapEvidence{File: file, Line: done.Line, Kind: "completion"}
			}
		}
	}
	return result
}

func milestoneEvidence(road string, item parser.Milestone, docs map[string]parser.Document, completed map[string]parser.RoadmapEvidence, sections []noteSection, outline bool, frequency termFrequency) (parser.RoadmapEvidence, bool) {
	if item.Check != "" {
		return checkEvidence(road, item.Check, docs)
	}
	if evidence, ok := completed[strings.Join(roadWords(topicNumber.ReplaceAllString(item.Title, "")), " ")]; ok {
		return evidence, true
	}
	if outline {
		return outlineEvidence(road, item.Title, sections, frequency)
	}
	return headingEvidence(road, item.Title, sections)
}

func checkEvidence(road, raw string, docs map[string]parser.Document) (parser.RoadmapEvidence, bool) {
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "" || target.Host != "" || target.User != nil || target.RawQuery != "" ||
		target.Path == "" || path.IsAbs(target.Path) || strings.Contains(target.Path, "\\") {
		return parser.RoadmapEvidence{}, false
	}
	file := path.Join(path.Dir(road), target.Path)
	doc, exists := docs[file]
	if !exists || doc.Roadmap != nil {
		return parser.RoadmapEvidence{}, false
	}
	matchingLabels := map[string]bool{}
	for _, label := range doc.Labels {
		if label.Value == target.Fragment {
			matchingLabels[label.Source] = true
		}
	}
	var matches []parser.Section
	for _, section := range doc.Sections {
		if section.ContentLine <= 0 {
			continue
		}
		targetMatches := target.Fragment == "" || matchingLabels[section.ID] ||
			(strings.EqualFold(path.Ext(file), ".md") && section.Kind == "section" && parser.HeadingAnchor(section.Title) == target.Fragment)
		if targetMatches {
			matches = append(matches, section)
		}
	}
	if len(matches) == 0 {
		return parser.RoadmapEvidence{}, false
	}
	evidence := parser.RoadmapEvidence{File: file, Line: matches[0].ContentLine, Kind: "check"}
	if target.Fragment != "" && (len(matches) > 1 || len(matchingLabels) > 1) {
		evidence.Review = "Il riferimento corrisponde a più sezioni"
	}
	return evidence, true
}

func headingEvidence(road, title string, sections []noteSection) (parser.RoadmapEvidence, bool) {
	words := roadWords(title)
	if len(words) < 2 {
		return parser.RoadmapEvidence{}, false
	}
	verb := words[0]
	var checkWriting bool
	switch verb {
	case "scrivere", "definire", "descrivere", "documentare", "spiegare", "write", "define", "describe", "document", "explain":
		checkWriting = true
	case "aggiungere", "formalizzare", "dimostrare", "sviluppare", "completare", "add", "prove", "complete":
	default:
		return parser.RoadmapEvidence{}, false
	}
	topic := withoutConnectors(words[1:])
	if len(topic) == 0 {
		return parser.RoadmapEvidence{}, false
	}
	var best noteSection
	bestRank, ties := 0, 0
	for _, section := range sections {
		if !inRoadmapScope(road, section.File) {
			continue
		}
		rank := topicMatch(topic, section.topic)
		if rank > bestRank {
			best, bestRank, ties = section, rank, 0
		} else if rank > 0 && rank == bestRank {
			ties++
		}
	}
	if bestRank == 0 {
		return parser.RoadmapEvidence{}, false
	}
	evidence := parser.RoadmapEvidence{File: best.File, Line: best.profile.line, Kind: "heading"}
	switch {
	case ties > 0:
		evidence.Review = "Più sezioni degli appunti corrispondono alla tappa"
	case bestRank < 100:
		evidence.Review = "Il titolo della sezione corrisponde solo in parte alla tappa"
	case best.pending:
		evidence.Review = "La sezione contiene ancora attività aperte"
	case !checkWriting:
		evidence.Review = "La sezione esiste. Controlla che soddisfi l’obiettivo della tappa"
	case best.profile.words < 12:
		evidence.Review = "La sezione contiene ancora poco testo"
	}
	return evidence, true
}

func topicMatch(a, b []string) int {
	if len(b) == 0 {
		return 0
	}
	if strings.Join(a, " ") == strings.Join(b, " ") {
		return 100
	}
	terms := make(map[string]bool, len(a))
	for _, term := range a {
		terms[term] = true
	}
	common := 0
	for _, term := range b {
		if terms[term] {
			common++
			delete(terms, term)
		}
	}
	size := max(len(a), len(b))
	if common < 2 || common*3 < size*2 {
		return 0
	}
	return min(99, common*100/size)
}

func roadWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r)
	})
}

func topicWords(text string) []string { return withoutConnectors(roadWords(text)) }

func withoutConnectors(words []string) []string {
	terms := make([]string, 0, len(words))
	for _, word := range words {
		switch word {
		case "il", "lo", "la", "i", "gli", "le", "l", "un", "uno", "una", "di", "del", "dello", "della", "dei", "degli", "delle", "e", "a", "su", "the", "of", "and", "an":
			continue
		}
		terms = append(terms, word)
	}
	return terms
}

func inRoadmapScope(road, file string) bool {
	dir := path.Dir(road)
	return dir == "." || strings.HasPrefix(file, dir+"/")
}
