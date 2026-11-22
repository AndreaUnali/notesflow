// Package graph maintains the persisted source index and projects it into a graph.
package graph

import (
	"cmp"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/storage"
)

type Edge struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Kind   string  `json:"kind"`
	Weight float64 `json:"weight"`
	Reason string  `json:"reason,omitempty"`
	Origin string  `json:"origin,omitempty"`
}

type Activity struct {
	Time time.Time
	Text string
}

type Snapshot struct {
	Revision uint64           `json:"revision"`
	Nodes    []parser.Node    `json:"nodes"`
	Edges    []Edge           `json:"edges"`
	Tasks    []parser.Task    `json:"tasks"`
	Roadmaps []parser.Roadmap `json:"roadmaps"`
	Pending  int              `json:"pending"`
}

type inference struct {
	Hash  string
	Edges []Edge
}

type revision struct {
	Time     time.Time
	NodeID   string
	Previous string
	Hash     string
	Summary  string
}

type diskState struct {
	Version   int
	Documents map[string]parser.Document
	Inferred  map[string]inference
	History   []revision
}

type Store struct {
	mu       sync.RWMutex
	path     string
	state    diskState
	sections map[string]parser.Section
	snapshot Snapshot
	change   chan struct{}
	activity []Activity
}

func Open(filename string) (*Store, error) {
	store := &Store{
		path: filename, change: make(chan struct{}),
		state: diskState{Version: 1, Documents: map[string]parser.Document{}, Inferred: map[string]inference{}},
	}
	if err := storage.Load(filename, &store.state); err != nil {
		return nil, fmt.Errorf("indice %s: %w", filename, err)
	}
	if store.state.Version != 1 || store.state.Documents == nil || store.state.Inferred == nil {
		return nil, fmt.Errorf("formato indice non valido: %s", filename)
	}
	store.rebuild()
	return store, nil
}

// Snapshot returns immutable slices. A change closes the returned channel.
func (s *Store) Snapshot() (Snapshot, <-chan struct{}) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot, s.change
}

func (s *Store) Hashes() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.state.Documents))
	for file, doc := range s.state.Documents {
		out[file] = doc.Hash
	}
	return out
}

func (s *Store) Log(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activity = append(s.activity, Activity{Time: time.Now(), Text: message})
	if len(s.activity) > 150 {
		s.activity = slices.Clone(s.activity[len(s.activity)-150:])
	}
}

func (s *Store) Activity() []Activity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.activity)
}

func (s *Store) Apply(changed map[string]parser.Document, deleted []string) error {
	if len(changed)+len(deleted) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state
	next.Documents = maps.Clone(next.Documents)
	next.Inferred = maps.Clone(next.Inferred)
	for file, doc := range changed {
		next.Documents[file] = doc
	}
	for _, file := range deleted {
		delete(next.Documents, file)
	}
	sections := collectSections(next.Documents)
	for id, result := range next.Inferred {
		section, exists := sections[id]
		if !exists || section.Hash != result.Hash {
			delete(next.Inferred, id)
			continue
		}
		result.Edges = slices.DeleteFunc(slices.Clone(result.Edges), func(edge Edge) bool {
			target, exists := sections[edge.Target]
			return !exists || target.Hash != s.sections[edge.Target].Hash
		})
		next.Inferred[id] = result
	}
	next.History = recordHistory(next.History, s.sections, sections)
	return s.commit(next)
}

func recordHistory(history []revision, before, after map[string]parser.Section) []revision {
	var changes []revision
	now := time.Now()
	for id, section := range after {
		if old := before[id].Hash; old != section.Hash {
			changes = append(changes, revision{Time: now, NodeID: id, Previous: old, Hash: section.Hash, Summary: section.Summary})
		}
	}
	for id, section := range before {
		if _, exists := after[id]; !exists {
			changes = append(changes, revision{Time: now, NodeID: id, Previous: section.Hash})
		}
	}
	if len(changes) == 0 {
		return history
	}
	slices.SortFunc(changes, func(a, b revision) int { return cmp.Compare(a.NodeID, b.NodeID) })
	history = append(slices.Clone(history), changes...)
	return slices.Clone(history[max(0, len(history)-500):])
}

type Candidate struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

type Delta struct {
	Source       parser.Node `json:"source"`
	Hash         string      `json:"-"`
	Text         string      `json:"text"`
	Candidates   []Candidate `json:"index"`
	targetHashes map[string]string
}

func (s *Store) Pending() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ids []string
	for _, node := range s.snapshot.Nodes {
		if s.pending(s.sections[node.ID]) {
			ids = append(ids, node.ID)
		}
	}
	return ids
}

func (s *Store) pending(section parser.Section) bool {
	if section.Text == "" || parser.IsRoadmap(section.File) {
		return false
	}
	old, exists := s.state.Inferred[section.ID]
	return !exists || old.Hash != section.Hash
}

// Delta sends one changed section, plus a bounded index of compact summaries.
func (s *Store) Delta(id string, limit, textLimit int) (Delta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, exists := s.sections[id]
	if !exists || !s.pending(source) {
		return Delta{}, false
	}
	type ranked struct {
		section parser.Section
		score   int
	}
	tokens := Terms(source.Title + " " + clip(source.Text, textLimit))
	order := make([]ranked, 0, len(s.sections))
	for _, target := range s.sections {
		if target.ID == id || target.Text == "" || parser.IsRoadmap(target.File) {
			continue
		}
		score := 0
		for word := range Terms(target.Title + " " + target.Summary) {
			if _, ok := tokens[word]; ok {
				score++
			}
		}
		order = append(order, ranked{target, score})
	}
	slices.SortFunc(order, func(a, b ranked) int {
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.section.ID, b.section.ID))
	})
	delta := Delta{
		Source: source.Node, Hash: source.Hash, Text: clip(source.Text, textLimit),
		Candidates: make([]Candidate, 0, min(limit, len(order))), targetHashes: make(map[string]string),
	}
	for _, item := range order[:min(limit, len(order))] {
		target := item.section
		delta.Candidates = append(delta.Candidates, Candidate{target.ID, target.Title, target.Summary})
		delta.targetHashes[target.ID] = target.Hash
	}
	return delta, true
}

func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for index := range text {
		if limit == 0 {
			return text[:index]
		}
		limit--
	}
	return text
}

var stopWords = strings.Fields("della delle della dello degli nella nello nelle negli alla alla alle agli dalla dalle dagli questo questa questi queste quello quella quelli quelle sono essere come quando quindi anche senza sulla sulle salvo perché parte fatto section subsection chapter begin document label ref eqref input include testo nota esempio esempi todo visual")

func Terms(text string) map[string]struct{} {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
	result := make(map[string]struct{})
	for _, word := range words {
		if len([]rune(word)) >= 4 && !slices.Contains(stopWords, word) {
			result[word] = struct{}{}
		}
	}
	return result
}

// Resolve ignores stale results if a source or a returned target changed in flight.
func (s *Store) Resolve(delta Delta, edges []Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, exists := s.sections[delta.Source.ID]
	if !exists || source.Hash != delta.Hash {
		return nil
	}
	valid := make([]Edge, 0, len(edges))
	for _, edge := range edges {
		hash, offered := delta.targetHashes[edge.Target]
		target, exists := s.sections[edge.Target]
		if !offered || !exists {
			continue
		}
		if target.Hash != hash {
			return nil
		}
		edge.Source, edge.Kind, edge.Weight = source.ID, "semantic", max(.1, min(1, edge.Weight))
		valid = append(valid, edge)
	}
	next := s.state
	next.Inferred = maps.Clone(next.Inferred)
	next.Inferred[source.ID] = inference{Hash: source.Hash, Edges: valid}
	return s.commit(next)
}

func (s *Store) commit(next diskState) error {
	if err := storage.Save(s.path, &next); err != nil {
		return err
	}
	s.state = next
	s.rebuild()
	close(s.change)
	s.change = make(chan struct{})
	return nil
}

func collectSections(docs map[string]parser.Document) map[string]parser.Section {
	out := make(map[string]parser.Section)
	for _, doc := range docs {
		for _, section := range doc.Sections {
			out[section.ID] = section
		}
	}
	return out
}

func (s *Store) rebuild() {
	s.sections = collectSections(s.state.Documents)
	snap := Snapshot{
		Revision: s.snapshot.Revision + 1, Nodes: []parser.Node{}, Edges: []Edge{},
		Tasks: []parser.Task{}, Roadmaps: []parser.Roadmap{},
	}
	labels := map[string][]parser.Section{}
	for _, doc := range s.state.Documents {
		snap.Tasks = append(snap.Tasks, doc.Tasks...)
		if doc.Roadmap != nil {
			snap.Roadmaps = append(snap.Roadmaps, *doc.Roadmap)
		}
		for _, label := range doc.Labels {
			labels[label.Value] = append(labels[label.Value], s.sections[label.Source])
		}
	}
	type edgeKey struct{ source, target, kind string }
	edges := map[edgeKey]Edge{}
	add := func(edge Edge) {
		if _, exists := s.sections[edge.Target]; !exists || edge.Source == edge.Target {
			return
		}
		key := edgeKey{edge.Source, edge.Target, edge.Kind}
		if edge.Kind == "semantic" && key.source > key.target {
			key.source, key.target = key.target, key.source
		}
		prior, exists := edges[key]
		if !exists || prior.Weight < edge.Weight || (prior.Weight == edge.Weight && edge.Source < prior.Source) {
			edges[key] = edge
		}
	}
	for _, section := range s.sections {
		snap.Nodes = append(snap.Nodes, section.Node)
		if s.pending(section) {
			snap.Pending++
		}
		if section.Parent != "" {
			add(Edge{Source: section.Parent, Target: section.ID, Kind: "hierarchy", Weight: 1})
		}
		if inferred, ok := s.state.Inferred[section.ID]; ok && inferred.Hash == section.Hash {
			for _, edge := range inferred.Edges {
				add(edge)
			}
		}
	}
	for _, doc := range s.state.Documents {
		for _, link := range doc.Links {
			var target string
			switch link.Kind {
			case "ref", "eqref":
				target = resolveLabel(doc.File, labels[link.Value])
			case "input", "include":
				target = resolveFile(doc.File, link.Value, s.state.Documents)
			}
			add(Edge{Source: link.Source, Target: target, Kind: link.Kind, Weight: 1, Reason: link.Value})
		}
	}
	snap.Edges = slices.AppendSeq(snap.Edges, maps.Values(edges))
	slices.SortFunc(snap.Nodes, func(a, b parser.Node) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.ID, b.ID))
	})
	slices.SortFunc(snap.Tasks, func(a, b parser.Task) int { return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line)) })
	slices.SortFunc(snap.Roadmaps, func(a, b parser.Roadmap) int { return cmp.Compare(a.File, b.File) })
	slices.SortFunc(snap.Edges, func(a, b Edge) int {
		return cmp.Or(cmp.Compare(a.Source, b.Source), cmp.Compare(a.Target, b.Target), cmp.Compare(a.Kind, b.Kind))
	})
	s.snapshot = snap
}

func resolveLabel(file string, candidates []parser.Section) string {
	best := -1
	id := ""
	for _, target := range candidates {
		rank := 0
		switch {
		case target.File == file:
			rank = 2
		case target.Course == parser.Course(file):
			rank = 1
		}
		if rank > best {
			best, id = rank, target.ID
		} else if rank == best && id != target.ID {
			id = ""
		}
	}
	return id
}

func resolveFile(file, value string, docs map[string]parser.Document) string {
	if value == "" || path.IsAbs(value) {
		return ""
	}
	if path.Ext(value) == "" {
		value += ".tex"
	}
	for _, candidate := range []string{path.Join(path.Dir(file), value), path.Clean(value)} {
		if doc, exists := docs[candidate]; exists && len(doc.Sections) > 0 {
			return doc.Sections[0].ID
		}
	}
	return ""
}
