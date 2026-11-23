package graph

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

func TestRoadmapProjectsEvidenceAndOneReviewPerUncertainDecision(t *testing.T) {
	file := filepath.Join(t.TempDir(), "index.json")
	store, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	const road = "analisi/roadmap.md"
	const notes = "analisi/successioni.tex"
	const roadmap = `# Analisi
- [ ] Definire la convergenza [verifica](successioni.tex#sec:limite)
- [ ] Formalizzare il criterio di Cauchy
- [ ] Ripassare tutto
- [ ] Disegnare le code
- [ ] Correggere gli esercizi
- [ ] Documentare il teorema del limite
- [ ] Scrivere il criterio di Cauchy <!-- notesflow: pending -->
`
	const source = `\section{Convergenza}
\label{sec:limite}
La distanza tra il termine e il limite diventa piccola scegliendo un indice sufficientemente grande.

\section{Criterio di Cauchy}
Il criterio controlla la distanza fra i termini della successione da un certo indice in poi.
% TODO VISUAL: Disegnare la banda epsilon.
% DONE VISUAL: Disegnare le code.

\section{Teorema del limite}
Il teorema del limite afferma che ogni successione convergente ammette uno e un solo limite reale.
`
	parsed := parser.Parse(road, []byte(roadmap))
	docs := map[string]parser.Document{
		road: parsed, notes: parser.Parse(notes, []byte(source)),
		"geometria/promemoria.md": parser.Parse("geometria/promemoria.md", []byte("- [x] Correggere gli esercizi")),
		"analisi/falsi.md":        parser.Parse("analisi/falsi.md", []byte("```md\n- [x] Ripassare tutto\n```")),
	}
	if err := store.Apply(docs, nil); err != nil {
		t.Fatal(err)
	}
	snap, _ := store.Snapshot()
	roadView := snap.Roadmaps[0]
	if done, total := roadView.Progress(); done != 4 || total != 7 {
		t.Fatalf("progress = %d/%d, wanted 4/7", done, total)
	}
	countReviews := func(s Snapshot) int {
		n := 0
		for _, task := range s.Tasks {
			if task.Kind == parser.MaintenanceTask {
				n++
				if task.File != road || !strings.Contains(task.Text, notes+":6") {
					t.Errorf("unexpected review: %+v", task)
				}
			}
		}
		return n
	}
	for i, kind := range map[int]string{1: "check", 2: "heading", 4: "completion", 6: "heading"} {
		if e := roadView.Items[i].Evidence; e == nil || e.Kind != kind {
			t.Errorf("item %d evidence = %+v, wanted %s", i, e, kind)
		}
	}
	if *parsed.Roadmap.Items[2].Done || countReviews(snap) != 1 {
		t.Fatal("automatic review changed the source checklist or was missing")
	}

	store, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	snap, _ = store.Snapshot()
	if countReviews(snap) != 1 {
		t.Fatal("the derived review must survive restart once")
	}

	confirmed := strings.Replace(roadmap, "[ ] Formalizzare", "[x] Formalizzare", 1)
	if err := store.Apply(map[string]parser.Document{road: parser.Parse(road, []byte(confirmed))}, []string{notes}); err != nil {
		t.Fatal(err)
	}
	snap, _ = store.Snapshot()
	if done, total := snap.Roadmaps[0].Progress(); done != 1 || total != 7 || countReviews(snap) != 0 {
		t.Fatalf("after confirmation and deletion: %d/%d, %d reviews", done, total, countReviews(snap))
	}
}
