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
	Revision      uint64           `json:"revision"`
	Nodes         []parser.Node    `json:"nodes"`
	Files         []parser.Node    `json:"files"`
	Headings      []parser.Node    `json:"headings"`
	Groups        []Group          `json:"groups"`
	OverviewEdges []Edge           `json:"overview_edges"`
	Edges         []Edge           `json:"edges"`
	Tasks         []parser.Task    `json:"tasks"`
	Roadmaps      []parser.Roadmap `json:"roadmaps"`
	Pending       int              `json:"pending"`
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
	mu        sync.RWMutex
	path      string
	state     diskState
	sections  map[string]parser.Section
	projected map[string]parser.Section
	explicit  []Edge
	local     []Edge
	snapshot  Snapshot
	change    chan struct{}
	activity  []Activity
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
	store.rebuild(true)
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
	return s.commit(next, true)
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
	if section.Kind != "section" || section.ContentWords == 0 {
		return false
	}
	old, exists := s.state.Inferred[section.ID]
	return !exists || old.Hash != section.Hash
}

// Delta sends one changed section, plus a bounded index of compact summaries.
func (s *Store) Delta(id string, limit, textLimit int) (Delta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, exists := s.projected[id]
	if !exists || !s.pending(source) {
		return Delta{}, false
	}
	type ranked struct {
		section parser.Section
		score   int
	}
	tokens := Terms(source.Title + " " + clip(source.Text, textLimit))
	blocked := map[string]bool{}
	for _, edge := range s.snapshot.Edges {
		if edge.Kind == "semantic" {
			continue
		}
		if edge.Source == id {
			blocked[edge.Target] = true
		}
		if edge.Target == id {
			blocked[edge.Source] = true
		}
	}
	order := make([]ranked, 0, len(s.projected))
	for _, target := range s.projected {
		if target.ID == id || blocked[target.ID] || target.Kind != "section" || target.ContentWords == 0 ||
			isAncestor(s.sections, id, target.ID) || isAncestor(s.sections, target.ID, id) {
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
	slices.SortFunc(valid, func(a, b Edge) int {
		return cmp.Or(cmp.Compare(b.Weight, a.Weight), cmp.Compare(a.Target, b.Target))
	})
	seen := make(map[string]bool)
	valid = slices.DeleteFunc(valid, func(edge Edge) bool {
		if seen[edge.Target] {
			return true
		}
		seen[edge.Target] = true
		return false
	})
	valid = valid[:min(semanticDegree, len(valid))]
	next := s.state
	next.Inferred = maps.Clone(next.Inferred)
	next.Inferred[source.ID] = inference{Hash: source.Hash, Edges: valid}
	return s.commit(next, false)
}

func (s *Store) commit(next diskState, sourcesChanged bool) error {
	if err := storage.Save(s.path, &next); err != nil {
		return err
	}
	s.state = next
	s.rebuild(sourcesChanged)
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

func (s *Store) rebuild(sourcesChanged bool) {
	snap := s.snapshot
	if sourcesChanged {
		s.sections = collectSections(s.state.Documents)
		profiles := conceptProfiles(s.state.Documents)
		snap.Nodes, snap.Tasks = []parser.Node{}, []parser.Task{}
		labels := map[string][]parser.Section{}
		for _, doc := range s.state.Documents {
			snap.Tasks = append(snap.Tasks, doc.Tasks...)
			for _, label := range doc.Labels {
				labels[label.Value] = append(labels[label.Value], s.sections[label.Source])
			}
		}
		roads, reviews := projectRoadmaps(s.state.Documents, profiles)
		snap.Roadmaps = roads
		snap.Tasks = append(snap.Tasks, reviews...)
		refs, referenced := explicitEdges(s.state.Documents, labels)
		s.explicit = refs
		s.projected, snap.Files, snap.Headings = significantSections(s.state.Documents, referenced)
		for _, section := range s.projected {
			snap.Nodes = append(snap.Nodes, section.Node)
		}
		sortNodes(snap.Nodes)
		sortNodes(snap.Files)
		sortNodes(snap.Headings)
		slices.SortFunc(snap.Tasks, func(a, b parser.Task) int { return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line)) })
		s.local = conceptEdges(s.projected, profiles, s.sections)
	}
	snap.Revision++
	snap.Pending = 0
	for _, section := range s.projected {
		if s.pending(section) {
			snap.Pending++
		}
	}
	snap.Edges = projectEdges(s.sections, s.projected, s.explicit, s.local, s.state.Inferred)
	snap.Groups, snap.OverviewEdges = groupGraph(snap.Nodes, s.projected, snap.Edges, s.explicit)
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
