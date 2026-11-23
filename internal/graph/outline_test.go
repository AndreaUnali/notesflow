package graph

import (
	"path/filepath"
	"testing"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

func TestOutlineUpdatesFromBilingualNotesWithoutEditingTheRoadmap(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	const road = "corso/roadmap.md"
	const notes = "corso/virtualizzazione.tex"
	const outline = `# Report di studio
## CAPITOLO 1: Infrastruttura virtuale
### 1.1 Isolamento dei processi: Namespace e Control Groups
* **Spiegazione concettuale:** obiettivo della sezione e materiali di studio.
#### Tabella comparativa
| Nome | Funzione |
| --- | --- |
| Namespace | Isolamento |
### 1.2 Tassonomia: VM e Hypervisor
- Materiali del capitolo.
### 1.3 Architettura del Cluster Kubernetes e Service Mesh
* Obiettivo ancora da studiare.
`
	const source = `\chapter{Computing infrastructure}
\section{VM and Hypervisor}
\paragraph{Definitions}
The hypervisor is the software layer that provides virtual hardware to a VM and allocates memory, storage and processor time between guests.
\section{Namespaces and Cgroups}
\begin{itemize}
  \item \textbf{Namespaces} provide process isolation and separate what each workload can see.
  \item \textbf{Cgroups} control resources per process and cap memory consumption.
\end{itemize}
% TODO: Add a comparison of cgroups versions.
`
	original := parser.Parse(road, []byte(outline))
	if err = store.Apply(map[string]parser.Document{road: original, notes: parser.Parse(notes, []byte(source))}, nil); err != nil {
		t.Fatal(err)
	}
	first, _ := store.Snapshot()
	got := first.Roadmaps[0]
	if done, total := got.Progress(); done != 2 || total != 3 {
		t.Fatalf("progress = %d/%d, want 2/3", done, total)
	}
	if got.Items[0].Evidence == nil || got.Items[0].Evidence.Review == "" || got.Items[1].Evidence == nil || got.Items[2].Evidence != nil {
		t.Fatalf("unexpected evidence: %+v", got.Items)
	}
	if original.Roadmap.Items[2].Done != nil {
		t.Fatal("outline parsing or projection wrote back a check")
	}
	withKubernetes := source + `
\section{Kubernetes Cluster Architecture and Service Mesh}
The Kubernetes cluster consists of a control plane and worker nodes, while the service mesh handles interservice communication and traffic policies.
`
	if err = store.Apply(map[string]parser.Document{notes: parser.Parse(notes, []byte(withKubernetes))}, nil); err != nil {
		t.Fatal(err)
	}
	second, _ := store.Snapshot()
	if done, total := second.Roadmaps[0].Progress(); done != 3 || total != 3 {
		t.Fatalf("saving only notes should update the roadmap, got %d/%d", done, total)
	}
}
