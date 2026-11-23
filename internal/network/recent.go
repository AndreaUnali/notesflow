// Package network stores the projects a user has opened and their workspace preferences.
package network

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/storage"
)

type Recent struct {
	Root   string        `json:"root"`
	Name   string        `json:"name"`
	Opened time.Time     `json:"opened"`
	Nodes  []parser.Node `json:"nodes,omitempty"`
	Edges  []graph.Edge  `json:"edges,omitempty"`
}

type Position struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Top    int    `json:"top"`
}

type Session struct {
	MainTeX  string              `json:"main_tex,omitempty"`
	PDF      string              `json:"pdf,omitempty"`
	Position map[string]Position `json:"position,omitempty"`
}

func sessionPath(root string) string { return filepath.Join(root, ".notesflow", "session.json") }

func LoadSession(root string) (Session, error) {
	session := Session{Position: map[string]Position{}}
	err := storage.Load(sessionPath(root), &session)
	if session.Position == nil {
		session.Position = map[string]Position{}
	}
	return session, err
}

func SaveSession(root string, session Session) error {
	return storage.Save(sessionPath(root), session)
}

func recentsPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(dir) {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "notesflow", "recent.json"), nil
}

func List() ([]Recent, error) {
	path, err := recentsPath()
	if err != nil {
		return nil, err
	}
	var entries []Recent
	err = storage.Load(path, &entries)
	return entries, err
}

func Remember(root, name string, snapshot graph.Snapshot) error {
	path, err := recentsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Close()
	var entries []Recent
	if err := storage.Load(path, &entries); err != nil {
		return err
	}
	entries = slices.DeleteFunc(entries, func(entry Recent) bool { return entry.Root == root })
	nodes, edges := compact(snapshot)
	entries = append([]Recent{{Root: root, Name: name, Opened: time.Now(), Nodes: nodes, Edges: edges}}, entries...)
	return storage.Save(path, entries[:min(50, len(entries))])
}

func Search(entries []Recent, query string) []Recent {
	query = strings.ToLower(strings.TrimSpace(query))
	result := slices.Clone(entries)
	if query != "" {
		result = slices.DeleteFunc(result, func(entry Recent) bool {
			return !strings.Contains(strings.ToLower(entry.Name+" "+entry.Root), query)
		})
	}
	rank := func(name string) int {
		switch {
		case query != "" && strings.EqualFold(name, query):
			return 0
		case query != "" && strings.HasPrefix(strings.ToLower(name), query):
			return 1
		default:
			return 2
		}
	}
	slices.SortStableFunc(result, func(a, b Recent) int {
		return cmp.Or(cmp.Compare(rank(a.Name), rank(b.Name)), b.Opened.Compare(a.Opened))
	})
	return result
}

// Preview uses the same projected graph as the course view, without scanning recent projects.
func (entry Recent) Preview() ([]parser.Node, []graph.Edge) {
	if len(entry.Nodes) > 0 {
		return entry.Nodes, entry.Edges
	}
	path := filepath.Join(entry.Root, ".notesflow", "index.json")
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	store, err := graph.Open(path)
	if err != nil {
		return nil, nil
	}
	snapshot, _ := store.Snapshot()
	return compact(snapshot)
}

func compact(snapshot graph.Snapshot) ([]parser.Node, []graph.Edge) {
	index := make(map[string]parser.Node, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		index[node.ID] = node
	}
	var nodes []parser.Node
	selected := map[string]bool{}
	for _, group := range snapshot.Groups[:min(4, len(snapshot.Groups))] {
		nodes = append(nodes, group.Node)
		selected[group.ID] = true
		for _, id := range group.Topics[:min(2, len(group.Topics))] {
			if node, ok := index[id]; ok {
				nodes = append(nodes, node)
				selected[id] = true
			}
		}
	}
	var edges []graph.Edge
	for _, edge := range snapshot.OverviewEdges {
		if selected[edge.Source] && selected[edge.Target] {
			edges = append(edges, edge)
		}
	}
	return nodes, edges
}
