package graph

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

func TestDeltaReopensOnlyEditedSection(t *testing.T) {
	file := filepath.Join(t.TempDir(), "index.json")
	store, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	const name = "corso/appunti.tex"
	original := `\section{Serie}
\label{serie}
Una serie converge quando le somme parziali ammettono un limite finito.
% TODO VISUAL: Disegnare le somme parziali.
\subsection{Criterio}
\eqref{serie}
` + "Termine " + strings.Repeat("suffisso ", 50) + "\n\\section{Confronto}\nIl criterio di confronto usa un limite. " + strings.Repeat("suffisso ", 50) + "FINE_INDICE_PRIVATO"
	doc := parser.Parse(name, []byte(original))
	if err := store.Apply(map[string]parser.Document{name: doc}, nil); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := store.Snapshot()
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Kind != "VISUAL" || snapshot.Tasks[0].Line != 4 {
		t.Fatalf("TODO indicizzati: %+v", snapshot.Tasks)
	}
	found := false
	for _, edge := range snapshot.Edges {
		found = found || edge.Kind == "eqref" && edge.Target == doc.Sections[1].ID
	}
	if !found {
		t.Fatalf("eqref mancante: %+v", snapshot.Edges)
	}
	for _, id := range store.Pending() {
		delta, _ := store.Delta(id, 10, 500)
		if err := store.Resolve(delta, nil); err != nil {
			t.Fatal(err)
		}
	}
	store, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if pending := store.Pending(); len(pending) != 0 {
		t.Fatalf("sezioni già analizzate: %v", pending)
	}

	edited := parser.Parse(name, []byte(strings.Replace(original, "Una serie converge quando le somme parziali ammettono un limite finito.", "Una serie converge oppure diverge con una caratterizzazione del limite.", 1)))
	if err := store.Apply(map[string]parser.Document{name: edited}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{doc.Sections[1].ID}
	if pending := store.Pending(); !reflect.DeepEqual(pending, want) {
		t.Fatalf("in coda %v, atteso %v", pending, want)
	}
	delta, ok := store.Delta(want[0], 10, 500)
	if !ok || len(delta.Candidates) != 1 {
		t.Fatalf("delta inatteso: %+v", delta)
	}
	payload, err := json.Marshal(delta)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "FINE_INDICE_PRIVATO") || !strings.Contains(delta.Text, "diverge") {
		t.Fatalf("payload delta inatteso: %s", payload)
	}
	store, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if pending := store.Pending(); !reflect.DeepEqual(pending, want) {
		t.Fatalf("coda dopo riavvio: %v", pending)
	}
}
