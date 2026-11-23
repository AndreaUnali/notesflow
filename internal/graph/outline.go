package graph

import (
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

// In an outline, milestones come from numbered topics or leaf headings.
func roadmapMilestones(road *parser.Roadmap) ([]parser.Milestone, bool) {
	items := slices.Clone(road.Items)
	if !strings.EqualFold(path.Ext(road.File), ".md") || slices.ContainsFunc(items, func(item parser.Milestone) bool { return item.Done != nil }) {
		return items, false
	}
	var numbered, headings []parser.Milestone
	for _, item := range items {
		if !item.Heading {
			continue
		}
		headings = append(headings, item)
		if topicNumber.MatchString(item.Title) {
			numbered = append(numbered, item)
		}
	}
	steps := numbered
	if len(steps) == 0 {
		for i, item := range headings {
			if i == 0 && item.Depth == 0 && len(headings) == 1 {
				continue
			}
			if i+1 == len(headings) || headings[i+1].Depth <= item.Depth {
				steps = append(steps, item)
			}
		}
	}
	if len(steps) == 0 {
		return items, false
	}
	for i := range steps {
		done := false
		steps[i].Done = &done
	}
	return steps, true
}

func outlineEvidence(road, title string, sections []noteSection, frequency termFrequency) (parser.RoadmapEvidence, bool) {
	plainTitle := topicAside.ReplaceAllString(topicNumber.ReplaceAllString(strings.ReplaceAll(title, `\"`, `"`), ""), "")
	goal := conceptTerms(plainTitle)
	var qualifier map[string]bool
	if _, tail, ok := strings.Cut(plainTitle, ":"); ok {
		qualifier = conceptTerms(tail)
	}
	terms := slices.Sorted(maps.Keys(goal))
	if len(terms) == 0 {
		return parser.RoadmapEvidence{}, false
	}
	total := 0.0
	for _, term := range terms {
		total += frequency.weight(term)
	}
	var best noteSection
	bestScore, bestCoverage, runnerUp := 0.0, 0.0, 0.0
	exact := false
	for _, section := range sections {
		if section.profile.words < 8 || !inRoadmapScope(road, section.File) {
			continue
		}
		head, covered, hits, anchored, salient := 0.0, 0.0, 0, 0, 0
		for _, term := range terms {
			weight := frequency.weight(term)
			if section.profile.title[term] {
				hits++
				anchored++
				if frequency.counts[term] <= max(3, frequency.total/4) {
					salient++
				}
				head += weight
				covered += weight
			} else if section.profile.body[term] {
				hits++
				covered += weight
			}
		}
		match := sameTerms(goal, section.profile.title)
		ratio := covered / total
		// A rare named concept or a full subtitle can support partial coverage.
		partialAnchor := salient > 0 && ratio >= .27 && section.profile.words >= 30
		subtitle := false
		if len(qualifier) >= 2 && section.Level >= 3 {
			var matched, wordCount int
			for term := range qualifier {
				if term == "model" || term == "primitive" {
					continue
				}
				wordCount++
				if section.profile.body[term] || section.profile.title[term] {
					matched++
				}
			}
			subtitle = wordCount >= 2 && matched == wordCount
		}
		ordinary := hits >= 2 && ratio >= .44 && (anchored > 0 || (hits >= 3 && ratio >= .70 && section.Level >= 3))
		if !match && !ordinary && !partialAnchor && !subtitle {
			continue
		}
		score := ratio*.65 + head/total*.35
		if match {
			score += .2
		}
		if score > bestScore {
			runnerUp = bestScore
			best, bestScore, bestCoverage, exact = section, score, ratio, match
		} else {
			runnerUp = max(runnerUp, score)
		}
	}
	if bestScore == 0 {
		return parser.RoadmapEvidence{}, false
	}
	evidence := parser.RoadmapEvidence{File: best.File, Line: best.profile.line, Kind: "outline"}
	switch {
	case best.pending:
		evidence.Review = "La sezione corrisponde alla tappa e contiene attività aperte"
	case !exact && bestCoverage < .67:
		evidence.Review = "Gli appunti coprono solo parte dei termini della tappa"
	case bestScore-runnerUp < .05:
		evidence.Review = "Più sezioni risultano pertinenti alla tappa"
	case !exact:
		evidence.Review = "Controlla se la sezione individuata copre tutta la tappa"
	case best.profile.words < 20:
		evidence.Review = "Il titolo corrisponde e il testo della sezione è ancora breve"
	}
	return evidence, true
}
