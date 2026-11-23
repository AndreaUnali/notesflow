package graph

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

func TestOverviewKeepsChaptersAndProjectsReferencesFromSmallHeadings(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"main.tex": `\documentclass{article}
\input{capitoli/primo}
\input{capitoli/secondo}`,
		"config.tex": `\usepackage{amsmath}`,
		"roadmap.md": "# Roadmap\n- [ ] Un obiettivo",
		"capitoli/primo.tex": `\section{Primo capitolo}
Una descrizione sufficientemente completa e utile per leggere gli appunti del corso.
\paragraph{Un dettaglio}
Il rimando al capitolo successivo si trova qui \ref{fig:dettaglio}.`,
		"capitoli/secondo.tex": `\section{Secondo capitolo}
Una descrizione rilevante per iniziare questo capitolo degli appunti.
\paragraph{Altro dettaglio}
\label{fig:dettaglio}
Questa spiegazione contiene un esempio completo di quanto discusso.`,
	}
	docs := make(map[string]parser.Document)
	for file, text := range files {
		docs[file] = parser.Parse(file, []byte(text))
	}
	if err := store.Apply(docs, nil); err != nil {
		t.Fatal(err)
	}
	snap, _ := store.Snapshot()
	if len(snap.Files) != 5 || len(snap.Groups) != 2 || len(snap.Nodes) != 4 || len(snap.Headings) != 4 {
		t.Fatalf("files %d, chapters %d, nodes %d, searchable headings %d", len(snap.Files), len(snap.Groups), len(snap.Nodes), len(snap.Headings))
	}
	if len(snap.OverviewEdges) != 1 || snap.OverviewEdges[0].Kind != "ref" {
		t.Fatalf("overview: %+v", snap.OverviewEdges)
	}
	small := docs["capitoli/secondo.tex"].Sections[2].ID
	if node, ok := SourceNode(snap, small); !ok || node.Title != "Altro dettaglio" {
		t.Fatal("omitted details must remain searchable")
	}
}

func TestSemanticProjectionKeepsOnlyTwoStrongDistinctNeighbors(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for i, title := range []string{"Virtualizzazione", "Container", "Bilanciamento", "Sequenze", "Archiviazione", "Sicurezza"} {
		fmt.Fprintf(&body, "\\section{%s}\nUna spiegazione per il concetto numero %d racconta come si usa la tecnologia in questo capitolo.\n", title, i)
	}
	doc := parser.Parse("appunti.tex", []byte(body.String()))
	if err = store.Apply(map[string]parser.Document{doc.File: doc}, nil); err != nil {
		t.Fatal(err)
	}
	for i, source := range doc.Sections[1:] {
		delta, ok := store.Delta(source.ID, 20, 500)
		if !ok {
			t.Fatalf("missing delta for %q", source.Title)
		}
		var edges []Edge
		for j, target := range doc.Sections[1:] {
			if source.ID != target.ID {
				edges = append(edges, Edge{Target: target.ID, Weight: 1 - float64(j)*.06, Origin: "opencode"})
			}
		}
		if i == 0 {
			edges = []Edge{{Target: doc.Sections[2].ID, Weight: .99, Origin: "mock"}}
		}
		if err = store.Resolve(delta, edges); err != nil {
			t.Fatal(err)
		}
	}
	snap, _ := store.Snapshot()
	degree := make(map[string]int)
	links := 0
	for _, edge := range snap.Edges {
		if edge.Kind != "semantic" {
			continue
		}
		links++
		degree[edge.Source]++
		degree[edge.Target]++
		if edge.Origin == "mock" {
			t.Fatal("generic shared vocabulary passed title anchoring")
		}
	}
	if links == 0 {
		t.Fatal("all strong relationships were lost")
	}
	for id, n := range degree {
		if n > semanticDegree {
			t.Errorf("%q has %d links", id, n)
		}
	}
}

func TestChapterOverviewIncludesSelectedConceptsAndOneBridgePerPair(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]parser.Document{}
	sources := map[string]string{
		"primo.tex": `\chapter{Virtualizzazione}
\section{VM and Hypervisor}
The guest VM exposes virtual devices controlled by the hypervisor running on its host. A container uses a different isolation strategy.
\section{Physical hosts}
Hardware is managed from the operating system on each physical node, with network configuration and memory shared by workloads.
`,
		"secondo.tex": `\chapter{Isolamento}
\section{Containers vs Hypervisors}
Unlike a VM, a container uses the host kernel directly. A hypervisor handles the guest operating system and presents virtual hardware devices.
\section{Resource controls}
The control group manages memory and processor time assigned to an isolated process, and it can restrict storage access accordingly.
`,
	}
	for file, text := range sources {
		docs[file] = parser.Parse(file, []byte(text))
	}
	if err = store.Apply(docs, nil); err != nil {
		t.Fatal(err)
	}
	view, _ := store.Snapshot()
	if len(view.Groups) != 2 {
		t.Fatalf("want 2 chapters: %+v", view.Groups)
	}
	var hierarchy, bridges int
	for _, e := range view.OverviewEdges {
		switch e.Kind {
		case "semantic":
			bridges++
		case "hierarchy":
			hierarchy++
		}
	}
	if hierarchy != 4 || bridges != 1 {
		t.Fatalf("overview should show four topics and their bridge: %+v", view.OverviewEdges)
	}
	for _, g := range view.Groups {
		if len(g.Topics) != 2 || len(g.Topics) > mapTopicsPerChapter {
			t.Fatalf("unexpected chapter topics: %+v", g)
		}
	}
}
