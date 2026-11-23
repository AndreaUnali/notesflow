package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

func TestSectionDraftMergesOutsideEditsAndRecordsDecisions(t *testing.T) {
	const source = "\\section{Introduzione}\nTesto iniziale.\n\n\\section{Container}\nDa completare.\n\\end{document}\n"
	file := "capitoli/main.tex"
	sections := parser.Parse(file, []byte(source)).Sections
	target := Target{File: file, Section: sections[2].ID, Title: sections[2].Title, Line: sections[2].Line}
	original, err := Excerpt(target, source)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{Target: target, Original: original, Automatic: true, Decision: "Prima i namespaces", DecisionWhy: "Ordine delle slide",
		Result: Draft{Text: "\\section{Container}\nI namespaces isolano la vista dei processi.", Visuals: []Visual{{"slides.pdf, p. 14", "Schema host e processi isolati"}}},
	}
	otherEdit := strings.Replace(source, "Testo iniziale.", "Testo scritto dall’utente.", 1)
	got, line, err := Update(job, otherEdit)
	if err != nil {
		t.Fatal(err)
	}
	if line != 4 || !strings.Contains(got, "Testo scritto dall’utente.") || !strings.HasSuffix(got, "\\end{document}\n") {
		t.Fatalf("unexpected result at %d:\n%s", line, got)
	}
	tasks := parser.Parse(file, []byte(got)).Tasks
	if len(tasks) != 2 || tasks[0].Kind != "VISUAL" || tasks[1].Kind != parser.MaintenanceTask {
		t.Fatalf("missing agent TODOs: %#v", tasks)
	}
	if _, _, err := Update(job, strings.Replace(source, "Da completare.", "Già modificato.", 1)); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed target accepted: %v", err)
	}
	job.Result.Text += "\n\\includegraphics{img.png}"
	if _, err := Replacement(job); err == nil {
		t.Fatal("visual asset accepted")
	}
}

func TestParseSourceBoundsPDFPages(t *testing.T) {
	ref, err := ParseSource("fonti/pacchetto 3.pdf:12-14")
	if err != nil || ref.File != "fonti/pacchetto 3.pdf" || ref.First != 12 || ref.Last != 14 {
		t.Fatalf("incorrect source %v (%v)", ref, err)
	}
	for _, input := range []string{"file.pdf:0", "file.pdf:90-2", "file.pdf:1-99", "file.png", "/file.pdf"} {
		if _, err := ParseSource(input); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
}
