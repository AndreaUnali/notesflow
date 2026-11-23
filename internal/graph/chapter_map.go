package graph

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

func chapterMap(groups []Group, sections map[string]parser.Section, membership map[string]string, edges []Edge) []Edge {
	bridges := map[string]float64{}
	for _, edge := range edges {
		if membership[edge.Source] != membership[edge.Target] {
			bridges[edge.Source] = max(bridges[edge.Source], edge.Weight)
			bridges[edge.Target] = max(bridges[edge.Target], edge.Weight)
		}
	}

	overview := []Edge{}
	topics := map[string]bool{}
	for i := range groups {
		group := &groups[i]
		group.Topics = []string{}
		var candidates []string
		var primary []string
		for _, id := range group.Members {
			section := sections[id]
			if section.Kind != "section" || strings.EqualFold(section.Title, group.Title) {
				continue
			}
			candidates = append(candidates, id)
			if section.Level <= 3 {
				primary = append(primary, id)
			}
		}
		slices.SortStableFunc(candidates, func(a, b string) int {
			return cmp.Compare(bridges[b], bridges[a])
		})
		for _, id := range candidates {
			if bridges[id] <= 0 || len(group.Topics) >= 2 {
				break
			}
			group.Topics = append(group.Topics, id)
			topics[id] = true
		}
		for _, id := range append(slices.Clone(primary), group.Members...) {
			section := sections[id]
			if len(group.Topics) >= mapTopicsPerChapter {
				break
			}
			if section.Kind == "section" && !strings.EqualFold(section.Title, group.Title) && !topics[id] {
				group.Topics = append(group.Topics, id)
				topics[id] = true
			}
		}
		slices.SortStableFunc(group.Topics, func(a, b string) int { return cmp.Compare(sections[a].Line, sections[b].Line) })
		for _, id := range group.Topics {
			overview = append(overview, Edge{Source: group.ID, Target: id, Kind: "hierarchy", Weight: 1})
		}
	}
	pairs := map[pair]int{}
	semantic := map[pair]Edge{}
	for _, edge := range edges {
		if edge.Kind == "hierarchy" {
			continue
		}
		source, target := membership[edge.Source], membership[edge.Target]
		if source == "" || target == "" {
			continue
		}
		if edge.Kind == "semantic" {
			if topics[edge.Source] && topics[edge.Target] {
				key := undirected(source, target)
				if old, found := semantic[key]; !found || edge.Weight > old.Weight {
					semantic[key] = edge
				}
			}
			continue
		}
		if topics[edge.Source] && topics[edge.Target] {
			overview = append(overview, edge)
		} else if source != target {
			pairs[undirected(source, target)]++
		}
	}
	for key, n := range pairs {
		overview = append(overview, Edge{Source: key.a, Target: key.b, Kind: "ref", Weight: 1, Reason: fmt.Sprintf("%d riferimenti LaTeX", n)})
	}
	for _, edge := range semantic {
		overview = append(overview, edge)
	}
	sortEdges(overview)
	return overview
}
