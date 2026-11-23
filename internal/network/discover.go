package network

import (
	"cmp"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/AndreaUnali/notesflow/internal/storage"
)

type SearchRoot struct {
	Path  string
	Depth int
}

func DefaultSearchRoots(recents []Recent) []SearchRoot {
	var roots []SearchRoot
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, SearchRoot{cwd, 3})
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, folder := range []string{"Desktop", "Documents"} {
			roots = append(roots, SearchRoot{filepath.Join(home, folder), 6})
		}
	}
	for _, recent := range recents[:min(10, len(recents))] {
		roots = append(roots, SearchRoot{filepath.Dir(recent.Root), 2})
	}
	return roots
}

// Discover streams candidates without waiting for a slow or unavailable search root.
func Discover(ctx context.Context, roots []SearchRoot) <-chan Recent {
	results := make(chan Recent, 32)
	go func() {
		var workers sync.WaitGroup
		active := make(chan struct{}, 4)
		for _, root := range roots {
			workers.Go(func() {
				select {
				case <-ctx.Done():
					return
				case active <- struct{}{}:
				}
				defer func() { <-active }()
				discoverRoot(ctx, root, results)
			})
		}
		workers.Wait()
		close(results)
	}()
	return results
}

func discoverRoot(ctx context.Context, root SearchRoot, results chan<- Recent) {
	path, err := filepath.EvalSymlinks(root.Path)
	if err != nil || !filepath.IsAbs(path) {
		return
	}
	// Visit nearby folders first even when a deep subtree is particularly large.
	queue := []SearchRoot{{path, max(0, root.Depth)}}
	for i := 0; i < len(queue) && i < 2500 && ctx.Err() == nil; i++ {
		candidate := queue[i]
		entries, err := os.ReadDir(candidate.Path)
		if err != nil {
			continue
		}
		if project, ok := identify(candidate.Path, entries); ok {
			select {
			case <-ctx.Done():
				return
			case results <- project:
			}
		}
		if candidate.Depth == 0 {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() && !skipFolder(entry.Name()) {
				queue = append(queue, SearchRoot{filepath.Join(candidate.Path, entry.Name()), candidate.Depth - 1})
			}
		}
	}
}

func skipFolder(name string) bool {
	return strings.HasPrefix(name, ".") || slices.Contains(
		[]string{"node_modules", "vendor", "build", "dist", "out", "__pycache__"}, strings.ToLower(name),
	)
}

var (
	texDocument = regexp.MustCompile(`(?m)^[\t ]*\\documentclass\b`)
	texTitle    = regexp.MustCompile(`\\title(?:\[[^\r\n\]]*\])?\s*\{([^{}\r\n]{1,150})\}`)
	titleBold   = regexp.MustCompile(`\\(?:textbf|textsc)\s*\{([^{}\r\n]{1,150})\}`)
	titleSeries = regexp.MustCompile(`\\(?:Huge|huge|LARGE|Large)\s*\\bfseries\s+([^{}\\\r\n]{1,150})`)
)

func identify(root string, entries []os.DirEntry) (Recent, bool) {
	result := Recent{Root: root, Name: projectDirectoryName(root)}
	var indexed, configured bool
	var texFiles []string
	for _, item := range entries {
		if item.Name() == ".notesflow" && item.IsDir() {
			info, err := os.Stat(filepath.Join(root, ".notesflow", "index.json"))
			indexed = err == nil && info.Mode().IsRegular()
			result.Indexed = indexed
		}
		if item.Type().IsRegular() {
			if item.Name() == "notesflow.json" {
				configured = true
			}
			if strings.EqualFold(filepath.Ext(item.Name()), ".tex") {
				texFiles = append(texFiles, item.Name())
			}
		}
	}
	if configured {
		var cfg struct{ Project string }
		if storage.Load(filepath.Join(root, "notesflow.json"), &cfg) == nil && strings.TrimSpace(cfg.Project) != "" {
			result.Name = strings.TrimSpace(cfg.Project)
			return result, true
		}
	}
	// Prefer conventional entry points before considering other TeX documents.
	rank := func(name string) int {
		if slices.Contains([]string{"main.tex", "root.tex", "master.tex"}, strings.ToLower(name)) {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(texFiles, func(a, b string) int { return cmp.Compare(rank(a), rank(b)) })
	for _, name := range texFiles {
		file, err := os.Open(filepath.Join(root, name))
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(file, 24<<10))
		_ = file.Close()
		if err != nil || !texDocument.Match(data) {
			continue
		}
		match := texTitle.FindSubmatch(data)
		if match == nil {
			if _, page, ok := strings.Cut(string(data), `\begin{titlepage}`); ok {
				page, _, _ = strings.Cut(page, `\end{titlepage}`)
				match = titleSeries.FindSubmatch([]byte(page))
				if match == nil {
					match = titleBold.FindSubmatch([]byte(page))
				}
			}
		}
		if len(match) > 1 && strings.TrimSpace(string(match[1])) != "" {
			result.Name = strings.TrimSpace(string(match[1]))
		}
		return result, true
	}
	return result, configured || indexed
}

func projectDirectoryName(root string) string {
	name := filepath.Base(root)
	if slices.Contains([]string{"latex", "tex", "notes", "appunti"}, strings.ToLower(name)) {
		name = filepath.Base(filepath.Dir(root))
	}
	return strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(name)), " ")
}

func DisplayName(root, configured string) string {
	if configured != filepath.Base(root) {
		return configured
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return configured
	}
	if project, ok := identify(root, entries); ok {
		return project.Name
	}
	return configured
}
