package graph

import (
	"cmp"
	"slices"
	"strings"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

type sharedConcept struct {
	term      string
	weight    float64
	titleRank int
}

// Associations are calculated locally from course vocabulary and section titles.
func conceptEdges(visible map[string]parser.Section, profiles map[string]conceptProfile, all map[string]parser.Section) []Edge {
	var ids []string
	for id, section := range visible {
		if section.Kind == "section" && len(profiles[id].title) > 0 && profiles[id].words > 0 {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	postings := make(map[string][]string)
	var vocabulary []conceptProfile
	for _, id := range ids {
		profile := profiles[id]
		vocabulary = append(vocabulary, profile)
		for term := range profile.body {
			postings[term] = append(postings[term], id)
		}
	}
	frequency := profileFrequency(vocabulary)
	seen := make(map[pair]bool)
	var edges []Edge
	for _, id := range ids {
		a := profiles[id]
		for anchor := range a.title {
			post := postings[anchor]
			if len(post) > max(10, len(ids)*2/3) {
				continue
			}
			for _, other := range post {
				if id == other {
					continue
				}
				key := undirected(id, other)
				if seen[key] || isAncestor(all, id, other) || isAncestor(all, other, id) {
					continue
				}
				seen[key] = true
				score, terms := relatedConcepts(a, profiles[other], frequency)
				if score > 0 {
					edges = append(edges, Edge{Source: key.a, Target: key.b, Kind: "semantic", Origin: "local", Weight: score, Reason: "Concetti condivisi: " + strings.Join(terms, ", ")})
				}
			}
		}
	}
	return edges
}

func relatedConcepts(a, b conceptProfile, f termFrequency) (float64, []string) {
	var shared []sharedConcept
	anchors, commonTitles := 0, 0
	for term := range a.body {
		if !b.body[term] || f.counts[term] > max(10, f.total*2/3) {
			continue
		}
		rank := 0
		if a.title[term] || b.title[term] {
			anchors++
			rank = 1
		}
		if a.title[term] && b.title[term] {
			commonTitles++
			rank = 2
		}
		shared = append(shared, sharedConcept{term, f.weight(term), rank})
	}
	if anchors == 0 || (len(shared) < 3 && commonTitles < 2) {
		return 0, nil
	}
	if commonTitles == 0 && (anchors < 2 || len(shared) < 4) {
		return 0, nil
	}
	slices.SortFunc(shared, func(a, b sharedConcept) int {
		return cmp.Or(cmp.Compare(b.titleRank, a.titleRank), cmp.Compare(b.weight, a.weight), cmp.Compare(a.term, b.term))
	})
	var terms []string
	for _, item := range shared[:min(4, len(shared))] {
		terms = append(terms, item.term)
	}
	return min(.94, .65+float64(min(anchors, 4))*.026+float64(min(commonTitles, 3))*.035+float64(min(len(shared), 12))*.008), terms
}
