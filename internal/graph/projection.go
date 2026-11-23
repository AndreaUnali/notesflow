package graph

import (
	"cmp"
	"fmt"
	"math"
	"path"
	"slices"
	"strings"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

const GraphPageSize = 14
const MapChaptersPerPage = 3
const mapTopicsPerChapter = 4
const semanticDegree = 3

type Group struct {
	parser.Node
	Members []string `json:"members"`
	Topics  []string `json:"topics"`
}

func sortNodes(nodes []parser.Node) {
	slices.SortFunc(nodes, func(a, b parser.Node) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.ID, b.ID))
	})
}

func explicitEdges(docs map[string]parser.Document, labels map[string][]parser.Section) ([]Edge, map[string]bool) {
	var edges []Edge
	referenced := make(map[string]bool)
	for _, doc := range docs {
		if doc.Roadmap != nil {
			continue
		}
		for _, link := range doc.Links {
			var target string
			switch link.Kind {
			case "ref", "eqref":
				target = resolveLabel(doc.File, labels[link.Value])
				if target != "" && target != link.Source {
					referenced[target] = true
				}
			case "input", "include":
				target = resolveFile(doc.File, link.Value, docs)
			}
			if target != "" && target != link.Source {
				edges = append(edges, Edge{Source: link.Source, Target: target, Kind: link.Kind, Weight: 1, Reason: link.Value})
			}
		}
	}
	return edges, referenced
}

func significantSections(docs map[string]parser.Document, referenced map[string]bool) (map[string]parser.Section, []parser.Node, []parser.Node) {
	visible := make(map[string]parser.Section)
	files, headings := []parser.Node{}, []parser.Node{}
	for _, doc := range docs {
		if len(doc.Sections) == 0 {
			continue
		}
		root := doc.Sections[0]
		files = append(files, root.Node)
		if doc.Roadmap != nil {
			continue
		}
		majorLevel := 3
		if strings.EqualFold(path.Ext(doc.File), ".md") {
			majorLevel = 2
		}
		substance := make(map[string]bool)
		for i := len(doc.Sections) - 1; i >= 1; i-- {
			section := doc.Sections[i]
			headings = append(headings, section.Node)
			if section.ContentWords > 0 || referenced[section.ID] || substance[section.ID] {
				substance[section.Parent] = true
				level := section.Level
				// Older caches are reparsed on scan. Keep the first familiar view meanwhile.
				if level == 0 {
					level = majorLevel
				}
				primary := level <= majorLevel
				subtopic := level == majorLevel+1 && (section.ContentWords >= 22 || referenced[section.ID] || substance[section.ID])
				deepTopic := level > majorLevel+1 && section.ContentWords >= 76 && len(conceptTerms(section.Title)) >= 2
				if primary || subtopic || deepTopic {
					visible[section.ID] = section
				}
			}
		}
		if substance[root.ID] || (root.ContentWords >= 12 && !assemblyOnly(doc)) {
			visible[root.ID] = root
		}
	}
	return visible, files, headings
}

func assemblyOnly(doc parser.Document) bool {
	if !strings.EqualFold(path.Ext(doc.File), ".tex") {
		return false
	}
	name := strings.ToLower(strings.TrimSuffix(path.Base(doc.File), path.Ext(doc.File)))
	switch name {
	case "config", "preamble", "preambolo", "macros", "packages", "settings":
		return true
	}
	return slices.ContainsFunc(doc.Links, func(link parser.Link) bool {
		return link.Source == doc.File && (link.Kind == "input" || link.Kind == "include")
	})
}

func isAncestor(all map[string]parser.Section, root, id string) bool {
	for parent := all[id].Parent; parent != ""; parent = all[parent].Parent {
		if parent == root {
			return true
		}
	}
	return false
}

type pair struct{ a, b string }

func undirected(a, b string) pair {
	if a > b {
		a, b = b, a
	}
	return pair{a, b}
}

func projectEdges(all, visible map[string]parser.Section, explicit, local []Edge, inferred map[string]inference) []Edge {
	type edgeKey struct{ source, target, kind string }
	known := map[edgeKey]Edge{}
	structural := map[pair]bool{}
	owner := func(id string) string {
		for id != "" {
			if _, ok := visible[id]; ok {
				return id
			}
			id = all[id].Parent
		}
		return ""
	}
	add := func(edge Edge) {
		if edge.Source == "" || edge.Target == "" || edge.Source == edge.Target {
			return
		}
		key := edgeKey{edge.Source, edge.Target, edge.Kind}
		if previous, ok := known[key]; !ok || edge.Weight > previous.Weight ||
			(edge.Weight == previous.Weight && edge.Reason < previous.Reason) {
			known[key] = edge
		}
		if edge.Kind != "semantic" {
			structural[undirected(edge.Source, edge.Target)] = true
		}
	}
	for _, edge := range explicit {
		edge.Source, edge.Target = owner(edge.Source), owner(edge.Target)
		add(edge)
	}
	for _, section := range visible {
		add(Edge{Source: owner(section.Parent), Target: section.ID, Kind: "hierarchy", Weight: 1})
	}
	var semantic []Edge
	for _, edge := range local {
		if !structural[undirected(edge.Source, edge.Target)] {
			semantic = append(semantic, edge)
		}
	}
	for id, result := range inferred {
		source, ok := visible[id]
		if !ok || source.Kind != "section" || result.Hash != source.Hash {
			continue
		}
		for _, edge := range result.Edges {
			target, ok := visible[edge.Target]
			if !ok || target.Kind != "section" || structural[undirected(id, target.ID)] ||
				isAncestor(all, id, target.ID) || isAncestor(all, target.ID, id) {
				continue
			}
			edge.Source = id
			if edge.Origin == "mock" {
				score, terms := LocalAffinity(source.Title, source.Summary, target.Title, target.Summary)
				if score == 0 {
					continue
				}
				edge.Weight, edge.Reason = score, "Termini distintivi: "+strings.Join(terms, ", ")
			}
			if edge.Weight >= .65 && !math.IsNaN(edge.Weight) {
				semantic = append(semantic, edge)
			}
		}
	}
	slices.SortFunc(semantic, func(a, b Edge) int {
		return cmp.Or(cmp.Compare(b.Weight, a.Weight), cmp.Compare(a.Source, b.Source), cmp.Compare(a.Target, b.Target), cmp.Compare(a.Reason, b.Reason))
	})
	degree := map[string]int{}
	used := map[pair]bool{}
	for _, edge := range semantic {
		key := undirected(edge.Source, edge.Target)
		if used[key] || degree[key.a] >= semanticDegree || degree[key.b] >= semanticDegree {
			continue
		}
		used[key] = true
		degree[key.a]++
		degree[key.b]++
		add(edge)
	}
	result := make([]Edge, 0, len(known))
	for _, edge := range known {
		result = append(result, edge)
	}
	sortEdges(result)
	return result
}

func sortEdges(edges []Edge) {
	slices.SortFunc(edges, func(a, b Edge) int {
		return cmp.Or(cmp.Compare(a.Source, b.Source), cmp.Compare(a.Target, b.Target), cmp.Compare(a.Kind, b.Kind))
	})
}

// LocalAffinity requires shared vocabulary anchored in a section title.
func LocalAffinity(titleA, bodyA, titleB, bodyB string) (float64, []string) {
	a, b := Terms(titleA+" "+bodyA), Terms(titleB+" "+bodyB)
	headA, headB := Terms(titleA), Terms(titleB)
	var shared []string
	anchors, sharedTitle := 0, 0
	for word := range a {
		if _, ok := b[word]; !ok {
			continue
		}
		shared = append(shared, word)
		_, inA := headA[word]
		_, inB := headB[word]
		if inA || inB {
			anchors++
		}
		if inA && inB {
			sharedTitle++
		}
	}
	if len(shared) < 2 || anchors == 0 {
		return 0, nil
	}
	ratio := float64(len(shared)) / float64(max(1, len(a)+len(b)-len(shared)))
	if (sharedTitle == 0 && (anchors < 2 || len(shared) < 3 || ratio < .15)) ||
		(sharedTitle == 1 && len(shared) < 3 && ratio < .22) {
		return 0, nil
	}
	slices.Sort(shared)
	return min(.95, .65+ratio*.55+float64(sharedTitle)*.035), shared[:min(5, len(shared))]
}

func groupGraph(nodes []parser.Node, sections map[string]parser.Section, edges, sourceEdges []Edge) ([]Group, []Edge) {
	groups := []Group{}
	byFile := map[string][]parser.Node{}
	var files []string
	for _, node := range nodes {
		if _, ok := byFile[node.File]; !ok {
			files = append(files, node.File)
		}
		byFile[node.File] = append(byFile[node.File], node)
	}
	membership := map[string]string{}
	add := func(anchor parser.Node, members []parser.Node) {
		n := anchor
		n.ID, n.Kind = "group:"+anchor.ID, "group"
		group := Group{Node: n}
		for _, member := range members {
			group.Members = append(group.Members, member.ID)
			membership[member.ID] = n.ID
		}
		group.Summary = fmt.Sprintf("%d nodi consultabili", len(members))
		groups = append(groups, group)
	}
	order := make(map[string]int)
	for _, edge := range sourceEdges {
		if (edge.Kind == "input" || edge.Kind == "include") && strings.EqualFold(path.Base(edge.Source), "main.tex") {
			if _, exists := order[edge.Target]; !exists {
				order[edge.Target] = len(order) + 1
			}
		}
	}
	slices.SortStableFunc(files, func(a, b string) int {
		ra, rb := order[a], order[b]
		if ra == 0 {
			ra = len(order) + 1
		}
		if rb == 0 {
			rb = len(order) + 1
		}
		return cmp.Compare(ra, rb)
	})
	for _, file := range files {
		members := byFile[file]
		root := members[0]
		// For a long standalone file, the top-level concepts are the chapter overview.
		if len(files) == 1 && len(members) > GraphPageSize {
			grouped := map[string][]parser.Node{}
			var order []string
			for _, node := range members {
				if node.Kind == "file" {
					continue
				}
				anchor := node.ID
				for ancestor := sections[node.ID].Parent; ancestor != ""; ancestor = sections[ancestor].Parent {
					if s, ok := sections[ancestor]; ok && s.Kind == "section" {
						anchor = s.ID
					}
				}
				if _, exists := grouped[anchor]; !exists {
					order = append(order, anchor)
				}
				grouped[anchor] = append(grouped[anchor], node)
			}
			if len(order) > 1 {
				for _, anchor := range order {
					add(sections[anchor].Node, grouped[anchor])
				}
				continue
			}
		}
		if len(members) > 1 {
			root.Title = members[1].Title
			root.Line = members[1].Line
		}
		add(root, members)
	}
	overview := chapterMap(groups, sections, membership, edges)

	return groups, overview
}

// SourceNode also resolves a heading reached explicitly through search.
func SourceNode(snapshot Snapshot, id string) (parser.Node, bool) {
	for _, list := range [][]parser.Node{snapshot.Nodes, snapshot.Headings} {
		for _, node := range list {
			if node.ID == id {
				return node, true
			}
		}
	}
	return parser.Node{}, false
}
