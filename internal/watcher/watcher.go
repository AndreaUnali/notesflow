// Package watcher reconciles filesystem events against the persisted section index.
package watcher

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/parser"
)

const maxSourceSize = 8 << 20

type stamp struct {
	size int64
	mod  time.Time
}

type Watcher struct {
	root    string
	store   *graph.Store
	known   map[string]stamp
	native  *fsnotify.Watcher
	watched map[string]bool
}

func New(root string, store *graph.Store) *Watcher {
	return &Watcher{root: root, store: store, known: map[string]stamp{}, watched: map[string]bool{}}
}

func eligible(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".tex", ".md":
		return true
	case ".pdf":
		return parser.IsRoadmap(file)
	default:
		return false
	}
}

func (w *Watcher) Scan(ctx context.Context) error {
	return w.scan(ctx, true, nil)
}

func (w *Watcher) scan(ctx context.Context, full bool, forced map[string]bool) error {
	prior := w.store.Hashes()
	seen := make(map[string]stamp)
	changed := make(map[string]parser.Document)
	var diagnostics []error
	err := filepath.WalkDir(w.root, func(absolute string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if absolute != w.root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
				return fs.SkipDir
			}
			if w.native != nil && !w.watched[absolute] {
				if err := w.native.Add(absolute); err == nil {
					w.watched[absolute] = true
				}
			}
			return nil
		}
		if !entry.Type().IsRegular() || !eligible(entry.Name()) || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		rel, err := filepath.Rel(w.root, absolute)
		if err != nil {
			return err
		}
		file := filepath.ToSlash(rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		now := stamp{info.Size(), info.ModTime()}
		seen[file] = now
		if !full && !forced[absolute] && w.known[file] == now {
			return nil
		}
		// PDF extraction has its own deadline. Its binary is never sent to a provider.
		doc, loaded, err := read(ctx, absolute, file, prior[file], info.Size())
		if err != nil {
			diagnostics = append(diagnostics, fmt.Errorf("%s: %w", file, err))
			delete(seen, file)
			prior[file] = "" // keep any previous index entry and retry on the next scan
			return nil
		}
		if loaded {
			changed[file] = doc
		}
		return nil
	})
	if err != nil {
		return err
	}
	var deleted []string
	for file, hash := range prior {
		if _, exists := seen[file]; !exists && hash != "" {
			deleted = append(deleted, file)
		}
	}
	if err := w.store.Apply(changed, deleted); err != nil {
		return err
	}
	w.known = seen
	if len(changed)+len(deleted) > 0 {
		w.store.Log(fmt.Sprintf("Indice aggiornato · %d file letti · %d rimossi", len(changed), len(deleted)))
	}
	return errors.Join(diagnostics...)
}

func read(ctx context.Context, absolute, file, previous string, size int64) (parser.Document, bool, error) {
	if strings.EqualFold(filepath.Ext(file), ".pdf") {
		return readPDF(ctx, absolute, file, previous, size)
	}
	if size > maxSourceSize {
		return parser.Document{}, false, errors.New("sorgente oltre 8 MiB")
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return parser.Document{}, false, err
	}
	if parser.Digest(data) == previous {
		return parser.Document{}, false, nil
	}
	return parser.Parse(file, data), true, nil
}

func readPDF(ctx context.Context, absolute, file, previous string, size int64) (parser.Document, bool, error) {
	doc := parser.Document{File: file, Roadmap: &parser.Roadmap{File: file, Course: parser.Course(file)}}
	if size > 64<<20 {
		doc.Hash = "oversize"
		doc.Roadmap.Note = "PDF oltre 64 MiB; aprilo con il visualizzatore."
		return doc, doc.Hash != previous, nil
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return doc, false, err
	}
	binary, err := exec.LookPath("pdftotext")
	doc.Hash = parser.Digest(data) + "|" + binary
	if doc.Hash == previous {
		return doc, false, nil
	}
	if err != nil {
		doc.Roadmap.Note = "Installa pdftotext (Poppler) per leggere il testo nel terminale."
		return doc, true, nil
	}
	extract, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	text, err := exec.CommandContext(extract, binary, "-f", "1", "-l", "8", "-layout", "-nopgbrk", absolute, "-").Output()
	if err != nil {
		doc.Roadmap.Note = "Estrazione PDF fallita; apri il documento con il visualizzatore."
		return doc, true, nil
	}
	for _, line := range strings.Split(string(text), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			doc.Roadmap.Items = append(doc.Roadmap.Items, parser.Milestone{Title: line})
		}
		if len(doc.Roadmap.Items) == 150 {
			break
		}
	}
	doc.Roadmap.Note = "Anteprima del testo · prime 8 pagine · fino a 150 righe"
	if len(doc.Roadmap.Items) == 0 {
		doc.Roadmap.Note = "Nessun testo estraibile; apri il PDF con il visualizzatore."
	}
	return doc, true, nil
}

// Run coalesces editor writes and renames; reconciliation also catches missed events.
func (w *Watcher) Run(ctx context.Context) {
	native, err := fsnotify.NewWatcher()
	var events <-chan fsnotify.Event
	var failures <-chan error
	if err == nil {
		w.native = native
		defer native.Close()
		events, failures = native.Events, native.Errors
	} else {
		w.store.Log("Watcher nativo indisponibile; scansione periodica attiva.")
	}
	scan := func(full bool, forced map[string]bool) {
		if err := w.scan(ctx, full, forced); err != nil && ctx.Err() == nil {
			w.store.Log("Scansione: " + err.Error())
		}
	}
	scan(false, nil)
	poll := time.NewTicker(3 * time.Second)
	defer poll.Stop()
	verify := time.NewTicker(time.Minute)
	defer verify.Stop()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var debounce <-chan time.Time
	dirty := make(map[string]bool)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
				for dir := range w.watched {
					if dir == ev.Name || strings.HasPrefix(dir, ev.Name+string(os.PathSeparator)) {
						delete(w.watched, dir)
					}
				}
			}
			if ev.Op == fsnotify.Chmod {
				continue
			}
			dirty[ev.Name] = true
			if debounce == nil {
				timer.Reset(160 * time.Millisecond)
				debounce = timer.C
			}
		case <-debounce:
			debounce = nil
			scan(false, dirty)
			clear(dirty)
		case <-poll.C:
			scan(false, nil)
		case <-verify.C:
			scan(true, nil)
		case err, ok := <-failures:
			if !ok {
				failures = nil
				continue
			}
			w.store.Log("Watcher nativo: " + err.Error())
			scan(true, nil)
		}
	}
}
